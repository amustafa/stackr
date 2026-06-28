package cmd

import (
	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var reorderCmd = &cobra.Command{
	Use:   "reorder",
	Short: "Reorder branches in the stack",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		return engine.Reorder(ctx)
	},
}

func init() {
	rootCmd.AddCommand(reorderCmd)
}
