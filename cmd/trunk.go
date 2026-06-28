package cmd

import (
	"fmt"

	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var trunkCmd = &cobra.Command{
	Use:   "trunk",
	Short: "Show or switch to the trunk branch",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		g, err := ctx.Store.ReadGraph()
		if err != nil {
			return err
		}
		trunk := g.TrunkName()

		if trunkFlagCheckout {
			result, err := engine.NavigateToBranch(ctx, trunk)
			if err != nil {
				return err
			}
			handleNavigateResult(result)
			return nil
		}

		fmt.Println(trunk)
		return nil
	},
}

var trunkFlagCheckout bool

func init() {
	trunkCmd.Flags().BoolVarP(&trunkFlagCheckout, "checkout", "a", false, "checkout the trunk branch")
	rootCmd.AddCommand(trunkCmd)
}
