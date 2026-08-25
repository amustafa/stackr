package cmd

import (
	"fmt"
	"sort"

	"github.com/amustafa/stackr/internal/engine"
	"github.com/amustafa/stackr/internal/ui"
	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:     "delete [branch]",
	Aliases: []string{"dl"},
	Short:   "Delete a branch (and its worktree) and reparent its children",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		opts := engine.DeleteOpts{
			Force:   deleteFlagForce,
			Upstack: deleteFlagUpstack,
		}
		if len(args) > 0 {
			opts.Name = args[0]
		} else if ctx.Interactive {
			branches, err := engine.TrackedBranches(ctx)
			if err != nil {
				return err
			}
			if len(branches) == 0 {
				return fmt.Errorf("no tracked branches")
			}
			sort.Strings(branches)
			current, _ := ctx.Git.CurrentBranch()
			selected, err := ui.SelectWithDefault("Delete branch:", branches, current)
			if err != nil {
				return err
			}
			opts.Name = selected
		}
		// Non-interactive with no arg: engine defaults to the current branch.
		result, err := engine.Delete(ctx, opts)
		// Report the navigation even when the delete failed partway — if we
		// already stepped down into a worktree, the shell should still cd.
		handleNavigateResult(result)
		return err
	},
}

var (
	deleteFlagForce   bool
	deleteFlagUpstack bool
)

func init() {
	deleteCmd.Flags().BoolVarP(&deleteFlagForce, "force", "f", false, "force delete")
	deleteCmd.Flags().BoolVar(&deleteFlagUpstack, "upstack", false, "delete all upstack branches too")
	rootCmd.AddCommand(deleteCmd)
}
