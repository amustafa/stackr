package cmd

import (
	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var restackCmd = &cobra.Command{
	Use:     "restack",
	Aliases: []string{"r"},
	Short:   "Rebase the stack so branches are correctly ordered",
	Long: `Rebase branches so each sits on its parent's tip.

By default restacks the branch's straight lineage down to trunk, the branch
itself, and everything stacked on top of it (the union of --downstack and
--upstack). Branches forking off an ancestor are left alone — sr log marks
them [needs restack] — and only sr sync sweeps every stack.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		return engine.Restack(ctx, engine.RestackOpts{
			Branch:    restackFlagBranch,
			Downstack: restackFlagDown,
			Upstack:   restackFlagUp,
			Only:      restackFlagOnly,
			Base:      restackFlagBase,
		})
	},
}

var (
	restackFlagBranch string
	restackFlagDown   bool
	restackFlagUp     bool
	restackFlagOnly   bool
	restackFlagBase   string
)

func init() {
	restackCmd.Flags().StringVar(&restackFlagBranch, "branch", "", "branch to restack")
	restackCmd.Flags().BoolVarP(&restackFlagDown, "downstack", "d", false, "restack downstack only")
	restackCmd.Flags().BoolVarP(&restackFlagUp, "upstack", "u", false, "restack upstack only")
	restackCmd.Flags().BoolVarP(&restackFlagOnly, "only", "o", false, "restack only this branch")
	restackCmd.Flags().StringVar(&restackFlagBase, "base", "", "re-point the branch's base commit (repairs a lost or corrupted base)")
	rootCmd.AddCommand(restackCmd)
}
