package engine

import (
	"fmt"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/git"
)

// FoldOpts holds options for folding.
type FoldOpts struct {
	KeepName bool // Keep the current branch name instead of parent's
}

// Fold squashes the current branch into its parent branch.
func Fold(c *context.Context, opts FoldOpts) error {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}

	current, err := c.Git.CurrentBranch()
	if err != nil {
		return err
	}

	if g.IsTrunk(current) {
		return fmt.Errorf("cannot fold trunk")
	}

	b := g.Branches[current]
	if b == nil {
		return fmt.Errorf("branch %q not tracked", current)
	}

	parent := b.ParentBranchName

	// Switch to parent and merge current branch's changes.
	if err := c.Git.Checkout(parent); err != nil {
		return err
	}

	// Soft reset to incorporate the commits.
	if err := c.Git.RunGit("merge", "--squash", current); err != nil {
		return fmt.Errorf("merge --squash failed: %w", err)
	}

	if err := c.Git.Commit("", git.CommitOpts{Amend: true, NoEdit: true}); err != nil {
		// If amend fails (no staged changes), just commit.
		if err := c.Git.Commit(fmt.Sprintf("fold %s into %s", current, parent), git.CommitOpts{}); err != nil {
			return err
		}
	}

	// Reparent children of current to parent.
	if err := g.RemoveBranch(current); err != nil {
		return err
	}

	// Delete the git branch.
	_ = c.Git.DeleteBranch(current, true)

	// Update parent revision.
	newRev, err := c.Git.RevParse("HEAD")
	if err != nil {
		return err
	}
	g.Branches[parent].BranchRevision = newRev

	if err := c.Store.WriteGraph(g); err != nil {
		return err
	}

	if !c.Quiet {
		fmt.Printf("Folded %s into %s\n", current, parent)
	}
	return nil
}
