package engine

import (
	"fmt"

	"github.com/amustafa/stackr/internal/context"
)

// SplitOpts holds options for splitting.
type SplitOpts struct {
	ByCommit bool
	ByHunk   bool
	ByFile   bool
}

// Split splits the current branch into multiple branches.
// For now, implements by-commit splitting.
func Split(c *context.Context, opts SplitOpts) error {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}

	current, err := c.Git.CurrentBranch()
	if err != nil {
		return err
	}
	if g.IsTrunk(current) {
		return fmt.Errorf("cannot split trunk")
	}

	b := g.Branches[current]
	if b == nil {
		return fmt.Errorf("branch %q not tracked", current)
	}

	// Get commits in this branch.
	entries, err := c.Git.CommitsBetween(b.ParentBranchName, current)
	if err != nil {
		return err
	}
	if len(entries) <= 1 {
		return fmt.Errorf("branch has %d commit(s) — nothing to split", len(entries))
	}

	if !c.Quiet {
		fmt.Printf("Splitting %s into %d branches (one per commit)\n", current, len(entries))
	}

	// Save children to reparent later.
	children := b.Children

	// For each commit, create a new branch.
	parent := b.ParentBranchName
	parentRev := b.ParentBranchRevision

	// Remove current branch from graph.
	g.Branches[b.ParentBranchName].Children = removeFromSlice(
		g.Branches[b.ParentBranchName].Children, current,
	)

	// Process commits from oldest to newest (entries are newest first).
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		branchName := fmt.Sprintf("%s-part%d", current, len(entries)-i)
		if i == 0 {
			branchName = current // Keep original name for the last (most recent) commit
		}

		if i == 0 {
			// The current branch already has the right content.
			g.Branches[current].ParentBranchName = parent
			g.Branches[current].ParentBranchRevision = parentRev
			g.Branches[current].BranchRevision = e.SHA
			g.Branches[current].Children = children
			g.Branches[parent].Children = append(g.Branches[parent].Children, current)

			// Reparent children to point to current.
			for _, child := range children {
				g.Branches[child].ParentBranchName = current
				g.Branches[child].ParentBranchRevision = e.SHA
			}
		} else {
			// Create a new branch at this commit.
			if err := c.Git.CreateBranch(branchName, e.SHA); err != nil {
				return fmt.Errorf("failed to create branch %s: %w", branchName, err)
			}

			if err := g.AddBranch(branchName, parent, parentRev, e.SHA); err != nil {
				return err
			}

			parent = branchName
			parentRev = e.SHA
		}
	}

	if err := c.Store.WriteGraph(g); err != nil {
		return err
	}

	if !c.Quiet {
		fmt.Printf("Split complete\n")
	}
	return nil
}
