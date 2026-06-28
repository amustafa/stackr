package cmd

import (
	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var foldCmd = &cobra.Command{
	Use:   "fold",
	Short: "Merge current branch into its parent",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		return engine.Fold(ctx, engine.FoldOpts{
			KeepName: foldFlagKeep,
		})
	},
}

var foldFlagKeep bool

func init() {
	foldCmd.Flags().BoolVarP(&foldFlagKeep, "keep", "k", false, "keep the current branch name")
	rootCmd.AddCommand(foldCmd)
}
