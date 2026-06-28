package cmd

import (
	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var bottomCmd = &cobra.Command{
	Use:     "bottom",
	Aliases: []string{"b"},
	Short:   "Go to the bottom of the current stack",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		result, err := engine.NavigateBottom(ctx)
		if err != nil {
			return err
		}
		handleNavigateResult(result)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(bottomCmd)
}
