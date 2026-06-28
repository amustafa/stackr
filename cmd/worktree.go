package cmd

import (
	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var worktreeCmd = &cobra.Command{
	Use:     "worktree",
	Aliases: []string{"wt"},
	Short:   "Manage worktrees for branches",
}

var worktreeAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Create a worktree for a branch",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return engine.WorktreeAdd(ctx, engine.WorktreeAddOpts{
			Name: args[0],
		})
	},
}

var worktreeRemoveCmd = &cobra.Command{
	Use:     "remove <name>",
	Aliases: []string{"rm"},
	Short:   "Remove a worktree",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return engine.WorktreeRemove(ctx, engine.WorktreeRemoveOpts{
			Name:   args[0],
			Delete: flagWorktreeDelete,
		})
	},
}

var flagWorktreeDelete bool

func init() {
	worktreeRemoveCmd.Flags().BoolVar(&flagWorktreeDelete, "delete", false, "also delete the branch")
	worktreeCmd.AddCommand(worktreeAddCmd)
	worktreeCmd.AddCommand(worktreeRemoveCmd)
	rootCmd.AddCommand(worktreeCmd)
}
