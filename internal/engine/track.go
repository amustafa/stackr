package engine

import (
	"fmt"

	"github.com/amustafa/stackr/internal/context"
)

// Track starts tracking a branch in the stack graph.
func Track(c *context.Context, name, parent string, force bool) error {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}

	if name == "" {
		name, err = c.Git.CurrentBranch()
		if err != nil {
			return err
		}
	}

	if g.Has(name) && !force {
		return fmt.Errorf("branch %q already tracked (use -f to re-track)", name)
	}

	if parent == "" {
		parent = g.TrunkName()
	}
	if !g.Has(parent) {
		return fmt.Errorf("parent branch %q not tracked", parent)
	}

	branchRev, err := c.Git.RevParse(name)
	if err != nil {
		return fmt.Errorf("cannot resolve %s: %w", name, err)
	}
	parentRev, err := c.Git.RevParse(parent)
	if err != nil {
		return fmt.Errorf("cannot resolve %s: %w", parent, err)
	}

	// If re-tracking, remove first.
	if g.Has(name) {
		_ = g.RemoveBranch(name)
	}

	if err := g.AddBranch(name, parent, parentRev, branchRev); err != nil {
		return err
	}

	if err := c.Store.WriteGraph(g); err != nil {
		return err
	}

	if !c.Quiet {
		fmt.Printf("Tracking %s (parent: %s)\n", name, parent)
	}
	return nil
}

// Untrack stops tracking a branch.
func Untrack(c *context.Context, name string, force bool) error {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}

	if name == "" {
		name, err = c.Git.CurrentBranch()
		if err != nil {
			return err
		}
	}

	if !g.Has(name) {
		return fmt.Errorf("branch %q not tracked", name)
	}
	if g.IsTrunk(name) {
		return fmt.Errorf("cannot untrack trunk branch")
	}

	children := g.ChildrenOf(name)
	if len(children) > 0 && !force {
		return fmt.Errorf("branch %q has children — use -f to force untrack (children will be reparented)", name)
	}

	if err := g.RemoveBranch(name); err != nil {
		return err
	}

	if err := c.Store.WriteGraph(g); err != nil {
		return err
	}

	if !c.Quiet {
		fmt.Printf("Untracked %s\n", name)
	}
	return nil
}

// Freeze marks a branch as frozen (skipped during restack).
func Freeze(c *context.Context, name string) error {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}
	if name == "" {
		name, err = c.Git.CurrentBranch()
		if err != nil {
			return err
		}
	}
	b := g.Branches[name]
	if b == nil {
		return fmt.Errorf("branch %q not tracked", name)
	}
	b.Frozen = true
	if err := c.Store.WriteGraph(g); err != nil {
		return err
	}
	fmt.Printf("Frozen %s\n", name)
	return nil
}

// Unfreeze removes the frozen flag from a branch.
func Unfreeze(c *context.Context, name string) error {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}
	if name == "" {
		name, err = c.Git.CurrentBranch()
		if err != nil {
			return err
		}
	}
	b := g.Branches[name]
	if b == nil {
		return fmt.Errorf("branch %q not tracked", name)
	}
	b.Frozen = false
	if err := c.Store.WriteGraph(g); err != nil {
		return err
	}
	fmt.Printf("Unfrozen %s\n", name)
	return nil
}
