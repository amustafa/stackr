package cmd

import (
	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var restackCmd = &cobra.Command{
	Use:     "restack",
	Aliases: []string{"r"},
	Short:   "Rebase the stack so branches are correctly ordered",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		return engine.Restack(ctx, engine.RestackOpts{
			Branch:    restackFlagBranch,
			Downstack: restackFlagDown,
			Upstack:   restackFlagUp,
			Only:      restackFlagOnly,
		})
	},
}

var (
	restackFlagBranch string
	restackFlagDown   bool
	restackFlagUp     bool
	restackFlagOnly   bool
)

func init() {
	restackCmd.Flags().StringVar(&restackFlagBranch, "branch", "", "branch to restack")
	restackCmd.Flags().BoolVarP(&restackFlagDown, "downstack", "d", false, "restack downstack only")
	restackCmd.Flags().BoolVarP(&restackFlagUp, "upstack", "u", false, "restack upstack only")
	restackCmd.Flags().BoolVarP(&restackFlagOnly, "only", "o", false, "restack only this branch")
	rootCmd.AddCommand(restackCmd)
}
