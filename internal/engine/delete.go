package engine

import (
	"fmt"
	"path/filepath"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/sandbox"
)

// DeleteOpts holds options for deleting a branch.
type DeleteOpts struct {
	Name      string
	Force     bool
	Upstack   bool // Delete all upstack branches too
	Downstack bool // Delete all downstack branches too
}

// Delete removes a branch from the stack, reparenting children to its parent.
//
// If the target is checked out in the current checkout, Delete first steps down
// the stack (like `sr down`) — the returned NavigateResult tells the shell hook
// where to cd if that lands in another worktree. If the target is checked out
// in a linked worktree, the worktree is removed first (refusing if it has
// uncommitted changes), since git cannot delete a checked-out branch.
func Delete(c *context.Context, opts DeleteOpts) (NavigateResult, error) {
	var nav NavigateResult

	g, err := c.Store.ReadGraph()
	if err != nil {
		return nav, err
	}

	name := opts.Name
	if name == "" {
		name, err = c.Git.CurrentBranch()
		if err != nil {
			return nav, err
		}
	}

	if g.IsTrunk(name) {
		return nav, fmt.Errorf("cannot delete trunk branch")
	}
	if !g.Has(name) {
		return nav, fmt.Errorf("branch %q not tracked", name)
	}

	targets := []string{name}
	if opts.Upstack {
		targets = nil
		for _, b := range g.Upstack(name) {
			if !g.IsTrunk(b) {
				targets = append(targets, b)
			}
		}
	}

	SaveUndoPoint(c, "delete", name)

	// If the current checkout holds a branch we're about to delete, step off it
	// by going down the stack first.
	current, _ := c.Git.CurrentBranch()
	deleting := make(map[string]bool, len(targets))
	for _, b := range targets {
		deleting[b] = true
	}
	if deleting[current] {
		parent := g.Parent(name)
		if parent == "" {
			parent = g.TrunkName()
		}
		if err := checkDeleteFromOwnWorktree(c, current, name, parent); err != nil {
			return nav, err
		}
		nav, err = NavigateToBranch(c, parent)
		if err != nil {
			return nav, err
		}
		// When the parent lives in another worktree, NavigateToBranch only
		// reports where to cd — this checkout is still on the doomed branch,
		// which git would refuse to delete. Park it on trunk.
		if stillOn, _ := c.Git.CurrentBranch(); deleting[stillOn] {
			if err := c.Git.Checkout(g.TrunkName()); err != nil {
				return nav, fmt.Errorf("moving this checkout off %q before deleting: %w", stillOn, err)
			}
		}
	}

	// Remove worktrees holding target branches before deleting: git refuses to
	// delete a branch that is checked out. Do this for every target up front so
	// a dirty worktree aborts the whole delete before any branch is removed.
	for _, b := range targets {
		if err := removeWorktreeHolding(c, b); err != nil {
			return nav, err
		}
	}

	// Delete leaves first so children are gone before their parents.
	for i := len(targets) - 1; i >= 0; i-- {
		b := targets[i]
		if err := g.RemoveBranch(b); err != nil {
			return nav, err
		}
		if err := c.Git.DeleteBranch(b, opts.Force); err != nil {
			return nav, fmt.Errorf("git branch delete failed: %w", err)
		}
		// Drop the Push Record too: a branch name that is later recreated must
		// not inherit a claim on a remote it never wrote (ADR-0014).
		_ = c.Store.DeletePushRecordsForBranch(b)
		if !c.Quiet {
			if opts.Upstack {
				fmt.Printf("Deleted %s\n", b)
			} else {
				fmt.Printf("Deleted %s (children reparented)\n", b)
			}
		}
	}

	return nav, c.Store.WriteGraph(g)
}

// checkDeleteFromOwnWorktree rejects deleting the current branch from inside
// its own linked worktree when the parent is checked out elsewhere. Stepping
// down would cd away, and removing the worktree we're running from would pull
// the directory out from under the rest of the operation.
func checkDeleteFromOwnWorktree(c *context.Context, current, name, parent string) error {
	mainRoot, err := deleteMainRoot(c)
	if err != nil {
		return err
	}
	if canonicalPath(c.Git.Dir) == mainRoot {
		return nil
	}
	// We're in a linked worktree holding a branch to delete. Stepping down
	// keeps this worktree alive only if the parent can be checked out here.
	parentWt, err := c.Git.WorktreeForBranch(parent)
	if err != nil {
		return err
	}
	if parentWt != "" {
		return fmt.Errorf("branch %q is checked out in this worktree and %q is checked out at %s; run `sr delete %s` from there or from the main checkout",
			current, parent, parentWt, name)
	}
	return nil
}

// removeWorktreeHolding removes the linked worktree that has branch checked
// out, if any. It refuses to touch a dirty worktree, the main checkout, or the
// worktree the command is running from.
func removeWorktreeHolding(c *context.Context, branch string) error {
	wtPath, err := c.Git.WorktreeForBranch(branch)
	if err != nil {
		return err
	}
	if wtPath == "" {
		return nil
	}

	absPath := wtPath
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(c.Git.Dir, absPath)
	}
	absPath = canonicalPath(absPath)

	mainRoot, err := deleteMainRoot(c)
	if err != nil {
		return err
	}
	if absPath == mainRoot {
		return fmt.Errorf("branch %q is checked out in the main checkout at %s; switch it to another branch first", branch, absPath)
	}
	if absPath == canonicalPath(c.Git.Dir) {
		return fmt.Errorf("branch %q is checked out in the worktree you are running from; run `sr delete %s` from the main checkout", branch, branch)
	}

	wtRunner := *c.Git
	wtRunner.Dir = absPath
	dirty, err := wtRunner.IsDirty()
	if err != nil {
		return fmt.Errorf("checking worktree at %s: %w", absPath, err)
	}
	if dirty {
		return fmt.Errorf("worktree for %q at %s has uncommitted changes; commit or stash them first", branch, absPath)
	}

	if err := c.Git.WorktreeRemove(absPath); err != nil {
		return fmt.Errorf("removing worktree at %s: %w", absPath, err)
	}
	if !c.Quiet {
		fmt.Printf("Removed worktree at %s\n", absPath)
	}
	return nil
}

// deleteMainRoot returns the canonical path of the main checkout.
func deleteMainRoot(c *context.Context) (string, error) {
	gitCommon, err := absGitCommonDir(c)
	if err != nil {
		return "", err
	}
	return canonicalPath(sandbox.MainRoot(gitCommon)), nil
}

// canonicalPath resolves symlinks best-effort so worktree paths reported by
// git compare equal to locally-derived paths (e.g. /tmp vs /private/tmp).
func canonicalPath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return filepath.Clean(p)
}
