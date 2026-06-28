package engine

import (
	"fmt"

	"github.com/amustafa/stackr/internal/context"
)

// DeleteOpts holds options for deleting a branch.
type DeleteOpts struct {
	Name      string
	Force     bool
	Upstack   bool // Delete all upstack branches too
	Downstack bool // Delete all downstack branches too
}

// Delete removes a branch from the stack, reparenting children to its parent.
func Delete(c *context.Context, opts DeleteOpts) error {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}

	name := opts.Name
	if name == "" {
		name, err = c.Git.CurrentBranch()
		if err != nil {
			return err
		}
	}

	SaveUndoPoint(c, "delete", name)

	if g.IsTrunk(name) {
		return fmt.Errorf("cannot delete trunk branch")
	}
	if !g.Has(name) {
		return fmt.Errorf("branch %q not tracked", name)
	}

	// If deleting the current branch, switch to parent first.
	current, _ := c.Git.CurrentBranch()
	if current == name {
		parent := g.Parent(name)
		if parent != "" {
			if err := c.Git.Checkout(parent); err != nil {
				return err
			}
		}
	}

	if opts.Upstack {
		// Delete all branches upstack (leaves first).
		upstack := g.Upstack(name)
		for i := len(upstack) - 1; i >= 0; i-- {
			b := upstack[i]
			if g.IsTrunk(b) {
				continue
			}
			_ = g.RemoveBranch(b)
			_ = c.Git.DeleteBranch(b, opts.Force)
			if !c.Quiet {
				fmt.Printf("Deleted %s\n", b)
			}
		}
	} else {
		if err := g.RemoveBranch(name); err != nil {
			return err
		}
		if err := c.Git.DeleteBranch(name, opts.Force); err != nil {
			return fmt.Errorf("git branch delete failed: %w", err)
		}
		if !c.Quiet {
			fmt.Printf("Deleted %s (children reparented)\n", name)
		}
	}

	return c.Store.WriteGraph(g)
}
