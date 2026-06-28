package cmd

import (
	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:     "delete [branch]",
	Aliases: []string{"dl"},
	Short:   "Delete a branch and reparent its children",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		opts := engine.DeleteOpts{
			Force:   deleteFlagForce,
			Upstack: deleteFlagUpstack,
		}
		if len(args) > 0 {
			opts.Name = args[0]
		}
		return engine.Delete(ctx, opts)
	},
}

var (
	deleteFlagForce   bool
	deleteFlagUpstack bool
)

func init() {
	deleteCmd.Flags().BoolVarP(&deleteFlagForce, "force", "f", false, "force delete")
	deleteCmd.Flags().BoolVar(&deleteFlagUpstack, "upstack", false, "delete all upstack branches too")
	rootCmd.AddCommand(deleteCmd)
}
