package engine

import (
	"fmt"

	"github.com/amustafa/stackr/internal/context"
)

// GetOpts holds options for getting a branch from remote.
type GetOpts struct {
	Branch  string
	Restack bool
	Force   bool
}

// Get fetches a branch from the remote and tracks it locally.
func Get(c *context.Context, opts GetOpts) error {
	cfg, err := c.Store.ReadConfig()
	if err != nil {
		return err
	}

	branch := opts.Branch
	if branch == "" {
		return fmt.Errorf("branch name required")
	}

	// Fetch the branch.
	if err := c.Git.Fetch(cfg.Remote); err != nil {
		return err
	}

	// Check if it exists on remote.
	exists, err := c.Git.RemoteBranchExists(cfg.Remote, branch)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("branch %q not found on remote %s", branch, cfg.Remote)
	}

	// Create local branch tracking the remote.
	localExists, _ := c.Git.BranchExists(branch)
	if localExists {
		if opts.Force {
			if err := c.Git.RunGit("checkout", branch); err != nil {
				return err
			}
			if err := c.Git.RunGit("reset", "--hard", cfg.Remote+"/"+branch); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("branch %q already exists locally (use -f to overwrite)", branch)
		}
	} else {
		if err := c.Git.RunGit("checkout", "-b", branch, cfg.Remote+"/"+branch); err != nil {
			return err
		}
	}

	// Track in stackr.
	if err := Track(c, branch, "", true); err != nil {
		return err
	}

	if !c.Quiet {
		fmt.Printf("Got %s from %s\n", branch, cfg.Remote)
	}
	return nil
}
