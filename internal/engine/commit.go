package engine

import (
	"encoding/json"
	"fmt"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/git"
	"github.com/amustafa/stackr/internal/graph"
)

// StackrCommitOpts holds options for the sr commit command.
type StackrCommitOpts struct {
	Message   string
	All       bool
	Untracked bool
	Patch     bool
	Desc      string   // update branch description
	Contexts  []string // raw JSON blobs of BranchContext
}

// StackrCommit wraps git commit with stackr graph integration.
func StackrCommit(c *context.Context, opts StackrCommitOpts) error {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}

	current, err := c.Git.CurrentBranch()
	if err != nil {
		return err
	}

	b := g.Branches[current]
	if b == nil {
		return fmt.Errorf("branch %q is not tracked", current)
	}
	if b.IsTrunk {
		return fmt.Errorf("cannot commit on trunk — use git commit directly")
	}

	// Stage changes.
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

	// Commit.
	if opts.Message == "" {
		return fmt.Errorf("commit message is required (-m)")
	}

	hasStagedChanges, err := c.Git.HasStagedChanges()
	if err != nil {
		return err
	}
	if !hasStagedChanges {
		return fmt.Errorf("no changes staged — use -a to stage all or stage manually")
	}

	if err := c.Git.Commit(opts.Message, git.CommitOpts{}); err != nil {
		return err
	}

	// Get the new commit SHA.
	sha, err := c.Git.RevParse("HEAD")
	if err != nil {
		return err
	}
	shortSHA := sha[:min(7, len(sha))]

	// Update branch description if provided.
	if opts.Desc != "" {
		if err := g.SetDescription(current, opts.Desc); err != nil {
			return err
		}
	}

	// Attach commit contexts.
	for _, raw := range opts.Contexts {
		var ctx graph.BranchContext
		if err := json.Unmarshal([]byte(raw), &ctx); err != nil {
			return fmt.Errorf("invalid --context JSON: %w", err)
		}
		if ctx.Key == "" {
			return fmt.Errorf("context entry must have a 'key' field")
		}
		if err := g.SetCommitContext(current, shortSHA, ctx); err != nil {
			return err
		}
	}

	// Update branch revision.
	b.BranchRevision = sha

	if err := c.Store.WriteGraph(g); err != nil {
		return err
	}

	if !c.Quiet {
		fmt.Printf("Committed %s on %s\n", shortSHA, current)
		if len(opts.Contexts) > 0 {
			fmt.Printf("Attached %d context entry(ies)\n", len(opts.Contexts))
		}
	}
	return nil
}
