package cmd

import (
	"fmt"
	"os"

	srctx "github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/graph"
	"github.com/amustafa/stackr/internal/store"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize stackr in a git repository",
	Long:  "Detects the trunk branch, initializes metadata storage, and seeds the branch graph.",
	RunE:  runInit,
}

var (
	initFlagTrunk string
	initFlagReset bool
)

func init() {
	initCmd.Flags().StringVar(&initFlagTrunk, "trunk", "", "trunk branch name (auto-detected if omitted)")
	initCmd.Flags().BoolVar(&initFlagReset, "reset", false, "re-initialize, overwriting existing data")
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	cwd := flagCwd
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return err
		}
	}

	// Discover the repo (but don't require stackr to be initialized).
	c, err := srctx.Discover(cwd, flagDebug, flagInteractive)
	if err != nil {
		return err
	}

	if c.Store.Exists() && !initFlagReset {
		return fmt.Errorf("stackr already initialized (use --reset to re-initialize)")
	}

	// Determine trunk branch.
	trunk := initFlagTrunk
	if trunk == "" {
		trunk, err = c.Git.DefaultBranch()
		if err != nil {
			return fmt.Errorf("could not detect default branch: %w", err)
		}
		if trunk == "" {
			return fmt.Errorf("could not detect trunk branch — specify with --trunk")
		}
	}

	exists, err := c.Git.BranchExists(trunk)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("trunk branch %q does not exist", trunk)
	}

	// Resolve trunk revision.
	rev, err := c.Git.RevParse(trunk)
	if err != nil {
		return fmt.Errorf("could not resolve %s: %w", trunk, err)
	}

	// Create directory structure.
	if err := c.Store.Init(); err != nil {
		return err
	}

	// Write config.
	cfg := &store.Config{
		Trunk:  trunk,
		Remote: "origin",
	}
	if err := c.Store.WriteConfig(cfg); err != nil {
		return err
	}

	// Seed graph with trunk.
	g := graph.New()
	g.AddTrunk(trunk, rev)

	// If current branch isn't trunk, track it too.
	current, err := c.Git.CurrentBranch()
	if err == nil && current != trunk {
		currentRev, err := c.Git.RevParse(current)
		if err == nil {
			_ = g.AddBranch(current, trunk, rev, currentRev)
		}
	}

	if err := c.Store.WriteGraph(g); err != nil {
		return err
	}

	fmt.Printf("Initialized stackr with trunk %q\n", trunk)
	return nil
}
