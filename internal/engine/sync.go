package engine

import (
	"fmt"
	"sort"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/graph"
)

// SyncOpts controls sync behavior.
type SyncOpts struct {
	Restack bool
	Force   bool
	All     bool
}

// Sync fetches trunk from remote, rebases onto it, restacks, and cleans merged branches.
func Sync(c *context.Context, opts SyncOpts) error {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}
	cfg, err := c.Store.ReadConfig()
	if err != nil {
		return err
	}

	trunk := g.TrunkName()
	origBranch, _ := c.Git.CurrentBranch()

	// Fetch from remote.
	if !c.Quiet {
		fmt.Printf("Fetching from %s...\n", cfg.Remote)
	}
	if err := c.Git.FetchPrune(cfg.Remote); err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}

	// Pull shared metadata (best-effort).
	TryPullMeta(c)

	// Checkout trunk and pull.
	if err := c.Git.Checkout(trunk); err != nil {
		return err
	}
	remoteTrunk := cfg.Remote + "/" + trunk
	if err := c.Git.RunGit("merge", "--ff-only", remoteTrunk); err != nil {
		return fmt.Errorf("could not fast-forward %s: %w", trunk, err)
	}

	// Update trunk revision in graph.
	trunkRev, err := c.Git.RevParse(trunk)
	if err != nil {
		return err
	}
	g.Branches[trunk].BranchRevision = trunkRev

	// Clean up merged branches.
	cleaned := cleanMergedBranches(c, g, trunk)
	for _, name := range cleaned {
		if !c.Quiet {
			fmt.Printf("Cleaned up merged branch: %s\n", name)
		}
	}

	if err := c.Store.WriteGraph(g); err != nil {
		return err
	}

	// Restack all stacks.
	if opts.Restack || opts.All {
		if !c.Quiet {
			fmt.Println("Restacking...")
		}
		if err := Restack(c, RestackOpts{Branch: trunk, SkipBlocked: true}); err != nil {
			return err
		}
	}

	// Return to original branch if it still exists.
	if origBranch != "" && origBranch != trunk {
		if g.Has(origBranch) {
			_ = c.Git.Checkout(origBranch)
		}
	}

	if !c.Quiet {
		fmt.Println("Sync complete")
	}
	return nil
}

// cleanMergedBranches removes branches whose work has landed on trunk.
//
// Ancestry alone is not enough. GitHub's "Squash and merge" and "Rebase and
// merge" rewrite the commits as they land, so the branch never becomes an
// ancestor of trunk and an ancestry test reports "not merged" forever. Those
// branches then survive into the restack below, which replays commits trunk
// already contains — producing a conflict on every commit, or a silent
// duplicate. That is the single most common way a stack gets mangled by sync.
//
// So three tests, cheapest and most authoritative first.
func cleanMergedBranches(c *context.Context, g *graph.Graph, trunk string) []string {
	// One batched query instead of one `gh pr view` per branch. Best-effort:
	// offline, or without gh, we fall through to the local patch-id test.
	mergedPRs := ghMergedHeadBranches()

	// Sort for deterministic output and stable parent-before-child removal.
	names := make([]string, 0, len(g.Branches))
	for name, b := range g.Branches {
		if !b.IsTrunk {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	var cleaned []string
	for _, name := range names {
		b := g.Branches[name]
		if b == nil {
			continue // already removed as part of an earlier reparent
		}

		if !branchHasLanded(c, name, b, trunk, mergedPRs) {
			continue
		}

		// Children are about to be reparented onto this branch's tip, which
		// becomes their base. Take the tip from git, not from the graph: a
		// stale BranchRevision here would hand every child a wrong base.
		if tip, err := c.Git.RevParse(name); err == nil {
			b.BranchRevision = tip
		}

		if err := g.RemoveBranch(name); err != nil {
			continue
		}
		_ = c.Git.DeleteBranch(name, true)
		cleaned = append(cleaned, name)
	}
	return cleaned
}

// branchHasLanded reports whether a branch's work is already present on trunk,
// by whichever merge strategy was used.
func branchHasLanded(c *context.Context, name string, b *graph.BranchState, trunk string, mergedPRs map[string]bool) bool {
	base, baseErr := resolveBase(c, name, b)

	// A branch with no commits of its own has not landed — it is simply empty.
	// Ancestry alone would call it merged, because its tip IS its parent's tip,
	// and sync would delete the branch the user created five seconds ago.
	if baseErr == nil {
		if has, err := c.Git.HasCommitsSince(base.SHA, name); err == nil && !has {
			return false
		}
	}

	// 1. Plain ancestry — a merge commit or fast-forward merge.
	if merged, err := c.Git.IsMergedInto(name, trunk); err == nil && merged {
		return true
	}

	// 2. The forge says the PR merged. Authoritative for squash and rebase
	//    merges, which rewrite commits and defeat every local test.
	if mergedPRs[name] {
		return true
	}

	// 3. Local fallback: every commit the branch owns already has a
	//    patch-equivalent commit on trunk. This needs a trustworthy base to
	//    know which commits the branch owns, so an unresolvable base means
	//    "not merged" — refusing to delete is the safe direction to be wrong in.
	if baseErr != nil {
		return false
	}
	landed, err := c.Git.AllCommitsUpstream(trunk, name, base.SHA)
	return err == nil && landed
}
