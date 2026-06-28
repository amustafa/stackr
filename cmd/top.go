package cmd

import (
	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var topCmd = &cobra.Command{
	Use:     "top",
	Aliases: []string{"t"},
	Short:   "Go to the top of the current stack",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		result, err := engine.NavigateTop(ctx)
		if err != nil {
			return err
		}
		handleNavigateResult(result)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(topCmd)
}
