package cmd

import (
	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var untrackCmd = &cobra.Command{
	Use:     "untrack [branch]",
	Aliases: []string{"utr"},
	Short:   "Stop tracking a branch",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		name := ""
		if len(args) > 0 {
			name = args[0]
		}
		return engine.Untrack(ctx, name, untrackFlagForce)
	},
}

var untrackFlagForce bool

func init() {
	untrackCmd.Flags().BoolVarP(&untrackFlagForce, "force", "f", false, "force untrack (reparent children)")
	rootCmd.AddCommand(untrackCmd)
}
