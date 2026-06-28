package cmd

import (
	"fmt"
	"sort"

	"github.com/amustafa/stackr/internal/engine"
	"github.com/amustafa/stackr/internal/ui"
	"github.com/spf13/cobra"
)

var checkoutCmd = &cobra.Command{
	Use:     "checkout [branch]",
	Aliases: []string{"co"},
	Short:   "Switch to a branch",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}

		var target string

		if checkoutFlagTrunk {
			g, err := ctx.Store.ReadGraph()
			if err != nil {
				return err
			}
			target = g.TrunkName()
		} else if len(args) == 0 {
			if !ctx.Interactive {
				return fmt.Errorf("branch name required")
			}
			branches, err := engine.TrackedBranches(ctx)
			if err != nil {
				return err
			}
			if len(branches) == 0 {
				return fmt.Errorf("no tracked branches")
			}
			sort.Strings(branches)
			selected, err := ui.Select("Switch to branch:", branches)
			if err != nil {
				return err
			}
			target = selected
		} else {
			target = args[0]
		}

		result, err := engine.NavigateToBranch(ctx, target)
		if err != nil {
			return err
		}
		handleNavigateResult(result)
		return nil
	},
}

var checkoutFlagTrunk bool

func init() {
	checkoutCmd.Flags().BoolVarP(&checkoutFlagTrunk, "trunk", "t", false, "checkout trunk branch")
	rootCmd.AddCommand(checkoutCmd)
}
