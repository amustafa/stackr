package engine

import (
	"fmt"
	"path/filepath"

	"github.com/amustafa/stackr/internal/context"
	srerr "github.com/amustafa/stackr/internal/errors"
	"github.com/amustafa/stackr/internal/ui"
)

// ErrMultipleChildren signals a fork in the stack where the user must choose.
type ErrMultipleChildren struct {
	Current  string
	Children []string
}

func (e *ErrMultipleChildren) Error() string {
	return fmt.Sprintf("branch %q has %d children", e.Current, len(e.Children))
}

// NavigateResult describes what happened after a navigation request.
type NavigateResult struct {
	// Branch is the target branch name.
	Branch string
	// WorktreePath is set when the branch lives in a worktree.
	// The caller (shell hook) should cd to this path.
	WorktreePath string
}

// IsWorktree returns true if navigation requires a directory change.
func (r NavigateResult) IsWorktree() bool {
	return r.WorktreePath != ""
}

// CheckoutBranch switches to a branch by name.
func CheckoutBranch(c *context.Context, name string) error {
	if err := c.RequireInit(); err != nil {
		return err
	}
	return c.Git.Checkout(name)
}

// NavigateToBranch checks if the target branch is in a worktree. If so, it
// returns a NavigateResult with the worktree path (and optionally moves dirty
// changes via stash). Otherwise it performs a normal checkout.
func NavigateToBranch(c *context.Context, target string) (NavigateResult, error) {
	wtPath, err := c.Git.WorktreeForBranch(target)
	if err != nil {
		return NavigateResult{}, err
	}

	// Not in a worktree — normal checkout.
	if wtPath == "" {
		if err := c.Git.Checkout(target); err != nil {
			return NavigateResult{}, err
		}
		return NavigateResult{Branch: target}, nil
	}

	// If the worktree path matches our current working directory, the branch
	// is already checked out here — just do a normal checkout (no cd needed).
	absWtPath := wtPath
	if !filepath.IsAbs(absWtPath) {
		absWtPath = filepath.Join(c.Git.Dir, absWtPath)
	}
	if absWtPath == c.Git.Dir {
		if err := c.Git.Checkout(target); err != nil {
			return NavigateResult{}, err
		}
		return NavigateResult{Branch: target}, nil
	}

	// Branch is in a different worktree. Check for local dirty state.
	dirty, err := c.Git.IsDirty()
	if err != nil {
		return NavigateResult{}, err
	}

	if dirty && c.Interactive {
		choice, err := ui.Select(
			"You have uncommitted changes. What would you like to do?",
			[]string{
				"Leave changes here",
				"Move changes to worktree (stash + pop)",
			},
		)
		if err != nil {
			return NavigateResult{}, err
		}
		if choice == "Move changes to worktree (stash + pop)" {
			if err := c.Git.StashPush("sr: moving changes to worktree " + target); err != nil {
				return NavigateResult{}, fmt.Errorf("stash failed: %w", err)
			}

			// Pop in the worktree by temporarily pointing a runner at it.
			wtRunner := *c.Git
			absPath := wtPath
			if !filepath.IsAbs(absPath) {
				absPath = filepath.Join(c.Git.Dir, absPath)
			}
			wtRunner.Dir = absPath
			if err := wtRunner.StashPop(); err != nil {
				return NavigateResult{}, fmt.Errorf("stash pop in worktree failed (changes saved in stash): %w", err)
			}
		}
	} else if dirty && !c.Interactive {
		// Non-interactive: leave changes, just switch.
		if !c.Quiet {
			fmt.Println("Note: uncommitted changes left in current directory")
		}
	}

	return NavigateResult{Branch: target, WorktreePath: wtPath}, nil
}

// NavigateUp moves N branches upstack.
// Returns ErrMultipleChildren if a fork is encountered.
func NavigateUp(c *context.Context, n int) (NavigateResult, error) {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return NavigateResult{}, err
	}
	current, err := c.Git.CurrentBranch()
	if err != nil {
		return NavigateResult{}, err
	}
	if !g.Has(current) {
		return NavigateResult{}, srerr.ErrBranchNotFound
	}

	target := current
	for i := 0; i < n; i++ {
		children := g.ChildrenOf(target)
		switch len(children) {
		case 0:
			if target == current {
				return NavigateResult{}, srerr.ErrNoChildren
			}
			// Ran out of children before completing all steps; stop here.
			return NavigateToBranch(c, target)
		case 1:
			target = children[0]
		default:
			return NavigateResult{}, &ErrMultipleChildren{Current: target, Children: children}
		}
	}
	if target == current {
		return NavigateResult{}, srerr.ErrNoChildren
	}
	return NavigateToBranch(c, target)
}

// NavigateDown moves N branches downstack (toward trunk).
func NavigateDown(c *context.Context, n int) (NavigateResult, error) {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return NavigateResult{}, err
	}
	current, err := c.Git.CurrentBranch()
	if err != nil {
		return NavigateResult{}, err
	}
	if !g.Has(current) {
		return NavigateResult{}, srerr.ErrBranchNotFound
	}
	if g.IsTrunk(current) {
		return NavigateResult{}, srerr.ErrOnTrunk
	}
	target := g.Down(current, n)
	if target == current {
		return NavigateResult{}, fmt.Errorf("already at bottom of stack")
	}
	return NavigateToBranch(c, target)
}

// NavigateTop moves to the tip of the current stack.
func NavigateTop(c *context.Context) (NavigateResult, error) {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return NavigateResult{}, err
	}
	current, err := c.Git.CurrentBranch()
	if err != nil {
		return NavigateResult{}, err
	}
	if !g.Has(current) {
		return NavigateResult{}, srerr.ErrBranchNotFound
	}
	target := g.Top(current)
	if target == current {
		fmt.Println("Already at the top of the stack")
		return NavigateResult{Branch: current}, nil
	}
	return NavigateToBranch(c, target)
}

// NavigateBottom moves to the bottom of the current stack (first above trunk).
func NavigateBottom(c *context.Context) (NavigateResult, error) {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return NavigateResult{}, err
	}
	current, err := c.Git.CurrentBranch()
	if err != nil {
		return NavigateResult{}, err
	}
	if !g.Has(current) {
		return NavigateResult{}, srerr.ErrBranchNotFound
	}
	target := g.Bottom(current)
	if target == current {
		fmt.Println("Already at the bottom of the stack")
		return NavigateResult{Branch: current}, nil
	}
	return NavigateToBranch(c, target)
}

// GetParent returns the parent branch name.
func GetParent(c *context.Context) (string, error) {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return "", err
	}
	current, err := c.Git.CurrentBranch()
	if err != nil {
		return "", err
	}
	parent := g.Parent(current)
	if parent == "" {
		return "", srerr.ErrNoParent
	}
	return parent, nil
}

// GetChildren returns the children branch names.
func GetChildren(c *context.Context) ([]string, error) {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return nil, err
	}
	current, err := c.Git.CurrentBranch()
	if err != nil {
		return nil, err
	}
	children := g.ChildrenOf(current)
	if len(children) == 0 {
		return nil, srerr.ErrNoChildren
	}
	return children, nil
}

// TrackedBranches returns all non-trunk branch names in the graph.
func TrackedBranches(c *context.Context) ([]string, error) {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return nil, err
	}
	var branches []string
	for name := range g.Branches {
		if !g.IsTrunk(name) {
			branches = append(branches, name)
		}
	}
	return branches, nil
}
