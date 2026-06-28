package cmd

import (
	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var getCmd = &cobra.Command{
	Use:   "get [branch]",
	Short: "Fetch a branch from remote and track it",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		return engine.Get(ctx, engine.GetOpts{
			Branch:  args[0],
			Restack: getFlagRestack,
			Force:   getFlagForce,
		})
	},
}

var (
	getFlagRestack bool
	getFlagForce   bool
)

func init() {
	getCmd.Flags().BoolVar(&getFlagRestack, "restack", false, "restack after getting")
	getCmd.Flags().BoolVarP(&getFlagForce, "force", "f", false, "overwrite local branch")
	rootCmd.AddCommand(getCmd)
}
