package engine

import (
	"fmt"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/git"
)

// ModifyOpts holds options for modifying the current branch.
type ModifyOpts struct {
	Message    string
	All        bool // -a: stage all
	Edit       bool // -e: open editor
	NewCommit  bool // -c: create new commit instead of amending
	NoVerify   bool
	ResetAuthor bool
}

// Modify amends the current branch's commit (or creates a new one) and restacks descendants.
func Modify(c *context.Context, opts ModifyOpts) error {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}

	SaveUndoPoint(c, "modify", "")

	current, err := c.Git.CurrentBranch()
	if err != nil {
		return err
	}

	if g.IsTrunk(current) {
		return fmt.Errorf("cannot modify trunk branch")
	}

	if !g.Has(current) {
		return fmt.Errorf("branch %q not tracked", current)
	}

	// Stage changes.
	if opts.All {
		if err := c.Git.AddAll(); err != nil {
			return err
		}
	}

	// Create the commit.
	commitOpts := git.CommitOpts{
		Edit: opts.Edit,
	}

	if opts.NewCommit {
		if opts.Message == "" {
			return fmt.Errorf("message required for new commit (-m)")
		}
		if err := c.Git.Commit(opts.Message, commitOpts); err != nil {
			return err
		}
	} else {
		commitOpts.Amend = true
		if opts.Message == "" {
			commitOpts.NoEdit = true
		}
		if err := c.Git.Commit(opts.Message, commitOpts); err != nil {
			return err
		}
	}

	// Update branch revision in graph.
	newRev, err := c.Git.RevParse("HEAD")
	if err != nil {
		return err
	}
	g.Branches[current].BranchRevision = newRev

	if err := c.Store.WriteGraph(g); err != nil {
		return err
	}

	// Restack descendants.
	children := g.ChildrenOf(current)
	if len(children) > 0 {
		if !c.Quiet {
			fmt.Println("Restacking descendants...")
		}
		return Restack(c, RestackOpts{Branch: current})
	}

	return nil
}
