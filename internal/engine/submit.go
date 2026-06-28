package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/graph"
	"github.com/amustafa/stackr/internal/store"
	"github.com/amustafa/stackr/internal/ui"
)

// SubmitOpts controls push/PR behavior.
type SubmitOpts struct {
	Draft      bool
	Stack      bool // Push all branches in the stack
	UpdateOnly bool // Only update already-pushed branches
	Force      bool
	DryRun     bool
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

	if opts.Stack {
		return submitStack(c, opts, g, cfg, prInfo, current)
	}

	return submitSingle(c, opts, g, cfg, prInfo, current)
}

// submitAIPrepare gathers context and outputs JSON to stdout.
func submitAIPrepare(c *context.Context) error {
	result, err := PrepareAI(c)
	if err != nil {
		return err
	}
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

// submitStack pushes downstack ancestors, current branch, and upstack dependents.
func submitStack(c *context.Context, opts SubmitOpts, g *graph.Graph, cfg *store.Config, prInfo *store.PRInfo, current string) error {
	if err := pushDownstack(c, opts, g, cfg, prInfo, current); err != nil {
		return err
	}

	b := g.Branches[current]
	if err := pushBranch(c, cfg, opts, prInfo, current, b.ParentBranchName); err != nil {
		return err
	}

	if err := pushUpstack(c, opts, g, cfg, prInfo, current); err != nil {
		return err
	}

	if err := c.Store.WritePRInfo(prInfo); err != nil {
		return err
	}
	TryPushMeta(c)
	return nil
}

// submitSingle handles the single-branch submit flow.
func submitSingle(c *context.Context, opts SubmitOpts, g *graph.Graph, cfg *store.Config, prInfo *store.PRInfo, current string) error {
	b := g.Branches[current]
	if b == nil {
		return fmt.Errorf("branch %q not found in stack graph", current)
	}
	if b.IsTrunk {
		fmt.Println("Cannot submit trunk branch")
		return nil
	}

	if !c.Quiet {
		fmt.Printf("Submitting %s (base: %s)\n", current, b.ParentBranchName)
	}

	// Check if a PR already exists on GitHub.
	if opts.DryRun {
		// In dry-run mode, skip the gh check and just push.
		if !c.Quiet {
			fmt.Println("Dry-run mode, skipping PR check")
		}
		return submitNewBranch(c, opts, g, cfg, prInfo, current)
	}

	if !c.Quiet {
		fmt.Printf("Checking for existing PR on %s...\n", current)
	}
	existing, err := ghPRForBranch(current)
	if err != nil {
		return fmt.Errorf("failed to check PR status: %w", err)
	}

	if existing != nil {
		return submitExisting(c, opts, g, cfg, prInfo, current, existing)
	}

	if !c.Quiet {
		fmt.Printf("No existing PR found for %s\n", current)
	}
	return submitNewBranch(c, opts, g, cfg, prInfo, current)
}

// submitExisting handles the flow when a PR already exists:
// push downstack ancestors, push current, offer to push upstack.
func submitExisting(c *context.Context, opts SubmitOpts, g *graph.Graph, cfg *store.Config, prInfo *store.PRInfo, current string, existing *PRResult) error {
	fmt.Printf("PR #%d already exists for %s (%s)\n", existing.Number, current, existing.URL)

	if err := pushDownstack(c, opts, g, cfg, prInfo, current); err != nil {
		return err
	}

	if err := pushBranch(c, cfg, opts, prInfo, current, g.Branches[current].ParentBranchName); err != nil {
		return err
	}

	// Update local PR info from GitHub data.
	if prInfo.Branches[current] == nil {
		prInfo.Branches[current] = &store.BranchPR{}
	}
	pr := prInfo.Branches[current]
	pr.Number = existing.Number
	pr.URL = existing.URL
	pr.State = existing.State
	pr.Title = existing.Title
	pr.Draft = existing.Draft

	if err := offerUpstack(c, opts, g, cfg, prInfo, current); err != nil {
		return err
	}

	return c.Store.WritePRInfo(prInfo)
}

// submitNewBranch handles the flow when no PR exists yet.
func submitNewBranch(c *context.Context, opts SubmitOpts, g *graph.Graph, cfg *store.Config, prInfo *store.PRInfo, current string) error {
	b := g.Branches[current]

	// Push downstack ancestors first (always, all modes).
	if err := pushDownstack(c, opts, g, cfg, prInfo, current); err != nil {
		return err
	}

	// Programmatic mode: title and body provided, skip prompts.
	if opts.Title != "" {
		if err := pushBranch(c, cfg, opts, prInfo, current, b.ParentBranchName); err != nil {
			return err
		}
		if opts.DryRun {
			fmt.Printf("[dry-run] Would create PR: %q (base: %s, draft: %v)\n", opts.Title, b.ParentBranchName, opts.Draft)
		} else {
			result, err := ghCreatePR(GHCreateOpts{
				Base:  b.ParentBranchName,
				Head:  current,
				Title: opts.Title,
				Body:  opts.Body,
				Draft: opts.Draft,
			})
			if err != nil {
				return err
			}
			storePRResult(prInfo, current, b.ParentBranchName, result, opts.Draft)
			if !c.Quiet {
				fmt.Printf("Created PR #%d: %s\n", result.Number, result.URL)
			}
		}
		return c.Store.WritePRInfo(prInfo)
	}

	// Non-interactive: push only.
	if !c.Interactive {
		if !c.Quiet {
			fmt.Println("Non-interactive mode, pushing only")
		}
		if err := pushBranch(c, cfg, opts, prInfo, current, b.ParentBranchName); err != nil {
			return err
		}
		return c.Store.WritePRInfo(prInfo)
	}

	// Interactive mode: present options.
	const (
		optPushOnly = "Push only"
		optCreatePR = "Create PR"
	)

	choice, err := ui.Select("No PR exists for "+current, []string{optPushOnly, optCreatePR})
	if err != nil {
		return err
	}

	if err := pushBranch(c, cfg, opts, prInfo, current, b.ParentBranchName); err != nil {
		return err
	}

	switch choice {
	case optPushOnly:
		// Already pushed, just save metadata.

	case optCreatePR:
		title, err := ui.Input("PR title")
		if err != nil {
			return err
		}
		if title == "" {
			return fmt.Errorf("PR title cannot be empty")
		}

		body, err := ui.EditText("")
		if err != nil {
			return fmt.Errorf("editor failed: %w", err)
		}

		if opts.DryRun {
			fmt.Printf("[dry-run] Would create PR: %q (base: %s, draft: %v)\n", title, b.ParentBranchName, opts.Draft)
		} else {
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
		}
	}

	// Offer to push upstack.
	if err := offerUpstack(c, opts, g, cfg, prInfo, current); err != nil {
		return err
	}

	return c.Store.WritePRInfo(prInfo)
}

// pushDownstack pushes all downstack ancestors of current (excluding current and trunk).
func pushDownstack(c *context.Context, opts SubmitOpts, g *graph.Graph, cfg *store.Config, prInfo *store.PRInfo, current string) error {
	downstack := g.Downstack(current)
	// Downstack returns [current, parent, grandparent, ...trunk].
	// Push in bottom-up order (reverse), skip current (index 0) and trunk.
	var ancestors []string
	for i := len(downstack) - 1; i >= 1; i-- {
		a := g.Branches[downstack[i]]
		if a != nil && !a.IsTrunk && !a.Frozen {
			ancestors = append(ancestors, downstack[i])
		}
	}
	if len(ancestors) > 0 && !c.Quiet {
		fmt.Printf("Pushing %d downstack ancestor(s)\n", len(ancestors))
	}
	for _, name := range ancestors {
		a := g.Branches[name]
		if err := pushBranch(c, cfg, opts, prInfo, name, a.ParentBranchName); err != nil {
			return err
		}
	}
	return nil
}

// pushUpstack pushes all upstack dependents of current (excluding current).
func pushUpstack(c *context.Context, opts SubmitOpts, g *graph.Graph, cfg *store.Config, prInfo *store.PRInfo, current string) error {
	upstack := g.Upstack(current)
	if len(upstack) <= 1 {
		return nil
	}
	dependents := upstack[1:] // skip current
	if !c.Quiet {
		fmt.Printf("Pushing %d upstack dependent(s)\n", len(dependents))
	}
	for _, name := range dependents {
		ub := g.Branches[name]
		if ub == nil || ub.IsTrunk || ub.Frozen {
			continue
		}
		if err := pushBranch(c, cfg, opts, prInfo, name, ub.ParentBranchName); err != nil {
			return err
		}
	}
	return nil
}

// offerUpstack prompts to push upstack dependents (interactive mode only).
func offerUpstack(c *context.Context, opts SubmitOpts, g *graph.Graph, cfg *store.Config, prInfo *store.PRInfo, current string) error {
	if !c.Interactive {
		return nil
	}
	upstack := g.Upstack(current)
	if len(upstack) <= 1 {
		return nil
	}
	upstackCount := len(upstack) - 1
	yes, err := ui.Confirm(fmt.Sprintf("Push %d upstack branch(es) too?", upstackCount))
	if err != nil {
		return err
	}
	if yes {
		return pushUpstack(c, opts, g, cfg, prInfo, current)
	}
	return nil
}

// pushBranch pushes a single branch to the remote and records basic metadata.
func pushBranch(c *context.Context, cfg *store.Config, opts SubmitOpts, prInfo *store.PRInfo, name, parent string) error {
	if opts.DryRun {
		fmt.Printf("[dry-run] Would push %s to %s/%s (base: %s)\n", name, cfg.Remote, name, parent)
		return nil
	}

	if !c.Quiet {
		if opts.Force {
			fmt.Printf("Force pushing %s -> %s/%s (base: %s)\n", name, cfg.Remote, name, parent)
		} else {
			fmt.Printf("Pushing %s -> %s/%s (base: %s)\n", name, cfg.Remote, name, parent)
		}
	}

	if err := c.Git.PushWithUpstream(cfg.Remote, name, opts.Force); err != nil {
		return fmt.Errorf("failed to push %s: %w", name, err)
	}

	// Ensure PR metadata entry exists.
	if prInfo.Branches[name] == nil {
		prInfo.Branches[name] = &store.BranchPR{}
	}
	pr := prInfo.Branches[name]
	pr.BaseBranch = parent
	if pr.State == "" {
		pr.State = "open"
	}
	pr.Draft = opts.Draft

	return nil
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
