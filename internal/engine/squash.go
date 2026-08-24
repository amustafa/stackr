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

	// Restack rebases the branch's descendants after squashing. Squashing
	// rewrites history, so without it descendants keep building on the
	// pre-squash commits until the next `sr restack`.
	Restack bool

	// Stack squashes every branch in the current stack, bottom to top,
	// restacking each branch onto its freshly squashed parent before squashing
	// it. Implies the restacking that Restack asks for, branch by branch.
	Stack bool
}

// Squash squashes all commits in the current branch into one.
func Squash(c *context.Context, opts SquashOpts) error {
	if opts.Stack {
		return squashStack(c, opts)
	}

	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}

	current, err := c.Git.CurrentBranch()
	if err != nil {
		return err
	}

	if err := squashCurrent(c, g, current, opts); err != nil {
		return err
	}

	children := g.ChildrenOf(current)
	if len(children) == 0 {
		return nil
	}
	if opts.Restack {
		return Restack(c, RestackOpts{Branch: current})
	}
	if !c.Quiet {
		fmt.Printf("Note: %d descendant branch(es) still build on the pre-squash commits — run `sr restack` (or use `sr squash --restack` next time).\n", len(children))
	}
	return nil
}

// squashCurrent squashes all commits of branch — which must be checked out —
// into one. Callers own the checkout and any restacking policy.
func squashCurrent(c *context.Context, g *graph.Graph, branch string, opts SquashOpts) error {
	if g.IsTrunk(branch) {
		return fmt.Errorf("cannot squash trunk")
	}

	b := g.Branches[branch]
	if b == nil {
		return fmt.Errorf("branch %q not tracked", branch)
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
	base, err := resolveBase(c, branch, b)
	if err != nil {
		return err
	}
	if base.Recovered() && !c.Quiet {
		fmt.Printf("Note: recorded base for %s was unusable; recovered %s from %s's reflog\n",
			branch, abbrev(base.SHA), b.ParentBranchName)
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
		msg = fmt.Sprintf("squash: %s", branch)
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
		fmt.Printf("Squashed commits in %s\n", branch)
	}
	return nil
}

// squashStack squashes every branch in the current stack, bottom to top.
//
// The order is squash-then-restack, interleaved: squash A, restack B onto the
// squashed A, squash B, restack C onto the squashed B, squash C. Restacking a
// child before squashing it is what keeps each squash a pure collapse of that
// branch's own commits — squashing first and restacking the whole stack after
// would work too, but a conflict would then surface against a parent whose
// shape the user has already lost to the squash.
func squashStack(c *context.Context, opts SquashOpts) error {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}

	current, err := c.Git.CurrentBranch()
	if err != nil {
		return err
	}
	if g.IsTrunk(current) {
		return fmt.Errorf("cannot squash trunk — check out a branch in the stack first")
	}
	if !g.Has(current) {
		return fmt.Errorf("branch %q not tracked", current)
	}

	// The sweep checks out every branch in turn, so start from a clean tree —
	// a dirty tree would either block a checkout mid-sweep or, worse, be
	// swept into some branch's squash commit by the soft reset.
	if dirty, derr := c.Git.IsDirty(); derr != nil {
		return derr
	} else if dirty {
		return fmt.Errorf("working tree has uncommitted changes — commit or stash them before `sr squash --stack`")
	}

	// Bottom of the stack: the last non-trunk ancestor. UpstackTopo from there
	// yields the whole stack above it, parents before children.
	ds := g.Downstack(current)
	bottom := current
	for _, name := range ds {
		if !g.IsTrunk(name) {
			bottom = name
		}
	}
	order := append([]string{bottom}, g.UpstackTopo(bottom)...)

	// Squashing needs the branch checked out here; a branch living in another
	// worktree cannot be. Refuse up front rather than dying mid-sweep with
	// half the stack rewritten.
	for _, name := range order {
		if wt, _ := c.Git.WorktreeForBranch(name); wt != "" && !sameWorktree(wt, c.Git.Dir) {
			return fmt.Errorf("%s is checked out in worktree %s — close that worktree (or `sr squash` there) first", name, wt)
		}
	}

	for _, name := range order {
		// The frozen wall (ADR-0015): --stack sweeps over branches the user
		// never named, so a frozen branch is left exactly as it is — not
		// restacked, not squashed. Its children still restack fine: the frozen
		// parent hasn't moved, so their restack is a no-op.
		if fb := g.Branches[name]; fb != nil && fb.Frozen {
			if !c.Quiet {
				fmt.Printf("Skipping %s (frozen)\n", name)
			}
			continue
		}

		// Restack this branch onto its parent — which the previous iteration
		// may have just rewritten — before squashing it.
		if err := Restack(c, RestackOpts{Branch: name, Only: true}); err != nil {
			return fmt.Errorf("%w\nAfter `sr continue`, rerun `sr squash --stack` to finish the sweep — already-squashed branches are skipped.", err)
		}

		// Re-read: the restack just rewrote revisions.
		g, err = c.Store.ReadGraph()
		if err != nil {
			return err
		}
		b := g.Branches[name]
		if b == nil {
			continue
		}

		// A branch already down to one commit has nothing to squash, and
		// rewriting it anyway would clobber its commit message with the
		// default. This is also what makes an interrupted sweep safe to rerun.
		base, err := resolveBase(c, name, b)
		if err != nil {
			return err
		}
		count, err := c.Git.RunGitCapture("rev-list", "--count", base.SHA+".."+name)
		if err != nil {
			return err
		}
		if count == "0" || count == "1" {
			if !c.Quiet {
				fmt.Printf("%s already a single commit — restacked only\n", name)
			}
			continue
		}

		if err := c.Git.Checkout(name); err != nil {
			return err
		}
		if err := squashCurrent(c, g, name, opts); err != nil {
			return err
		}
	}

	// Return to where the user was.
	if cur, _ := c.Git.CurrentBranch(); cur != current {
		if err := c.Git.Checkout(current); err != nil {
			return err
		}
	}
	return nil
}
