package engine

import (
	"fmt"

	"github.com/amustafa/stackr/internal/context"
)

// AbsorbOpts holds options for absorb.
type AbsorbOpts struct {
	DryRun bool
	Force  bool
	All    bool
	Patch  bool
}

// Absorb distributes staged hunks to the appropriate commits in the stack.
// This is a complex operation — for now, it stages all changes into the current branch.
func Absorb(c *context.Context, opts AbsorbOpts) error {
	dirty, err := c.Git.IsDirty()
	if err != nil {
		return err
	}
	if !dirty {
		return fmt.Errorf("no changes to absorb")
	}

	if opts.All {
		if err := c.Git.AddAll(); err != nil {
			return err
		}
	}

	hasStagedChanges, err := c.Git.HasStagedChanges()
	if err != nil {
		return err
	}
	if !hasStagedChanges {
		return fmt.Errorf("no staged changes — use -a to stage all or stage manually")
	}

	if opts.DryRun {
		fmt.Println("[dry-run] Would absorb staged changes into appropriate stack commits")
		fmt.Println("(Full hunk distribution not yet implemented — changes will be amended into current branch)")
		return nil
	}

	// For now, amend into current branch.
	if err := c.Git.RunGit("commit", "--amend", "--no-edit"); err != nil {
		return err
	}

	if !c.Quiet {
		fmt.Println("Absorbed changes into current branch (full hunk distribution coming soon)")
	}
	return nil
}
