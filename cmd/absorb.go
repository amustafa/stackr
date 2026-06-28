package cmd

import (
	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var absorbCmd = &cobra.Command{
	Use:     "absorb",
	Aliases: []string{"ab"},
	Short:   "Distribute changes to appropriate stack commits",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		return engine.Absorb(ctx, engine.AbsorbOpts{
			DryRun: absorbFlagDryRun,
			Force:  absorbFlagForce,
			All:    absorbFlagAll,
			Patch:  absorbFlagPatch,
		})
	},
}

var (
	absorbFlagDryRun bool
	absorbFlagForce  bool
	absorbFlagAll    bool
	absorbFlagPatch  bool
)

func init() {
	absorbCmd.Flags().BoolVar(&absorbFlagDryRun, "dry-run", false, "show what would be absorbed")
	absorbCmd.Flags().BoolVarP(&absorbFlagForce, "force", "f", false, "force absorb")
	absorbCmd.Flags().BoolVarP(&absorbFlagAll, "all", "a", false, "stage all changes")
	absorbCmd.Flags().BoolVarP(&absorbFlagPatch, "patch", "p", false, "interactive patch selection")
	rootCmd.AddCommand(absorbCmd)
}
