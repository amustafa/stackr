package engine

import (
	"fmt"

	"github.com/amustafa/stackr/internal/context"
)

// MoveOpts holds options for moving a branch to a new parent.
type MoveOpts struct {
	Onto   string // Target parent branch
	Source string // Branch to move (default: current)
}

// Move rebases a branch onto a new parent.
func Move(c *context.Context, opts MoveOpts) error {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}

	source := opts.Source
	if source == "" {
		source, err = c.Git.CurrentBranch()
		if err != nil {
			return err
		}
	}

	if g.IsTrunk(source) {
		return fmt.Errorf("cannot move trunk")
	}

	b := g.Branches[source]
	if b == nil {
		return fmt.Errorf("branch %q not tracked", source)
	}

	onto := opts.Onto
	if onto == "" {
		return fmt.Errorf("--onto is required")
	}
	if !g.Has(onto) {
		return fmt.Errorf("target branch %q not tracked", onto)
	}

	oldParent := b.ParentBranchName
	oldParentRev := b.ParentBranchRevision

	// Perform the rebase.
	if !c.Quiet {
		fmt.Printf("Moving %s onto %s\n", source, onto)
	}

	if err := c.Git.RebaseOnto(onto, oldParentRev, source); err != nil {
		return fmt.Errorf("rebase failed: %w", err)
	}

	// Update graph: remove from old parent's children, add to new parent.
	oldP := g.Branches[oldParent]
	if oldP != nil {
		oldP.Children = removeFromSlice(oldP.Children, source)
	}

	newP := g.Branches[onto]
	newP.Children = append(newP.Children, source)

	newParentRev, _ := c.Git.RevParse(onto)
	newBranchRev, _ := c.Git.RevParse(source)
	b.ParentBranchName = onto
	b.ParentBranchRevision = newParentRev
	b.BranchRevision = newBranchRev

	return c.Store.WriteGraph(g)
}

func removeFromSlice(s []string, val string) []string {
	result := make([]string, 0, len(s))
	for _, v := range s {
		if v != val {
			result = append(result, v)
		}
	}
	return result
}
