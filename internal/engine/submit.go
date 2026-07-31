package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/amustafa/stackr/internal/context"
	gitpkg "github.com/amustafa/stackr/internal/git"
	"github.com/amustafa/stackr/internal/graph"
	"github.com/amustafa/stackr/internal/store"
	"github.com/amustafa/stackr/internal/ui"
)

// SubmitOpts controls push/PR behavior.
type SubmitOpts struct {
	Draft      bool
	Stack      bool // Push all branches in the stack
	UpdateOnly bool // Only update already-pushed branches

	// Force answers every Content Divergence with "overwrite remote" instead of
	// prompting. It does NOT skip preflight and does NOT relax the lease: the
	// push is still pinned to the commit that was inspected, so a remote that
	// moves after the decision still rejects.
	Force bool
	// NoForce refuses to force-push at all, failing instead. Mutually exclusive
	// with Force.
	NoForce bool

	DryRun bool
	Reviewers  []string
	Title      string // PR title (programmatic mode — skip interactive)
	Body       string // PR body (programmatic mode — skip interactive)
	BodyFile   string // Read PR body from file instead of --body
	AI         bool   // Spawn Claude session to own the submit flow
	AIPrepare  bool   // Output JSON context and exit (no push, no PR)
}

// Submit pushes branches to the remote and manages PRs.
func Submit(c *context.Context, opts SubmitOpts) error {
	// Mode 1a: --aiprepare outputs JSON context and exits.
	if opts.AIPrepare {
		return submitAIPrepare(c)
	}

	// Mode 3: --ai spawns a Claude session that owns the flow.
	if opts.AI {
		return submitAI(c, opts)
	}

	// Resolve body from file if specified.
	if opts.BodyFile != "" {
		data, err := os.ReadFile(opts.BodyFile)
		if err != nil {
			return fmt.Errorf("could not read body file: %w", err)
		}
		opts.Body = string(data)
	}

	if err := ghCheckInstalled(); err != nil {
		return err
	}

	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}
	cfg, err := c.Store.ReadConfig()
	if err != nil {
		return err
	}

	current, err := c.Git.CurrentBranch()
	if err != nil {
		return err
	}

	prInfo, err := c.Store.ReadPRInfo()
	if err != nil {
		return err
	}

	if opts.Force && opts.NoForce {
		return fmt.Errorf("--force and --no-force are mutually exclusive")
	}

	set, dropped, err := buildPushSet(c, g, cfg, opts, current)
	if err != nil {
		return err
	}
	reportDropped(c, dropped)
	if len(set) == 0 {
		fmt.Println("Nothing to submit")
		return nil
	}

	if opts.DryRun {
		return dryRunReport(c, cfg, set)
	}

	pre, err := Preflight(c, opts, cfg, set)
	if err != nil {
		return err
	}
	reportDropped(c, pre.Dropped)

	if pre.Stopped {
		fmt.Println("\nStopped. Nothing was pushed.")
		if pre.Mutated && pre.RollbackID != "" {
			fmt.Printf("Local branches were changed. To put them back: sr rollback %s\n", pre.RollbackID)
		}
		return nil
	}

	pushed, err := pushPhase(c, cfg, pre.Ready)
	if err != nil {
		return err
	}

	return reconcilePhase(c, opts, cfg, prInfo, current, pushed)
}

// reportDropped names every branch left out of this submit. An operation that
// silently treats one branch differently from its neighbours is
// indistinguishable from a bug.
func reportDropped(c *context.Context, dropped []DroppedBranch) {
	if c.Quiet || len(dropped) == 0 {
		return
	}
	for _, d := range dropped {
		fmt.Printf("Skipping %s (%s)\n", d.Name, d.Reason)
	}
}

// dryRunReport classifies without touching anything — no fetch side effects
// beyond updating remote-tracking refs, no remediation, no restack, no push.
func dryRunReport(c *context.Context, cfg *store.Config, set []string) error {
	if err := c.Git.Fetch(cfg.Remote); err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}
	fmt.Println("[dry-run] nothing will be changed locally or on the remote")
	for _, name := range set {
		class, err := ClassifyBranch(c, cfg.Remote, name)
		if err != nil {
			return err
		}
		switch class.Disposition {
		case DispNeedsDecision:
			fmt.Printf("  %s: needs a decision — %s\n", name, class.Reason)
		case DispPushForce:
			fmt.Printf("  %s: force push (lossless, lease pinned to %s)\n", name, abbrev(class.RemoteSHA))
		default:
			fmt.Printf("  %s: %s\n", name, class.Disposition)
		}
	}
	return nil
}

// pushPhase publishes the settled branches bottom-up.
//
// It re-fetches first: preflight may have sat waiting on a human for a long
// time, and every lease was pinned to what the remote held before that pause.
// If anything moved we publish nothing rather than half a stack.
func pushPhase(c *context.Context, cfg *store.Config, ready []Classification) ([]string, error) {
	if err := c.Git.Fetch(cfg.Remote); err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}

	for _, class := range ready {
		if class.RemoteSHA == "" {
			continue
		}
		now, err := c.Git.RevParse(cfg.Remote + "/" + class.Branch)
		if err != nil {
			continue // the ref vanished; the lease will catch it
		}
		if now != class.RemoteSHA {
			return nil, fmt.Errorf(
				"%s changed on the remote while you were resolving — nothing was pushed.\n"+
					"Run `sr submit` again to re-check it.", class.Branch)
		}
	}

	var pushed []string
	for _, class := range ready {
		if class.Disposition == DispNoPush {
			if !c.Quiet {
				fmt.Printf("%s: already up to date\n", class.Branch)
			}
			pushed = append(pushed, class.Branch)
			continue
		}

		if !c.Quiet {
			if class.Disposition == DispPushForce {
				fmt.Printf("Force pushing %s -> %s/%s\n", class.Branch, cfg.Remote, class.Branch)
			} else {
				fmt.Printf("Pushing %s -> %s/%s\n", class.Branch, cfg.Remote, class.Branch)
			}
		}

		if err := c.Git.PushPinned(cfg.Remote, class.Branch, class.RemoteSHA, true); err != nil {
			if gitpkg.IsStaleLease(err) {
				return pushed, fmt.Errorf(
					"%w\n\nPushed: %s\nNot pushed: %s\nRun `sr submit` again to re-check the rest.",
					err, joinOrNone(pushed), joinOrNone(remaining(ready, class.Branch)))
			}
			return pushed, err
		}

		// Record what we just left there. This is what makes the next submit of
		// this branch provably safe to force (ADR-0014).
		if newSHA, err := c.Git.RevParse(class.Branch); err == nil {
			_ = c.Store.SetPushRecord(cfg.Remote, class.Branch, newSHA)
		}
		pushed = append(pushed, class.Branch)
	}

	return pushed, nil
}

func remaining(ready []Classification, from string) []string {
	var out []string
	seen := false
	for _, class := range ready {
		if class.Branch == from {
			seen = true
		}
		if seen {
			out = append(out, class.Branch)
		}
	}
	return out
}

func joinOrNone(names []string) string {
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}

// reconcilePhase brings GitHub in line with what was just published: PR bases,
// a PR for the branch being submitted, then stack registration.
//
// Bottom-up push order guarantees a parent already exists on the remote before
// its child's base is retargeted.
func reconcilePhase(c *context.Context, opts SubmitOpts, cfg *store.Config,
	prInfo *store.PRInfo, current string, pushed []string) error {

	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}

	for _, name := range pushed {
		b := g.Branches[name]
		if b == nil {
			continue
		}
		if prInfo.Branches[name] == nil {
			prInfo.Branches[name] = &store.BranchPR{}
		}
		pr := prInfo.Branches[name]

		// A frozen parent that was never published leaves this branch's PR base
		// naming a ref GitHub does not have. Say so against the frozen branch
		// rather than letting GitHub reject this one.
		if parent := g.Branches[b.ParentBranchName]; parent != nil && parent.Frozen {
			if exists, _ := c.Git.RemoteBranchExists(cfg.Remote, b.ParentBranchName); !exists {
				fmt.Printf("Warning: %s is based on frozen branch %s, which has never been pushed — "+
					"its PR cannot be based on it. Unfreeze and submit %s first.\n",
					name, b.ParentBranchName, b.ParentBranchName)
				continue
			}
		}

		if pr.Number != 0 && pr.BaseBranch != b.ParentBranchName {
			if err := ghUpdatePRBase(pr.Number, b.ParentBranchName); err != nil && c.Debug {
				fmt.Printf("Note: could not retarget PR #%d to %s yet: %v\n", pr.Number, b.ParentBranchName, err)
			}
		}
		pr.BaseBranch = b.ParentBranchName
		if pr.State == "" {
			pr.State = "open"
		}
	}

	if err := ensurePR(c, opts, g, prInfo, current); err != nil {
		return err
	}

	syncGitHubStacks(g, prInfo, pushed, c.Quiet, c.Interactive)

	if err := c.Store.WritePRInfo(prInfo); err != nil {
		return err
	}
	TryPushMeta(c)
	return nil
}

// ensurePR creates a pull request for the branch being submitted if it does not
// already have one.
func ensurePR(c *context.Context, opts SubmitOpts, g *graph.Graph, prInfo *store.PRInfo, current string) error {
	b := g.Branches[current]
	if b == nil || b.IsTrunk {
		return nil
	}

	existing, err := ghPRForBranch(current)
	if err != nil {
		return fmt.Errorf("failed to check PR status: %w", err)
	}
	if existing != nil {
		if prInfo.Branches[current] == nil {
			prInfo.Branches[current] = &store.BranchPR{}
		}
		pr := prInfo.Branches[current]
		pr.Number = existing.Number
		pr.URL = existing.URL
		pr.State = existing.State
		pr.Title = existing.Title
		pr.Draft = existing.Draft
		if !c.Quiet {
			fmt.Printf("PR #%d for %s (%s)\n", existing.Number, current, existing.URL)
		}
		return nil
	}

	title, body := opts.Title, opts.Body

	if title == "" {
		if !c.Interactive {
			if !c.Quiet {
				fmt.Println("Non-interactive mode: pushed without creating a PR")
			}
			return nil
		}

		choice, err := ui.Select("No PR exists for "+current, []string{"Push only", "Create PR"})
		if err != nil {
			return err
		}
		if choice == "Push only" {
			return nil
		}

		// Consume a sandbox-deposited PR Suggestion (reserved `pr` context
		// entry) as editable defaults, if present (ADR-0010).
		sugTitle, sugBody, hasSug := lookupPRSuggestion(b)
		if hasSug && !c.Quiet {
			fmt.Println("Using PR suggestion from branch context (edit as needed).")
		}
		prompt := "PR title"
		if sugTitle != "" {
			prompt = fmt.Sprintf("PR title [%s]", sugTitle)
		}
		title, err = ui.Input(prompt)
		if err != nil {
			return err
		}
		if title == "" {
			title = sugTitle
		}
		if title == "" {
			return fmt.Errorf("PR title cannot be empty")
		}
		body, err = ui.EditText(sugBody)
		if err != nil {
			return fmt.Errorf("editor failed: %w", err)
		}
	}

	result, err := ghCreatePR(GHCreateOpts{
		Base:  b.ParentBranchName,
		Head:  current,
		Title: title,
		Body:  body,
		Draft: opts.Draft,
	})
	if err != nil {
		return err
	}
	storePRResult(prInfo, current, b.ParentBranchName, result, opts.Draft)
	if !c.Quiet {
		fmt.Printf("Created PR #%d: %s\n", result.Number, result.URL)
	}
	return nil
}

// submitAIPrepare gathers context and outputs JSON to stdout.
func submitAIPrepare(c *context.Context) error {
	result, err := PrepareAI(c)
	if err != nil {
		return err
	}
	result.Prompt = BuildAISystemPrompt()
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal aiprepare result: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

// submitAI spawns a Claude session to generate and submit a PR.
func submitAI(c *context.Context, opts SubmitOpts) error {
	if _, err := exec.LookPath("claude"); err != nil {
		return fmt.Errorf("claude CLI not found — install it from https://claude.ai/code")
	}

	result, err := PrepareAI(c)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal context: %w", err)
	}

	systemPrompt := BuildAISystemPrompt()
	goal := "/goal PR is created with a title and description, and the branch is pushed"
	if opts.DryRun {
		goal += ". This is a dry-run — show what you would do but do not push or create the PR"
	}
	if opts.Draft {
		goal += ". Mark the PR as a draft (add --draft flag)"
	}

	args := []string{
		"--bare",
		"-p", goal,
		"--allowedTools", "Read,Edit,Bash(sr *),Bash(git *),Bash(gh *)",
		"--append-system-prompt", systemPrompt,
	}

	cmd := exec.Command("claude", args...)
	cmd.Stdin = strings.NewReader(string(data))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if !c.Quiet {
		fmt.Println("Launching Claude to generate and submit PR...")
	}

	return cmd.Run()
}

// storePRResult updates local PR metadata from a GitHub PR result.
func storePRResult(prInfo *store.PRInfo, branch, parent string, result *PRResult, draft bool) {
	if prInfo.Branches[branch] == nil {
		prInfo.Branches[branch] = &store.BranchPR{}
	}
	pr := prInfo.Branches[branch]
	pr.Number = result.Number
	pr.URL = result.URL
	pr.State = result.State
	pr.Title = result.Title
	pr.Draft = draft
	pr.BaseBranch = parent
}
