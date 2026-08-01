package cmd

import (
	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var rollbackCmd = &cobra.Command{
	Use:   "rollback [id]",
	Short: "Restore branch tips recorded before a submit changed them",
	Long: `Restore branch tips recorded before `+ "`sr submit`" + ` changed them.

Submit records where every branch stood before its preflight merges, cherry-picks
or resets anything, and prints the id. Rolling back puts those branch tips back.

This is not ` + "`sr undo`" + `, which restores the branch graph — who depends on whom —
and cannot put back a reset or a cherry-pick.

With no id, the most recent rollback point is used.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		var id string
		if len(args) == 1 {
			id = args[0]
		}
		return engine.Rollback(ctx, engine.RollbackOpts{
			ID:   id,
			List: rollbackFlagList,
		})
	},
}

var rollbackFlagList bool

func init() {
	rollbackCmd.Flags().BoolVar(&rollbackFlagList, "list", false, "list available rollback points")
	rootCmd.AddCommand(rollbackCmd)
}
