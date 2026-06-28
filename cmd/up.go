package cmd

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/amustafa/stackr/internal/engine"
	"github.com/amustafa/stackr/internal/ui"
	"github.com/spf13/cobra"
)

var upCmd = &cobra.Command{
	Use:     "up [steps]",
	Aliases: []string{"u"},
	Short:   "Move upstack (away from trunk)",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		n := upFlagN
		if len(args) > 0 {
			var err error
			n, err = strconv.Atoi(args[0])
			if err != nil {
				return err
			}
		}

		result, err := engine.NavigateUp(ctx, n)
		var multi *engine.ErrMultipleChildren
		if errors.As(err, &multi) {
			var target string
			if ctx.Interactive {
				title := fmt.Sprintf("Branch %q has multiple children:", multi.Current)
				selected, selectErr := ui.Select(title, multi.Children)
				if selectErr != nil {
					return selectErr
				}
				target = selected
			} else {
				target = multi.Children[0]
			}
			result, err = engine.NavigateToBranch(ctx, target)
			if err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		handleNavigateResult(result)
		return nil
	},
}

var upFlagN int

func init() {
	upCmd.Flags().IntVarP(&upFlagN, "steps", "n", 1, "number of steps")
	rootCmd.AddCommand(upCmd)
}
