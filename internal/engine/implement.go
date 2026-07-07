package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/graph"
)

// ImplementOpts controls `sr implement`.
type ImplementOpts struct {
	Ref      string // raw issue reference (number, key, or URL)
	Source   string // "" = auto-detect; "github" | "jira" force it
	Branch   string // override the derived branch name
	Parent   string // "" = current branch
	Worktree bool
	Sandbox  bool // implies Worktree
	AI       bool // emit JSON and hand off
	Comments bool // include the issue discussion in the prompt
	Network  string
}

// ImplementResult is the JSON emitted in hand-off/AI mode.
type ImplementResult struct {
	Branch        string `json:"branch"`
	WorktreePath  string `json:"worktreePath,omitempty"`
	IssueRef      string `json:"issueRef"`
	Prompt        string `json:"prompt"`
	AttachCommand string `json:"attachCommand,omitempty"`
}

// driveKind is how implementation is carried out after the branch is scaffolded.
type driveKind int

const (
	driveEmitJSON        driveKind = iota // hand off: print JSON, no agent spawned
	driveSpawn                            // spawn an interactive claude on the host
	driveSandboxAttach                    // launch the sandbox and attach
	driveSandboxDetached                  // launch the sandbox detached, emit JSON
)

// chooseDrive selects the drive mode. "Hand-off" (don't seize the terminal)
// applies when the caller is another agent (--ai), we're already inside a Claude
// session (CLAUDECODE=1), or there is no interactive terminal.
func chooseDrive(sandbox, ai, interactive, inClaude bool) driveKind {
	handoff := ai || inClaude || !interactive
	if sandbox {
		if handoff {
			return driveSandboxDetached
		}
		return driveSandboxAttach
	}
	if handoff {
		return driveEmitJSON
	}
	return driveSpawn
}

// insideClaude reports whether we're running inside a Claude Code session, which
// sets CLAUDECODE=1 for every command it spawns.
func insideClaude() bool {
	return os.Getenv("CLAUDECODE") == "1"
}

// Implement fetches an issue, creates a new tracked branch for it, records the
// linkage, and drives implementation per flags/context (ADR-0013 for fetch).
func Implement(c *context.Context, opts ImplementOpts) error {
	source, ref, locator, err := detectSource(opts.Ref, opts.Source)
	if err != nil {
		return err
	}
	iss, err := fetchIssue(source, ref, locator, opts.Comments)
	if err != nil {
		return err
	}
	if strings.TrimSpace(iss.Title) == "" {
		return fmt.Errorf("issue %s has no title — cannot derive a branch name", iss.displayRef())
	}

	branch := opts.Branch
	if branch == "" {
		branch = deriveBranchName(iss)
	}
	if exists, err := c.Git.BranchExists(branch); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("branch %q already exists — pass --branch to choose another name", branch)
	}

	worktree := opts.Worktree || opts.Sandbox
	kind := chooseDrive(opts.Sandbox, opts.AI, c.Interactive, insideClaude())
	// In hand-off/emit modes stdout is a data channel (JSON), so silence our own
	// prints and route git/docker subprocess chatter to stderr (see quietStdout).
	emitting := kind == driveEmitJSON || kind == driveSandboxDetached
	if emitting {
		c.Quiet = true
	}

	scaffold := func() error {
		// Parent handling: branch off the current branch unless --parent overrides.
		orig, err := c.Git.CurrentBranch()
		if err != nil {
			return err
		}
		parentChanged := false
		if opts.Parent != "" && opts.Parent != orig {
			if err := c.Git.Checkout(opts.Parent); err != nil {
				return fmt.Errorf("could not switch to parent %q: %w", opts.Parent, err)
			}
			parentChanged = true
		}

		if err := Create(c, CreateOpts{
			Name:     branch,
			Desc:     fmt.Sprintf("%s: %s", iss.displayRef(), iss.Title),
			Worktree: worktree,
		}); err != nil {
			return err
		}

		// When a worktree holds the work, the caller's checkout shouldn't have
		// moved to the parent — restore where they started.
		if worktree && parentChanged {
			if err := c.Git.Checkout(orig); err != nil && !c.Quiet {
				fmt.Printf("Warning: could not restore branch %q: %v\n", orig, err)
			}
		}

		if err := setTicketContext(c, branch, iss); err != nil && !c.Quiet {
			fmt.Printf("Warning: could not record ticket context: %v\n", err)
		}
		return nil
	}

	if emitting {
		err = quietStdout(scaffold)
	} else {
		err = scaffold()
	}
	if err != nil {
		return err
	}

	prompt := buildPrompt(iss, branch, opts.Comments)
	worktreePath := ""
	if worktree {
		worktreePath = filepath.Join(c.Git.Dir, ".worktrees", branch)
	}

	res := ImplementResult{
		Branch:       branch,
		WorktreePath: worktreePath,
		IssueRef:     iss.displayRef(),
		Prompt:       prompt,
	}

	switch kind {
	case driveEmitJSON:
		return emitImplementJSON(res)

	case driveSpawn:
		return spawnImplementClaude(worktreePath, prompt)

	case driveSandboxAttach:
		return SandboxRun(c, SandboxRunOpts{Branch: branch, Prompt: prompt, Network: opts.Network, Attach: true})

	case driveSandboxDetached:
		if err := quietStdout(func() error {
			return SandboxRun(c, SandboxRunOpts{Branch: branch, Prompt: prompt, Network: opts.Network, Attach: false})
		}); err != nil {
			return err
		}
		res.AttachCommand = fmt.Sprintf("sr sandbox attach %s", branch)
		return emitImplementJSON(res)
	}
	return nil
}

// quietStdout runs fn with os.Stdout pointed at os.Stderr, so subprocess output
// that git/docker send to stdout (checkout diagnostics, "HEAD is now at", …) is
// kept off our JSON data channel. Restores os.Stdout before returning. Safe for
// this single-threaded CLI; not concurrency-safe.
func quietStdout(fn func() error) error {
	orig := os.Stdout
	os.Stdout = os.Stderr
	defer func() { os.Stdout = orig }()
	return fn()
}

// setTicketContext records the issue reference as a `ticket` branch-context entry.
func setTicketContext(c *context.Context, branch string, iss Issue) error {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}
	text := iss.URL
	if text == "" {
		text = iss.displayRef()
	}
	entry := graph.BranchContext{
		Key:     "ticket",
		Text:    text,
		Tickets: []string{iss.Ref},
	}
	if iss.URL != "" {
		entry.Sources = []graph.Source{{Type: "ticket", Reference: iss.URL}}
	}
	if err := g.SetContext(branch, entry); err != nil {
		return err
	}
	return c.Store.WriteGraph(g)
}

func emitImplementJSON(res ImplementResult) error {
	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal implement result: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

// spawnImplementClaude launches an interactive Claude session seeded with the
// brief. Host spawns run with normal permissions (no skip-permissions — that's
// the --sandbox path's job).
func spawnImplementClaude(worktreePath, prompt string) error {
	if _, err := exec.LookPath("claude"); err != nil {
		return fmt.Errorf("claude CLI not found — install it from https://claude.ai/code")
	}
	cmd := exec.Command("claude", prompt)
	if worktreePath != "" {
		cmd.Dir = worktreePath
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
