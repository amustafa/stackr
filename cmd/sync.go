package cmd

import (
	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Fetch trunk, restack, and clean merged branches",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		return engine.Sync(ctx, engine.SyncOpts{
			Restack: syncFlagRestack,
			Clean:   syncFlagClean,
			All:     syncFlagAll,
		})
	},
}

var (
	syncFlagRestack bool
	syncFlagClean   bool
	syncFlagAll     bool
)

func init() {
	syncCmd.Flags().BoolVar(&syncFlagRestack, "restack", true, "restack after syncing")
	syncCmd.Flags().BoolVar(&syncFlagClean, "clean", false, "clean up merged branches without asking")
	syncCmd.Flags().BoolVarP(&syncFlagAll, "all", "a", false, "sync all stacks")
	rootCmd.AddCommand(syncCmd)
}
