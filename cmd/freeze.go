package cmd

import (
	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var freezeCmd = &cobra.Command{
	Use:   "freeze [branch]",
	Short: "Freeze a branch (skip during restack)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		name := ""
		if len(args) > 0 {
			name = args[0]
		}
		return engine.Freeze(ctx, name)
	},
}

var unfreezeCmd = &cobra.Command{
	Use:   "unfreeze [branch]",
	Short: "Unfreeze a branch",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		name := ""
		if len(args) > 0 {
			name = args[0]
		}
		return engine.Unfreeze(ctx, name)
	},
}

func init() {
	rootCmd.AddCommand(freezeCmd)
	rootCmd.AddCommand(unfreezeCmd)
}
