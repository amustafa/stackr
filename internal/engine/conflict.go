package engine

import (
	"fmt"

	"github.com/amustafa/stackr/internal/context"
)

// Continue resumes a conflicted operation after the user has resolved conflicts.
func Continue(c *context.Context) error {
	if !c.Store.HasRebaseState() {
		return fmt.Errorf("no operation in progress to continue")
	}

	rs, err := c.Store.ReadRebaseState()
	if err != nil {
		return err
	}

	// Continue the git rebase.
	if c.Git.IsRebaseInProgress() {
		if err := c.Git.RebaseContinue(); err != nil {
			return fmt.Errorf("rebase continue failed — resolve remaining conflicts and retry")
		}
	}

	// Update the graph for the branch we just finished rebasing.
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

	// Continue restacking remaining branches.
	if len(rs.Pending) > 0 {
		err := restackBranches(c, rs.Pending, rs.OrigBranch)
		if err != nil {
			return err
		}
	} else {
		// All done — return to original branch.
		if rs.OrigBranch != "" {
			_ = c.Git.Checkout(rs.OrigBranch)
		}
	}

	// Clean up state.
	return c.Store.ClearRebaseState()
}

// Abort cancels the current conflicted operation.
func Abort(c *context.Context) error {
	if !c.Store.HasRebaseState() {
		return fmt.Errorf("no operation in progress to abort")
	}

	rs, err := c.Store.ReadRebaseState()
	if err != nil {
		return err
	}

	// Abort git rebase if in progress.
	if c.Git.IsRebaseInProgress() {
		if err := c.Git.RebaseAbort(); err != nil {
			return err
		}
	}

	// Return to original branch.
	if rs.OrigBranch != "" {
		_ = c.Git.Checkout(rs.OrigBranch)
	}

	return c.Store.ClearRebaseState()
}
