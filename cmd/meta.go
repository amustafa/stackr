package cmd

import (
	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var pushMetaCmd = &cobra.Command{
	Use:   "push-meta",
	Short: "Push stackr metadata to the remote",
	Long:  "Pushes the shared branch graph, config, and PR metadata to the remote so collaborators can access it.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		return engine.PushMeta(ctx)
	},
}

var pullMetaCmd = &cobra.Command{
	Use:   "pull-meta",
	Short: "Pull and merge stackr metadata from the remote",
	Long:  "Fetches the shared branch graph, config, and PR metadata from the remote and merges with local state.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		return engine.PullMeta(ctx)
	},
}

func init() {
	rootCmd.AddCommand(pushMetaCmd)
	rootCmd.AddCommand(pullMetaCmd)
}
