package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var popCmd = &cobra.Command{
	Use:   "pop",
	Short: "Delete current branch from the stack, keeping working tree changes",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		g, err := ctx.Store.ReadGraph()
		if err != nil {
			return err
		}

		current, err := ctx.Git.CurrentBranch()
		if err != nil {
			return err
		}
		if g.IsTrunk(current) {
			return fmt.Errorf("cannot pop trunk")
		}

		b := g.Branches[current]
		if b == nil {
			return fmt.Errorf("branch %q not tracked", current)
		}

		parent := b.ParentBranchName

		// Stash changes, switch to parent, apply stash.
		dirty, _ := ctx.Git.IsDirty()
		if dirty {
			_ = ctx.Git.RunGit("stash", "push", "-m", "sr pop: "+current)
		}

		// Remove from graph and delete branch.
		if err := g.RemoveBranch(current); err != nil {
			return err
		}
		if err := ctx.Git.Checkout(parent); err != nil {
			return err
		}
		_ = ctx.Git.DeleteBranch(current, true)

		if dirty {
			_ = ctx.Git.RunGit("stash", "pop")
		}

		if err := ctx.Store.WriteGraph(g); err != nil {
			return err
		}

		fmt.Printf("Popped %s (now on %s)\n", current, parent)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(popCmd)
}
