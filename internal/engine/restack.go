package engine

import (
	"fmt"
	"path/filepath"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/store"
)

// RestackOpts controls restacking behavior.
type RestackOpts struct {
	Branch    string // Specific branch to restack (empty = current)
	Downstack bool   // Restack downstack only
	Upstack   bool   // Restack upstack only
	Only      bool   // Restack only this branch (not descendants)

	// SkipBlocked, when true, keeps restacking independent branches when one
	// branch cannot be cleanly restacked (its worktree is dirty, or the rebase
	// conflicts). The blocked branch and everything stacked on top of it are
	// left as-is instead of halting the whole operation. Used by `sr sync`.
	SkipBlocked bool

	// Base explicitly re-points the target branch's base commit before
	// restacking. This is the escape hatch for a branch whose base was lost or
	// corrupted beyond automatic recovery — see BaseUnresolvedError.
	Base string
}

// Restack rebases branches so they're correctly stacked on their parents.
func Restack(c *context.Context, opts RestackOpts) error {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}

	branch := opts.Branch
	if branch == "" {
		branch, err = c.Git.CurrentBranch()
		if err != nil {
			return err
		}
	}

	if !g.Has(branch) {
		return fmt.Errorf("branch %q not tracked", branch)
	}

	// An explicit --base repairs the recorded pointer before anything reads it.
	if opts.Base != "" {
		if err := setBase(c, g, branch, opts.Base); err != nil {
			return err
		}
		if err := c.Store.WriteGraph(g); err != nil {
			return err
		}
	}

	// Determine which branches to restack.
	// Each mode must yield branches in an order where a branch's parent is
	// restacked before the branch itself (parents-first / trunk-first), and
	// must never include trunk.
	var toRestack []string
	switch {
	case opts.Only:
		// Just this branch, nothing else.
		if !g.IsTrunk(branch) {
			toRestack = []string{branch}
		}

	case opts.Downstack:
		// This branch plus its ancestors toward trunk — nothing upstack.
		// g.Downstack gives [branch, parent, ..., trunk] (child-first); reverse
		// it so parents restack before their children, and drop trunk.
		ds := g.Downstack(branch)
		for i := len(ds) - 1; i >= 0; i-- {
			if !g.IsTrunk(ds[i]) {
				toRestack = append(toRestack, ds[i])
			}
		}

	case opts.Upstack:
		// This branch plus everything upstack (its descendants).
		toRestack = g.UpstackTopo(branch)
		if !g.IsTrunk(branch) {
			toRestack = append([]string{branch}, toRestack...)
		}

	default:
		// No scope flag: restack the current branch and everything upstack.
		toRestack = g.UpstackTopo(branch)
		if !g.IsTrunk(branch) {
			toRestack = append([]string{branch}, toRestack...)
		}
	}

	if len(toRestack) == 0 {
		if !c.Quiet {
			fmt.Println("Nothing to restack")
		}
		return nil
	}

	// Remember where we are.
	origBranch, _ := c.Git.CurrentBranch()

	// Freezing withdraws a branch from operations that sweep over it, not from
	// the user's direct instruction. Naming a branch as the subject of the
	// command is an explicit instruction, so it is exempt from the frozen wall
	// (ADR-0015).
	exempt := map[string]bool{}
	if opts.Branch != "" || opts.Only {
		exempt[branch] = true
	}

	return restackBranches(c, toRestack, origBranch, opts.SkipBlocked, exempt)
}

// skippedBranch records a branch that could not be cleanly restacked.
type skippedBranch struct {
	name   string
	reason string
}

// restackBranches performs the actual rebase of each branch onto its parent,
// in topological order (parents before children).
//
// When skipBlocked is true, a branch that cannot be cleanly restacked — because
// its worktree has uncommitted changes, or the rebase conflicts — is skipped
// along with every branch stacked on top of it, and the remaining independent
// branches are still restacked. When skipBlocked is false, the first conflict
// halts the operation: a merge conflict in the current worktree is saved so
// `sr continue` can resume it, and any other failure is returned as-is.
//
// A frozen branch is a wall for this operation regardless of skipBlocked
// (ADR-0015): it is not rebased, and neither is anything stacked on it, because
// replaying dependents onto a parent tip that was deliberately left in place is
// meaningless. Freezing is an intention rather than a failure, so it is always
// reported and never returns an error. Branches in exempt were named explicitly
// by the user and are restacked even when frozen.
func restackBranches(c *context.Context, branches []string, origBranch string, skipBlocked bool, exempt map[string]bool) error {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}

	// Worktree root of the current process — branches checked out here are
	// rebased in place; branches checked out elsewhere are rebased in their
	// own worktree to avoid git's "already used by worktree" lock.
	curRoot := c.Git.Dir

	blocked := map[string]bool{} // branch -> its lineage cannot be restacked
	var restacked []string
	var skipped []skippedBranch

	for i, name := range branches {
		b := g.Branches[name]
		if b == nil || b.IsTrunk {
			continue
		}

		// If an ancestor was blocked, this branch can't move either.
		if blocked[b.ParentBranchName] {
			blocked[name] = true
			skipped = append(skipped, skippedBranch{name, "stacked on a branch that was left unrestacked"})
			continue
		}

		// The frozen wall. Marking it blocked is what propagates the exclusion
		// up its lineage, reusing the same mechanism as a conflict.
		if b.Frozen && !exempt[name] {
			blocked[name] = true
			skipped = append(skipped, skippedBranch{name, "frozen"})
			continue
		}

		parentRev, err := c.Git.RevParse(b.ParentBranchName)
		if err != nil {
			return fmt.Errorf("cannot resolve parent %s: %w", b.ParentBranchName, err)
		}

		// Is the branch already built on the parent's current tip? Ask git
		// rather than comparing recorded revisions — the graph can be stale or
		// wrong, git's ancestry cannot. This also self-heals the recorded base:
		// if parentRev is an ancestor of the branch then, by definition, the
		// branch's own commits are exactly (parentRev, name].
		if isStackedOn(c, name, parentRev) {
			b.ParentBranchRevision = parentRev
			if newRev, rerr := c.Git.RevParse(name); rerr == nil {
				b.BranchRevision = newRev
			}
			continue
		}

		// Determine which commits actually belong to this branch before moving
		// anything. Getting this wrong is what replays a parent's superseded
		// commits on top of its rewritten ones.
		base, err := resolveBase(c, name, b)
		if err != nil {
			if skipBlocked {
				blocked[name] = true
				skipped = append(skipped, skippedBranch{name, "base commit could not be determined"})
				continue
			}
			return err
		}
		if base.Recovered() && !c.Quiet {
			fmt.Printf("Note: recorded base for %s was unusable; recovered %s from %s's reflog\n",
				name, abbrev(base.SHA), b.ParentBranchName)
		}

		if !c.Quiet {
			fmt.Printf("Restacking %s onto %s\n", name, b.ParentBranchName)
		}

		wtPath, _ := c.Git.WorktreeForBranch(name)
		inOtherWorktree := wtPath != "" && !sameWorktree(wtPath, curRoot)

		if inOtherWorktree {
			// Rebase inside the worktree that holds the branch. We refuse to
			// touch a worktree with uncommitted changes.
			runner := *c.Git
			runner.Dir = wtPath

			if dirty, derr := runner.IsDirty(); derr != nil || dirty {
				reason := fmt.Sprintf("uncommitted changes in worktree %s", wtPath)
				if derr != nil {
					reason = fmt.Sprintf("could not inspect worktree %s: %v", wtPath, derr)
				}
				if skipBlocked {
					blocked[name] = true
					skipped = append(skipped, skippedBranch{name, reason})
					continue
				}
				_ = c.Store.WriteGraph(g)
				return fmt.Errorf("cannot restack %s: %s", name, reason)
			}

			if err := runner.RebaseOnto(b.ParentBranchName, base.SHA, name); err != nil {
				// A conflict in another worktree can't be resumed via
				// `sr continue` (rebase state lives in the shared git dir but
				// the rebase is in that worktree), so abort to leave it clean.
				_ = runner.RebaseAbort()
				reason := fmt.Sprintf("conflict — restack manually in worktree %s", wtPath)
				if skipBlocked {
					blocked[name] = true
					skipped = append(skipped, skippedBranch{name, reason})
					continue
				}
				_ = c.Store.WriteGraph(g)
				return fmt.Errorf("cannot restack %s: %s", name, reason)
			}
		} else {
			// Rebase: --onto <new parent tip> <old parent rev> <branch>
			if err := c.Git.RebaseOnto(b.ParentBranchName, base.SHA, name); err != nil {
				// A rebase can fail two ways: it PAUSED on a merge conflict (a
				// rebase is now in progress and `sr continue` can resume it), or
				// it never started at all (a precondition fatal). Only the
				// former is a genuine, resumable conflict.
				if !c.Git.IsRebaseInProgress() {
					_ = c.Store.WriteGraph(g)
					return fmt.Errorf("cannot restack %s onto %s: %w", name, b.ParentBranchName, err)
				}
				// Genuine merge conflict in the current worktree.
				if skipBlocked {
					_ = c.Git.RebaseAbort()
					blocked[name] = true
					skipped = append(skipped, skippedBranch{name, "merge conflict"})
					continue
				}
				// Persist what already succeeded before bailing out. Branches
				// earlier in the loop have genuinely moved, and dropping their
				// new revisions leaves the graph claiming a base that the branch
				// no longer sits on — so the NEXT restack replays commits the
				// branch already contains and conflicts for no reason.
				if werr := c.Store.WriteGraph(g); werr != nil {
					return werr
				}

				// Conflict — save state for `sr continue`.
				rs := &store.RebaseState{
					Operation:     "restack",
					OrigBranch:    origBranch,
					Pending:       branches[i+1:],
					Completed:     branches[:i],
					CurrentBranch: name,
				}
				_ = c.Store.WriteRebaseState(rs)
				return fmt.Errorf("conflict while restacking %s — resolve and run `sr continue`", name)
			}
		}

		// Update the graph with new revisions.
		newRev, err := c.Git.RevParse(name)
		if err != nil {
			return err
		}
		b.ParentBranchRevision = parentRev
		b.BranchRevision = newRev
		restacked = append(restacked, name)
	}

	// Write updated graph.
	if err := c.Store.WriteGraph(g); err != nil {
		return err
	}

	// Return to original branch.
	if origBranch != "" {
		_ = c.Git.Checkout(origBranch)
	}

	// Frozen branches are reported whether or not skipBlocked is set: an
	// operation that quietly treats one branch differently from its neighbours
	// is indistinguishable from a bug (ADR-0015).
	if !c.Quiet && len(skipped) > 0 && (skipBlocked || anyFrozen(skipped)) {
		fmt.Printf("\nRestacked %d branch(es); left %d unrestacked:\n", len(restacked), len(skipped))
		for _, s := range skipped {
			fmt.Printf("  - %s (%s)\n", s.name, s.reason)
		}
	}

	return nil
}

func anyFrozen(skipped []skippedBranch) bool {
	for _, s := range skipped {
		if s.reason == "frozen" {
			return true
		}
	}
	return false
}

// sameWorktree reports whether two worktree paths refer to the same directory,
// tolerating trailing slashes and symlinked paths.
func sameWorktree(a, b string) bool {
	if a == b {
		return true
	}
	ca, cb := filepath.Clean(a), filepath.Clean(b)
	if ca == cb {
		return true
	}
	ra, aerr := filepath.EvalSymlinks(ca)
	rb, berr := filepath.EvalSymlinks(cb)
	return aerr == nil && berr == nil && ra == rb
}
