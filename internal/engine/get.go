package engine

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/amustafa/stackr/internal/context"
	gitpkg "github.com/amustafa/stackr/internal/git"
	"github.com/amustafa/stackr/internal/graph"
	"github.com/amustafa/stackr/internal/store"
	"github.com/amustafa/stackr/internal/ui"
)

type GetOpts struct {
	Branch        string
	Downstack     bool
	RemoteUpstack bool
	Worktree      bool
	Stay          bool
	Force         bool
}

type GetResult struct {
	NavigateResult NavigateResult
	Synced         []string
	Skipped        []string
	Created        []string
	Conflicts      bool
}

func Get(c *context.Context, opts GetOpts) (*GetResult, error) {
	if c.Store.HasRebaseState() {
		return nil, fmt.Errorf("a rebase is in progress — run `sr continue` or `sr abort` first")
	}
	if c.Store.HasGetState() {
		return nil, fmt.Errorf("a get operation is in progress — run `sr continue` or `sr abort` first")
	}

	cfg, err := c.Store.ReadConfig()
	if err != nil {
		return nil, err
	}

	if !c.Quiet {
		fmt.Printf("Fetching from %s...\n", cfg.Remote)
	}
	if err := c.Git.Fetch(cfg.Remote); err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}
	TryPullMeta(c)

	g, err := c.Store.ReadGraph()
	if err != nil {
		return nil, err
	}

	origBranch, _ := c.Git.CurrentBranch()

	target, err := resolveTarget(c, g, cfg, opts.Branch, opts.Force)
	if err != nil {
		return nil, err
	}

	walkPath := computeWalkPath(g, target)

	result := &GetResult{}

	if err := syncBranches(c, g, cfg, walkPath, origBranch, target, opts, result); err != nil {
		return result, err
	}

	if !opts.Downstack && !result.Conflicts {
		if err := syncUpstack(c, g, cfg, target, origBranch, opts, result); err != nil {
			return result, err
		}
	}

	if err := c.Store.WriteGraph(g); err != nil {
		return result, err
	}

	if result.Conflicts {
		return result, nil
	}

	if !opts.Stay {
		nav, err := navigateToTarget(c, target, opts.Worktree)
		if err != nil {
			return result, err
		}
		result.NavigateResult = nav
	}

	if !c.Quiet {
		printGetSummary(result)
	}

	return result, nil
}

func resolveTarget(c *context.Context, g *graph.Graph, cfg *store.Config, branch string, force bool) (string, error) {
	if branch == "" {
		current, err := c.Git.CurrentBranch()
		if err != nil {
			return "", err
		}
		if !g.Has(current) {
			return "", fmt.Errorf("current branch %q not tracked", current)
		}
		return current, nil
	}

	if num, err := strconv.Atoi(branch); err == nil {
		resolved := resolvePRNumber(c, num)
		if resolved != "" {
			branch = resolved
		} else {
			return "", fmt.Errorf("could not resolve PR #%d to a branch", num)
		}
	}

	if g.Has(branch) {
		return branch, nil
	}

	remoteExists, _ := c.Git.RemoteBranchExists(cfg.Remote, branch)
	if !remoteExists {
		return "", fmt.Errorf("branch %q not found locally or on remote %s", branch, cfg.Remote)
	}

	if !c.Interactive || force {
		if err := trackOnTrunk(c, g, cfg, branch); err != nil {
			return "", err
		}
		return branch, nil
	}

	choice, err := ui.Select(
		fmt.Sprintf("Branch %q exists on remote but is not tracked. What would you like to do?", branch),
		[]string{
			"Stack on trunk",
			"Skip (don't track)",
		},
	)
	if err != nil {
		return "", err
	}

	switch choice {
	case "Stack on trunk":
		if err := trackOnTrunk(c, g, cfg, branch); err != nil {
			return "", err
		}
		return branch, nil
	default:
		return "", fmt.Errorf("skipped — branch %q not tracked", branch)
	}
}

func trackOnTrunk(c *context.Context, g *graph.Graph, cfg *store.Config, branch string) error {
	localExists, _ := c.Git.BranchExists(branch)
	if !localExists {
		if err := c.Git.RunGit("branch", branch, cfg.Remote+"/"+branch); err != nil {
			return fmt.Errorf("creating local branch %s: %w", branch, err)
		}
	}

	branchRev, err := c.Git.RevParse(branch)
	if err != nil {
		return err
	}
	trunk := g.TrunkName()
	trunkRev, err := c.Git.RevParse(trunk)
	if err != nil {
		return err
	}

	if g.Has(branch) {
		_ = g.RemoveBranch(branch)
	}
	return g.AddBranch(branch, trunk, trunkRev, branchRev)
}

func resolvePRNumber(c *context.Context, num int) string {
	prInfo, err := c.Store.ReadPRInfo()
	if err == nil {
		if branch := prInfo.BranchForPR(num); branch != "" {
			return branch
		}
	}

	out, err := exec.Command("gh", "pr", "view", strconv.Itoa(num), "--json", "headRefName", "-q", ".headRefName").Output()
	if err == nil {
		branch := strings.TrimSpace(string(out))
		if branch != "" {
			return branch
		}
	}

	return ""
}

func computeWalkPath(g *graph.Graph, target string) []string {
	ds := g.Downstack(target)
	reversed := make([]string, 0, len(ds))
	for i := len(ds) - 1; i >= 0; i-- {
		if !g.IsTrunk(ds[i]) {
			reversed = append(reversed, ds[i])
		}
	}
	return reversed
}

func syncBranches(c *context.Context, g *graph.Graph, cfg *store.Config, walkPath []string, origBranch, target string, opts GetOpts, result *GetResult) error {
	for _, branch := range walkPath {
		action, err := syncOneBranch(c, g, cfg, branch, opts)
		if err != nil {
			if gitpkg.IsMergeConflict(err) {
				saveGetState(c, origBranch, target, walkPath, result.Synced, branch, opts)
				result.Conflicts = true
				fmt.Printf("\nMerge conflict on %s. Resolve conflicts, then run `sr continue`.\n", branch)
				return nil
			}
			return err
		}
		switch action {
		case syncActionSynced:
			result.Synced = append(result.Synced, branch)
		case syncActionCreated:
			result.Created = append(result.Created, branch)
		case syncActionSkipped:
			result.Skipped = append(result.Skipped, branch)
		}
	}
	return nil
}

type syncAction int

const (
	syncActionSkipped syncAction = iota
	syncActionSynced
	syncActionCreated
)

func syncOneBranch(c *context.Context, g *graph.Graph, cfg *store.Config, branch string, opts GetOpts) (syncAction, error) {
	remoteExists, _ := c.Git.RemoteBranchExists(cfg.Remote, branch)
	if !remoteExists {
		if !c.Quiet {
			fmt.Printf("  %s: not on remote, skipping\n", branch)
		}
		return syncActionSkipped, nil
	}

	localExists, _ := c.Git.BranchExists(branch)
	if !localExists {
		if err := c.Git.RunGit("branch", branch, cfg.Remote+"/"+branch); err != nil {
			return 0, fmt.Errorf("creating local branch %s: %w", branch, err)
		}
		rev, _ := c.Git.RevParse(branch)
		if b := g.Branches[branch]; b != nil {
			b.BranchRevision = rev
		}
		if !c.Quiet {
			fmt.Printf("  %s: created from remote\n", branch)
		}
		return syncActionCreated, nil
	}

	localRev, err := c.Git.RevParse(branch)
	if err != nil {
		return 0, err
	}
	remoteRef := cfg.Remote + "/" + branch
	remoteRev, err := c.Git.RevParse(remoteRef)
	if err != nil {
		return 0, err
	}

	if localRev == remoteRev {
		if !c.Quiet {
			fmt.Printf("  %s: up to date\n", branch)
		}
		return syncActionSkipped, nil
	}

	isLocalAnc, _ := c.Git.IsAncestor(localRev, remoteRev)
	if isLocalAnc {
		if err := handleWorktreeBranch(c, branch); err != nil {
			if !c.Quiet {
				fmt.Printf("  %s: skipped (worktree: %v)\n", branch, err)
			}
			return syncActionSkipped, nil
		}
		if err := c.Git.MergeFF(branch, remoteRef); err != nil {
			return 0, err
		}
		newRev, _ := c.Git.RevParse(branch)
		if b := g.Branches[branch]; b != nil {
			b.BranchRevision = newRev
		}
		if !c.Quiet {
			fmt.Printf("  %s: fast-forwarded\n", branch)
		}
		return syncActionSynced, nil
	}

	isRemoteAnc, _ := c.Git.IsAncestor(remoteRev, localRev)
	if isRemoteAnc {
		if !c.Quiet {
			fmt.Printf("  %s: local is ahead, skipping\n", branch)
		}
		return syncActionSkipped, nil
	}

	return handleDivergence(c, g, cfg, branch, remoteRef, opts)
}

func handleDivergence(c *context.Context, g *graph.Graph, cfg *store.Config, branch, remoteRef string, opts GetOpts) (syncAction, error) {
	if opts.Force {
		return replaceWithRemote(c, g, branch, remoteRef)
	}

	if !c.Interactive {
		if !c.Quiet {
			fmt.Printf("  %s: diverged (non-interactive, skipping)\n", branch)
		}
		return syncActionSkipped, nil
	}

	choice, err := ui.Select(
		fmt.Sprintf("Branch %q has diverged from remote. What would you like to do?", branch),
		[]string{
			"Replace with remote",
			"Keep local",
			"Merge remote into local",
		},
	)
	if err != nil {
		return 0, err
	}

	switch choice {
	case "Replace with remote":
		return replaceWithRemote(c, g, branch, remoteRef)
	case "Merge remote into local":
		return mergeFromRemote(c, g, branch, remoteRef)
	default:
		if !c.Quiet {
			fmt.Printf("  %s: keeping local version\n", branch)
		}
		return syncActionSkipped, nil
	}
}

func replaceWithRemote(c *context.Context, g *graph.Graph, branch, remoteRef string) (syncAction, error) {
	remoteRev, _ := c.Git.RevParse(remoteRef)

	current, _ := c.Git.CurrentBranch()
	if current == branch {
		if err := c.Git.RunGit("reset", "--hard", remoteRef); err != nil {
			return 0, err
		}
	} else {
		if err := c.Git.RunGit("update-ref", "refs/heads/"+branch, remoteRev); err != nil {
			return 0, err
		}
	}

	if b := g.Branches[branch]; b != nil {
		b.BranchRevision = remoteRev
	}
	if !c.Quiet {
		fmt.Printf("  %s: replaced with remote\n", branch)
	}
	return syncActionSynced, nil
}

func mergeFromRemote(c *context.Context, g *graph.Graph, branch, remoteRef string) (syncAction, error) {
	current, _ := c.Git.CurrentBranch()
	if current != branch {
		if err := c.Git.Checkout(branch); err != nil {
			return 0, err
		}
	}

	if err := c.Git.Merge(remoteRef); err != nil {
		return 0, err
	}

	newRev, _ := c.Git.RevParse(branch)
	if b := g.Branches[branch]; b != nil {
		b.BranchRevision = newRev
	}
	if !c.Quiet {
		fmt.Printf("  %s: merged from remote\n", branch)
	}
	return syncActionSynced, nil
}

func handleWorktreeBranch(c *context.Context, branch string) error {
	wtPath, err := c.Git.WorktreeForBranch(branch)
	if err != nil || wtPath == "" {
		return nil
	}

	absPath := wtPath
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(c.Git.Dir, absPath)
	}

	wtRunner := *c.Git
	wtRunner.Dir = absPath

	dirty, err := wtRunner.IsDirty()
	if err != nil {
		return nil
	}
	if !dirty {
		return nil
	}

	if !c.Interactive {
		return fmt.Errorf("dirty worktree at %s", absPath)
	}

	choice, err := ui.Select(
		fmt.Sprintf("Worktree for %q at %s has uncommitted changes.", branch, absPath),
		[]string{
			"Stash and continue",
			"Skip this branch",
		},
	)
	if err != nil {
		return err
	}

	if choice == "Skip this branch" {
		return fmt.Errorf("skipped by user")
	}

	if err := wtRunner.StashPush("sr get: stash for sync of " + branch); err != nil {
		return fmt.Errorf("stash failed: %w", err)
	}

	return nil
}

func syncUpstack(c *context.Context, g *graph.Graph, cfg *store.Config, target, origBranch string, opts GetOpts, result *GetResult) error {
	upstack := g.Upstack(target)
	if len(upstack) <= 1 {
		return nil
	}

	for _, branch := range upstack[1:] {
		if opts.RemoteUpstack {
			localExists, _ := c.Git.BranchExists(branch)
			if !localExists {
				remoteExists, _ := c.Git.RemoteBranchExists(cfg.Remote, branch)
				if remoteExists {
					if err := c.Git.RunGit("branch", branch, cfg.Remote+"/"+branch); err == nil {
						rev, _ := c.Git.RevParse(branch)
						if b := g.Branches[branch]; b != nil {
							b.BranchRevision = rev
						}
						result.Created = append(result.Created, branch)
						if !c.Quiet {
							fmt.Printf("  %s: created from remote (upstack)\n", branch)
						}
						continue
					}
				}
			}
		}

		localExists, _ := c.Git.BranchExists(branch)
		if !localExists {
			continue
		}

		action, err := syncOneBranch(c, g, cfg, branch, opts)
		if err != nil {
			if gitpkg.IsMergeConflict(err) {
				allBranches := append(computeWalkPath(g, target), upstack[1:]...)
				saveGetState(c, origBranch, target, allBranches, result.Synced, branch, opts)
				result.Conflicts = true
				fmt.Printf("\nMerge conflict on %s (upstack). Resolve conflicts, then run `sr continue`.\n", branch)
				return nil
			}
			return err
		}
		switch action {
		case syncActionSynced:
			result.Synced = append(result.Synced, branch)
		case syncActionCreated:
			result.Created = append(result.Created, branch)
		case syncActionSkipped:
			result.Skipped = append(result.Skipped, branch)
		}
	}
	return nil
}

func saveGetState(c *context.Context, origBranch, target string, walkPath, completed []string, currentBranch string, opts GetOpts) {
	gs := &store.GetState{
		Operation:     "get",
		OrigBranch:    origBranch,
		Target:        target,
		WalkPath:      walkPath,
		Completed:     completed,
		CurrentBranch: currentBranch,
		Flags: store.GetFlags{
			Downstack:     opts.Downstack,
			RemoteUpstack: opts.RemoteUpstack,
			Worktree:      opts.Worktree,
			Stay:          opts.Stay,
			Force:         opts.Force,
		},
	}
	_ = c.Store.WriteGetState(gs)
}

func navigateToTarget(c *context.Context, target string, worktree bool) (NavigateResult, error) {
	if worktree {
		wtPath, err := c.Git.WorktreeForBranch(target)
		if err != nil {
			return NavigateResult{}, err
		}
		if wtPath == "" {
			if err := WorktreeAdd(c, WorktreeAddOpts{Name: target}); err != nil {
				return NavigateResult{}, fmt.Errorf("creating worktree for %s: %w", target, err)
			}
			wtPath, _ = c.Git.WorktreeForBranch(target)
		}
		return NavigateResult{Branch: target, WorktreePath: wtPath}, nil
	}

	return NavigateToBranch(c, target)
}

func printGetSummary(result *GetResult) {
	parts := []string{}
	if len(result.Synced) > 0 {
		parts = append(parts, fmt.Sprintf("%d synced", len(result.Synced)))
	}
	if len(result.Created) > 0 {
		parts = append(parts, fmt.Sprintf("%d created", len(result.Created)))
	}
	if len(result.Skipped) > 0 {
		parts = append(parts, fmt.Sprintf("%d up-to-date", len(result.Skipped)))
	}
	if len(parts) > 0 {
		fmt.Printf("Get complete: %s\n", strings.Join(parts, ", "))
	}
}
