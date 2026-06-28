package cmd

import (
	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var trackCmd = &cobra.Command{
	Use:     "track [branch]",
	Aliases: []string{"tr"},
	Short:   "Start tracking a branch in the stack",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		name := ""
		if len(args) > 0 {
			name = args[0]
		}
		return engine.Track(ctx, name, trackFlagParent, trackFlagForce)
	},
}

var (
	trackFlagParent string
	trackFlagForce  bool
)

func init() {
	trackCmd.Flags().StringVarP(&trackFlagParent, "parent", "p", "", "parent branch")
	trackCmd.Flags().BoolVarP(&trackFlagForce, "force", "f", false, "force re-track")
	rootCmd.AddCommand(trackCmd)
}
