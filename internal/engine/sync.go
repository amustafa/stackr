package engine

import (
	"fmt"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/graph"
)

// SyncOpts controls sync behavior.
type SyncOpts struct {
	Restack bool
	Force   bool
	All     bool
}

// Sync fetches trunk from remote, rebases onto it, restacks, and cleans merged branches.
func Sync(c *context.Context, opts SyncOpts) error {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}
	cfg, err := c.Store.ReadConfig()
	if err != nil {
		return err
	}

	trunk := g.TrunkName()
	origBranch, _ := c.Git.CurrentBranch()

	// Fetch from remote.
	if !c.Quiet {
		fmt.Printf("Fetching from %s...\n", cfg.Remote)
	}
	if err := c.Git.FetchPrune(cfg.Remote); err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}

	// Pull shared metadata (best-effort).
	TryPullMeta(c)

	// Checkout trunk and pull.
	if err := c.Git.Checkout(trunk); err != nil {
		return err
	}
	remoteTrunk := cfg.Remote + "/" + trunk
	if err := c.Git.RunGit("merge", "--ff-only", remoteTrunk); err != nil {
		return fmt.Errorf("could not fast-forward %s: %w", trunk, err)
	}

	// Update trunk revision in graph.
	trunkRev, err := c.Git.RevParse(trunk)
	if err != nil {
		return err
	}
	g.Branches[trunk].BranchRevision = trunkRev

	// Clean up merged branches.
	cleaned := cleanMergedBranches(c, g, trunk)
	for _, name := range cleaned {
		if !c.Quiet {
			fmt.Printf("Cleaned up merged branch: %s\n", name)
		}
	}

	if err := c.Store.WriteGraph(g); err != nil {
		return err
	}

	// Restack all stacks.
	if opts.Restack || opts.All {
		if !c.Quiet {
			fmt.Println("Restacking...")
		}
		if err := Restack(c, RestackOpts{Branch: trunk, SkipBlocked: true}); err != nil {
			return err
		}
	}

	// Return to original branch if it still exists.
	if origBranch != "" && origBranch != trunk {
		if g.Has(origBranch) {
			_ = c.Git.Checkout(origBranch)
		}
	}

	if !c.Quiet {
		fmt.Println("Sync complete")
	}
	return nil
}

// cleanMergedBranches removes branches that have been merged into trunk.
func cleanMergedBranches(c *context.Context, g *graph.Graph, trunk string) []string {
	var cleaned []string
	for name, b := range g.Branches {
		if b.IsTrunk {
			continue
		}
		merged, err := c.Git.IsMergedInto(name, trunk)
		if err != nil || !merged {
			continue
		}
		// Remove from graph and delete git branch.
		if err := g.RemoveBranch(name); err != nil {
			continue
		}
		_ = c.Git.DeleteBranch(name, true)
		cleaned = append(cleaned, name)
	}
	return cleaned
}
