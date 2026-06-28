package engine

import (
	"fmt"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/store"
)

// RestackOpts controls restacking behavior.
type RestackOpts struct {
	Branch   string // Specific branch to restack (empty = current)
	Downstack bool  // Restack downstack only
	Upstack  bool   // Restack upstack only
	Only     bool   // Restack only this branch (not descendants)
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

	// Determine which branches to restack.
	var toRestack []string
	if opts.Only {
		if !g.IsTrunk(branch) {
			toRestack = []string{branch}
		}
	} else {
		// Restack upstack from current branch (or its children if on trunk).
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

	return restackBranches(c, toRestack, origBranch)
}

// restackBranches performs the actual rebase of each branch onto its parent.
func restackBranches(c *context.Context, branches []string, origBranch string) error {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}

	for i, name := range branches {
		b := g.Branches[name]
		if b == nil || b.IsTrunk {
			continue
		}

		parentRev, err := c.Git.RevParse(b.ParentBranchName)
		if err != nil {
			return fmt.Errorf("cannot resolve parent %s: %w", b.ParentBranchName, err)
		}

		// If the parent hasn't moved, skip.
		if parentRev == b.ParentBranchRevision {
			continue
		}

		if !c.Quiet {
			fmt.Printf("Restacking %s onto %s\n", name, b.ParentBranchName)
		}

		// Rebase: --onto <new parent tip> <old parent rev> <branch>
		err = c.Git.RebaseOnto(b.ParentBranchName, b.ParentBranchRevision, name)
		if err != nil {
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

		// Update the graph with new revisions.
		newRev, err := c.Git.RevParse(name)
		if err != nil {
			return err
		}
		b.ParentBranchRevision = parentRev
		b.BranchRevision = newRev
	}

	// Write updated graph.
	if err := c.Store.WriteGraph(g); err != nil {
		return err
	}

	// Return to original branch.
	if origBranch != "" {
		_ = c.Git.Checkout(origBranch)
	}

	return nil
}
