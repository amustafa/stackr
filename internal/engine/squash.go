package engine

import (
	"fmt"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/git"
	"github.com/amustafa/stackr/internal/graph"
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

	// Soft reset to this branch's BASE, then recommit.
	//
	// Resetting to the parent branch *name* is wrong whenever the parent has
	// moved since this branch was last restacked: the parent's tip is then not
	// an ancestor of HEAD, so `reset --soft` leaves the index holding the old
	// tree while HEAD jumps to the new parent. The commit that follows has a
	// diff of old-branch-tree against new-parent-tree — which silently REVERTS
	// the parent's newer work inside the squashed commit.
	//
	// The recorded base is an ancestor of HEAD by construction, so the squashed
	// commit contains exactly this branch's own work and nothing else.
	base, err := resolveBase(c, current, b)
	if err != nil {
		return err
	}
	if base.Recovered() && !c.Quiet {
		fmt.Printf("Note: recorded base for %s was unusable; recovered %s from %s's reflog\n",
			current, abbrev(base.SHA), b.ParentBranchName)
	}

	if err := c.Git.RunGit("reset", "--soft", base.SHA); err != nil {
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
	// HEAD is now base.SHA plus exactly one commit, so the base is unchanged in
	// meaning — but persist it in case it was recovered rather than recorded.
	b.ParentBranchRevision = base.SHA

	// Merge all commit contexts into the squashed commit.
	if len(b.CommitContexts) > 0 {
		shortRev := newRev[:min(7, len(newRev))]
		merged := make(map[string][]graph.BranchContext)
		for _, entries := range b.CommitContexts {
			merged[shortRev] = append(merged[shortRev], entries...)
		}
		b.CommitContexts = merged
	}

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
