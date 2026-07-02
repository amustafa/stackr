package engine

import (
	"fmt"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/git"
)

// CreateOpts holds options for creating a new stacked branch.
type CreateOpts struct {
	Name       string
	Message    string
	All        bool // -a: stage all tracked
	Untracked  bool // -u: stage untracked too
	Patch      bool // -p: interactive patch
	Insert     bool // -i: insert between current and its children
	NoVerify   bool
	Desc       string // branch description/objective
	Worktree   bool   // create in a worktree instead of checking out
	Stay       bool   // create branch without checking it out
}

// Create creates a new branch on top of the current branch.
func Create(c *context.Context, opts CreateOpts) error {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}

	SaveUndoPoint(c, "create", "")

	current, err := c.Git.CurrentBranch()
	if err != nil {
		return fmt.Errorf("cannot determine current branch: %w", err)
	}

	if !g.Has(current) {
		return fmt.Errorf("current branch %q is not tracked — run `sr track` first", current)
	}

	if opts.Name != "" {
		exists, err := c.Git.BranchExists(opts.Name)
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("branch %q already exists", opts.Name)
		}
	}

	// Stage changes if requested.
	if opts.All {
		if err := c.Git.AddAll(); err != nil {
			return err
		}
	} else if opts.Untracked {
		if err := c.Git.AddUpdate(); err != nil {
			return err
		}
	}

	if opts.Patch {
		if err := c.Git.RunGit("add", "-p"); err != nil {
			return err
		}
	}

	// Create the commit if there's a message (and staged changes or allow-empty).
	if opts.Message != "" {
		hasStagedChanges, err := c.Git.HasStagedChanges()
		if err != nil {
			return err
		}
		if !hasStagedChanges {
			return fmt.Errorf("no changes staged — use -a to stage all or stage manually")
		}

		commitOpts := git.CommitOpts{}
		if opts.NoVerify {
			// Runner.Verify controls --no-verify; set it via the flag.
		}
		if err := c.Git.Commit(opts.Message, commitOpts); err != nil {
			return err
		}
	}

	// Record the parent revision before branching.
	parentRev, err := c.Git.RevParse(current)
	if err != nil {
		return err
	}

	// Determine branch name.
	branchName := opts.Name
	if branchName == "" {
		return fmt.Errorf("branch name is required — pass it as an argument or let create generate one")
	}

	// Create the branch. --stay uses CreateBranch (no checkout); default uses CheckoutNew.
	var branchRev string
	if opts.Stay {
		if err := c.Git.CreateBranch(branchName, ""); err != nil {
			return err
		}
		branchRev = parentRev
	} else {
		if err := c.Git.CheckoutNew(branchName, ""); err != nil {
			return err
		}
		branchRev, err = c.Git.RevParse("HEAD")
		if err != nil {
			return err
		}
	}

	// If insert mode, reparent current branch's children to the new branch.
	if opts.Insert {
		children := g.ChildrenOf(current)
		for _, child := range children {
			g.Branches[child].ParentBranchName = branchName
			g.Branches[child].ParentBranchRevision = branchRev
		}
		// Add to graph with children inherited.
		if err := g.AddBranch(branchName, current, parentRev, branchRev); err != nil {
			return err
		}
		g.Branches[branchName].Children = children
		// Clear old children from parent.
		g.Branches[current].Children = []string{branchName}
	} else {
		if err := g.AddBranch(branchName, current, parentRev, branchRev); err != nil {
			return err
		}
	}

	if opts.Desc != "" {
		g.SetDescription(branchName, opts.Desc)
	}

	if err := c.Store.WriteGraph(g); err != nil {
		return err
	}

	if opts.Worktree {
		if !opts.Stay {
			// Existing worktree path: switch back to the original branch first.
			if err := c.Git.Checkout(current); err != nil {
				return fmt.Errorf("could not switch back to %s: %w", current, err)
			}
		}
		if err := WorktreeAdd(c, WorktreeAddOpts{Name: branchName}); err != nil {
			return fmt.Errorf("worktree creation failed: %w", err)
		}
		if !c.Quiet {
			fmt.Printf("Created branch %q with worktree (parent: %s)\n", branchName, current)
		}
		return nil
	}

	if !c.Quiet {
		if opts.Stay {
			fmt.Printf("Created branch %q on top of %q (stayed on %s)\n", branchName, current, current)
		} else {
			fmt.Printf("Created branch %q on top of %q\n", branchName, current)
		}
	}
	return nil
}
