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
	Long: "Fetches the shared branch graph, config, and PR metadata from the remote and merges with local state.\n\n" +
		"Works in an uninitialized clone too: git does not fetch stackr's metadata ref, so this is how a " +
		"fresh clone of an sr-managed repo bootstraps itself.",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Deliberately no RequireInit: pull-meta is the way OUT of the
		// uninitialized state for a fresh clone, so gating it on being
		// initialized would be a bootstrap catch-22.
		return engine.PullMeta(ctx)
	},
}

func init() {
	rootCmd.AddCommand(pushMetaCmd)
	rootCmd.AddCommand(pullMetaCmd)
}
