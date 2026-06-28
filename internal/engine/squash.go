package engine

import (
	"fmt"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/git"
)

// SquashOpts holds options for squashing.
type SquashOpts struct {
	Message string
	Edit    bool
	NoEdit  bool
}

// Squash squashes all commits in the current branch into one.
func Squash(c *context.Context, opts SquashOpts) error {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}

	current, err := c.Git.CurrentBranch()
	if err != nil {
		return err
	}

	if g.IsTrunk(current) {
		return fmt.Errorf("cannot squash trunk")
	}

	b := g.Branches[current]
	if b == nil {
		return fmt.Errorf("branch %q not tracked", current)
	}

	// Soft reset to parent, then recommit.
	if err := c.Git.RunGit("reset", "--soft", b.ParentBranchName); err != nil {
		return fmt.Errorf("reset failed: %w", err)
	}

	commitOpts := git.CommitOpts{
		Edit:   opts.Edit,
		NoEdit: opts.NoEdit,
	}

	msg := opts.Message
	if msg == "" {
		msg = fmt.Sprintf("squash: %s", current)
	}

	if err := c.Git.Commit(msg, commitOpts); err != nil {
		return err
	}

	// Update graph.
	newRev, err := c.Git.RevParse("HEAD")
	if err != nil {
		return err
	}
	b.BranchRevision = newRev

	if err := c.Store.WriteGraph(g); err != nil {
		return err
	}

	if !c.Quiet {
		fmt.Printf("Squashed commits in %s\n", current)
	}

	// Restack descendants.
	children := g.ChildrenOf(current)
	if len(children) > 0 {
		return Restack(c, RestackOpts{Branch: current})
	}

	return nil
}
