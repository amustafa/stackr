package cmd

import (
	"strconv"

	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var downCmd = &cobra.Command{
	Use:     "down [steps]",
	Aliases: []string{"d"},
	Short:   "Move downstack (toward trunk)",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		n := downFlagN
		if len(args) > 0 {
			var err error
			n, err = strconv.Atoi(args[0])
			if err != nil {
				return err
			}
		}
		result, err := engine.NavigateDown(ctx, n)
		if err != nil {
			return err
		}
		handleNavigateResult(result)
		return nil
	},
}

var downFlagN int

func init() {
	downCmd.Flags().IntVarP(&downFlagN, "steps", "n", 1, "number of steps")
	rootCmd.AddCommand(downCmd)
}
