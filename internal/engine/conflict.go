package engine

import (
	"fmt"

	"github.com/amustafa/stackr/internal/context"
	gitpkg "github.com/amustafa/stackr/internal/git"
	"github.com/amustafa/stackr/internal/store"
)

// Continue resumes a conflicted operation after the user has resolved conflicts.
func Continue(c *context.Context) error {
	if c.Store.HasGetState() {
		return continueGet(c)
	}

	if !c.Store.HasRebaseState() {
		return fmt.Errorf("no operation in progress to continue")
	}

	return continueRebase(c)
}

func continueGet(c *context.Context) error {
	gs, err := c.Store.ReadGetState()
	if err != nil {
		return err
	}

	if c.Git.IsMergeInProgress() {
		if err := c.Git.RunGit("commit", "--no-edit"); err != nil {
			return fmt.Errorf("merge commit failed — resolve remaining conflicts and retry")
		}
	}

	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}

	if b := g.Branches[gs.CurrentBranch]; b != nil {
		newRev, err := c.Git.RevParse(gs.CurrentBranch)
		if err == nil {
			b.BranchRevision = newRev
		}
	}

	remaining := remainingBranches(gs)

	cfg, err := c.Store.ReadConfig()
	if err != nil {
		return err
	}

	opts := GetOpts{
		Branch:        gs.Target,
		Downstack:     gs.Flags.Downstack,
		RemoteUpstack: gs.Flags.RemoteUpstack,
		Worktree:      gs.Flags.Worktree,
		Stay:          gs.Flags.Stay,
		Force:         gs.Flags.Force,
	}

	result := &GetResult{
		Synced:  gs.Completed,
		Created: []string{},
	}
	result.Synced = append(result.Synced, gs.CurrentBranch)

	for _, branch := range remaining {
		action, err := syncOneBranch(c, g, cfg, branch, opts)
		if err != nil {
			if gitpkg.IsMergeConflict(err) {
				gs.Completed = result.Synced
				gs.CurrentBranch = branch
				_ = c.Store.WriteGetState(gs)
				fmt.Printf("\nMerge conflict on %s. Resolve conflicts, then run `sr continue`.\n", branch)
				return nil
			}
			return err
		}
		switch action {
		case syncActionSynced:
			result.Synced = append(result.Synced, branch)
		case syncActionCreated:
			result.Created = append(result.Created, branch)
		case syncActionSkipped:
			result.Skipped = append(result.Skipped, branch)
		}
	}

	if err := c.Store.WriteGraph(g); err != nil {
		return err
	}

	if err := c.Store.ClearGetState(); err != nil {
		return err
	}

	if !opts.Stay {
		nav, err := navigateToTarget(c, gs.Target, opts.Worktree)
		if err != nil {
			return err
		}
		result.NavigateResult = nav
	}

	if !c.Quiet {
		printGetSummary(result)
	}

	return nil
}

func continueRebase(c *context.Context) error {
	rs, err := c.Store.ReadRebaseState()
	if err != nil {
		return err
	}

	if c.Git.IsRebaseInProgress() {
		if err := c.Git.RebaseContinue(); err != nil {
			return fmt.Errorf("rebase continue failed — resolve remaining conflicts and retry")
		}
	}

	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}

	if b := g.Branches[rs.CurrentBranch]; b != nil {
		newRev, err := c.Git.RevParse(rs.CurrentBranch)
		if err == nil {
			parentRev, err := c.Git.RevParse(b.ParentBranchName)
			if err == nil {
				b.BranchRevision = newRev
				b.ParentBranchRevision = parentRev
			}
		}
		if err := c.Store.WriteGraph(g); err != nil {
			return err
		}
	}

	if len(rs.Pending) > 0 {
		err := restackBranches(c, rs.Pending, rs.OrigBranch, false)
		if err != nil {
			return err
		}
	} else {
		if rs.OrigBranch != "" {
			_ = c.Git.Checkout(rs.OrigBranch)
		}
	}

	return c.Store.ClearRebaseState()
}

// Abort cancels the current conflicted operation.
func Abort(c *context.Context) error {
	if c.Store.HasGetState() {
		return abortGet(c)
	}

	if !c.Store.HasRebaseState() {
		return fmt.Errorf("no operation in progress to abort")
	}

	return abortRebase(c)
}

func abortGet(c *context.Context) error {
	gs, err := c.Store.ReadGetState()
	if err != nil {
		return err
	}

	if c.Git.IsMergeInProgress() {
		if err := c.Git.RunGit("merge", "--abort"); err != nil {
			return err
		}
	}

	if gs.OrigBranch != "" {
		_ = c.Git.Checkout(gs.OrigBranch)
	}

	return c.Store.ClearGetState()
}

func abortRebase(c *context.Context) error {
	rs, err := c.Store.ReadRebaseState()
	if err != nil {
		return err
	}

	if c.Git.IsRebaseInProgress() {
		if err := c.Git.RebaseAbort(); err != nil {
			return err
		}
	}

	if rs.OrigBranch != "" {
		_ = c.Git.Checkout(rs.OrigBranch)
	}

	return c.Store.ClearRebaseState()
}

func remainingBranches(gs *store.GetState) []string {
	currentIdx := -1
	for i, name := range gs.WalkPath {
		if name == gs.CurrentBranch {
			currentIdx = i
			break
		}
	}
	if currentIdx < 0 || currentIdx >= len(gs.WalkPath)-1 {
		return nil
	}
	return gs.WalkPath[currentIdx+1:]
}

