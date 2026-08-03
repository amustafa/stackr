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

	// Bring trunk up to date, without claiming it in this worktree.
	if err := fastForwardTrunk(c, cfg.Remote, trunk); err != nil {
		return err
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
			// Sync no longer parks this worktree on trunk, so usually we never
			// left. Only check out when something downstream (restack) moved us.
			if cur, cerr := c.Git.CurrentBranch(); cerr != nil || cur != origBranch {
				_ = c.Git.Checkout(origBranch)
			}
		} else if !c.Quiet {
			// origBranch was cleaned up as merged, so cleanMergedBranches
			// vacated this worktree off it — a process can't safely delete the
			// directory it's executing in. Surface that so a worktree that
			// existed only for this branch doesn't sit forgotten as a stray
			// checkout.
			if gitDir, derr := c.Git.GitDir(); derr == nil {
				if commonDir, cerr := c.Git.GitCommonDir(); cerr == nil && gitDir != commonDir {
					fmt.Printf("Note: %s was cleaned up as merged; this worktree no longer holds a tracked branch. Remove it with `sr worktree remove` if you no longer need it.\n", origBranch)
				}
			}
		}
	}

	if !c.Quiet {
		fmt.Println("Sync complete")
	}
	return nil
}

// fastForwardTrunk brings the local trunk ref up to date with the remote.
//
// Where trunk is checked out decides how. Checking it out unconditionally — what
// sync used to do — fails outright whenever another worktree already holds it,
// and that is the ordinary shape of a stackr repo: the main checkout sits on
// trunk while feature branches live in worktrees. Running `sr sync` from one of
// those worktrees died on git's "already used by worktree" before it fetched,
// restacked, or cleaned anything up.
func fastForwardTrunk(c *context.Context, remote, trunk string) error {
	remoteTrunk := remote + "/" + trunk

	// Trunk is checked out right here: an ff-only merge moves the ref and this
	// working tree together.
	if cur, err := c.Git.CurrentBranch(); err == nil && cur == trunk {
		if err := c.Git.RunGit("merge", "--ff-only", remoteTrunk); err != nil {
			return fmt.Errorf("could not fast-forward %s: %w", trunk, err)
		}
		return nil
	}

	// Another worktree holds trunk. Git refuses to update a ref checked out
	// elsewhere, so the merge has to run over there — the same way restack
	// rebases a branch inside the worktree that owns it.
	//
	// A failure here stops the sync rather than continuing on a stale trunk.
	// Everything downstream measures against trunk — which branches have landed,
	// what the stack rebases onto — so proceeding would quietly do less than the
	// user asked for and report success. Better to say why and let them clear
	// the blocking worktree.
	if wtPath, werr := c.Git.WorktreeForBranch(trunk); werr == nil && wtPath != "" && !sameWorktree(wtPath, c.Git.Dir) {
		runner := *c.Git
		runner.Dir = wtPath
		if err := runner.RunGit("merge", "--ff-only", remoteTrunk); err != nil {
			return fmt.Errorf("could not fast-forward %s in worktree %s: %w", trunk, wtPath, err)
		}
		return nil
	}

	// Trunk is checked out nowhere: fast-forward the ref in place, no checkout
	// at all. Fetching from "." reuses the remote-tracking ref updated by the
	// fetch above and keeps fast-forward-only semantics — a diverged trunk still
	// errors rather than being silently reset.
	if err := c.Git.RunGit("fetch", ".", remoteTrunk+":"+trunk); err != nil {
		return fmt.Errorf("could not fast-forward %s: %w", trunk, err)
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

		// A branch checked out anywhere can't be force-deleted — git refuses —
		// and even where it could, the worktree would be left behind as a
		// checkout of a branch that no longer exists in the graph. Which
		// worktree holds it decides what to do about that.
		//
		// Every failure below leaves the branch tracked and bails out. Git has
		// to let go of the branch before the graph does: reporting it cleaned
		// while git still holds it strands it — dropped from the graph, alive on
		// disk, restacked by nothing, and invisible to `sr log`.
		if wtPath, werr := c.Git.WorktreeForBranch(name); werr == nil && wtPath != "" {
			if sameWorktree(wtPath, c.Git.Dir) {
				// The worktree we're running from. A process can't delete the
				// directory it is executing in, so vacate the branch instead of
				// removing the worktree. Land on trunk when it is free; when
				// another worktree owns the trunk ref, detach at it instead —
				// attempting the checkout first would only spill git's
				// "already used by worktree" fatal into an otherwise fine sync.
				vacate := []string{"checkout", trunk}
				if trunkWt, terr := c.Git.WorktreeForBranch(trunk); terr == nil && trunkWt != "" {
					vacate = []string{"checkout", "--detach", trunk}
				}
				if err := c.Git.RunGit(vacate...); err != nil {
					if !c.Quiet {
						fmt.Printf("Note: %s is merged but checked out here and could not be vacated (%v); leaving the branch in place\n", name, err)
					}
					continue
				}
			} else if rmErr := c.Git.WorktreeRemove(wtPath); rmErr != nil {
				if !c.Quiet {
					fmt.Printf("Note: could not remove worktree %s for merged branch %s (%v); leaving it in place\n", wtPath, name, rmErr)
				}
				continue
			} else if !c.Quiet {
				fmt.Printf("Removed worktree for merged branch %s: %s\n", name, wtPath)
			}
		}

		if err := c.Git.DeleteBranch(name, true); err != nil {
			if !c.Quiet {
				fmt.Printf("Note: %s is merged but could not be deleted (%v); leaving it tracked\n", name, err)
			}
			continue
		}

		// Only now that git has released the branch may it leave the graph.
		// RemoveBranch reparents children onto the BranchRevision set above, and
		// can only fail for a branch that is missing or trunk — neither reachable
		// here, since trunk is filtered out of names.
		if err := g.RemoveBranch(name); err != nil {
			continue
		}

		// A recreated branch of the same name must start with no claim on the
		// remote (ADR-0014).
		_ = c.Store.DeletePushRecordsForBranch(name)
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
	if landed, err := c.Git.AllCommitsUpstream(trunk, name, base.SHA); err == nil && landed {
		return true
	}

	// 4. Local fallback for a squash merge: git cherry (used above) compares
	//    patch IDs one commit at a time, so it can't see a squash merge that
	//    collapsed this branch's several commits into trunk's one. Compare the
	//    branch's combined diff against each trunk commit's diff instead.
	landed, err := c.Git.SquashMergedUpstream(trunk, name, base.SHA)
	return err == nil && landed
}
