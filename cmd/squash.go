package cmd

import (
	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var squashCmd = &cobra.Command{
	Use:     "squash",
	Aliases: []string{"sq"},
	Short:   "Squash all commits in the current branch into one",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		return engine.Squash(ctx, engine.SquashOpts{
			Message: squashFlagMessage,
			Edit:    squashFlagEdit,
			NoEdit:  squashFlagNoEdit,
		})
	},
}

var (
	squashFlagMessage string
	squashFlagEdit    bool
	squashFlagNoEdit  bool
)

func init() {
	squashCmd.Flags().StringVarP(&squashFlagMessage, "message", "m", "", "squash commit message")
	squashCmd.Flags().BoolVar(&squashFlagEdit, "edit", false, "open editor for commit message")
	squashCmd.Flags().BoolVar(&squashFlagNoEdit, "no-edit", false, "use default commit message")
	rootCmd.AddCommand(squashCmd)
}
