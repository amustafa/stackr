package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var renameCmd = &cobra.Command{
	Use:     "rename [new-name]",
	Aliases: []string{"rn"},
	Short:   "Rename the current branch",
	Args:    cobra.ExactArgs(1),
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
			return fmt.Errorf("cannot rename trunk")
		}

		newName := args[0]
		if g.Has(newName) {
			return fmt.Errorf("branch %q already exists in graph", newName)
		}

		// Rename in git.
		if err := ctx.Git.RenameBranch(current, newName); err != nil {
			return err
		}

		// Rename in graph.
		b := g.Branches[current]
		delete(g.Branches, current)
		g.Branches[newName] = b

		// Update parent references in children.
		for _, child := range b.Children {
			g.Branches[child].ParentBranchName = newName
		}

		// Update children references in parent.
		parent := g.Branches[b.ParentBranchName]
		if parent != nil {
			for i, c := range parent.Children {
				if c == current {
					parent.Children[i] = newName
				}
			}
		}

		if err := ctx.Store.WriteGraph(g); err != nil {
			return err
		}

		fmt.Printf("Renamed %s → %s\n", current, newName)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(renameCmd)
}
