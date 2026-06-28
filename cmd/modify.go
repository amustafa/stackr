package cmd

import (
	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var modifyCmd = &cobra.Command{
	Use:     "modify",
	Aliases: []string{"m"},
	Short:   "Amend the current branch and restack descendants",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		return engine.Modify(ctx, engine.ModifyOpts{
			Message:   modifyFlagMessage,
			All:       modifyFlagAll,
			Edit:      modifyFlagEdit,
			NewCommit: modifyFlagCommit,
		})
	},
}

var (
	modifyFlagMessage string
	modifyFlagAll     bool
	modifyFlagEdit    bool
	modifyFlagCommit  bool
)

func init() {
	modifyCmd.Flags().StringVarP(&modifyFlagMessage, "message", "m", "", "commit message")
	modifyCmd.Flags().BoolVarP(&modifyFlagAll, "all", "a", false, "stage all changes")
	modifyCmd.Flags().BoolVarP(&modifyFlagEdit, "edit", "e", false, "open editor for commit message")
	modifyCmd.Flags().BoolVarP(&modifyFlagCommit, "commit", "c", false, "create new commit instead of amending")
	rootCmd.AddCommand(modifyCmd)
}
