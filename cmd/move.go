package cmd

import (
	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var moveCmd = &cobra.Command{
	Use:   "move",
	Short: "Move a branch onto a new parent",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		return engine.Move(ctx, engine.MoveOpts{
			Onto:   moveFlagOnto,
			Source: moveFlagSource,
		})
	},
}

var (
	moveFlagOnto   string
	moveFlagSource string
)

func init() {
	moveCmd.Flags().StringVarP(&moveFlagOnto, "onto", "o", "", "target parent branch")
	moveCmd.Flags().StringVarP(&moveFlagSource, "source", "s", "", "branch to move (default: current)")
	rootCmd.AddCommand(moveCmd)
}
