package cmd

import (
	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var squashCmd = &cobra.Command{
	Use:     "squash",
	Aliases: []string{"sq"},
	Short:   "Squash all commits in the current branch into one",
	Long: `Squash all commits in the current branch into one.

Squashing rewrites history, so descendant branches keep building on the
pre-squash commits. Pass --restack to rebase them onto the squashed branch in
the same run; otherwise run ` + "`sr restack`" + ` when you're ready.

--stack squashes the whole stack, bottom to top: each branch is restacked onto
its freshly squashed parent, then squashed itself. Branch messages default to
"squash: <branch>"; frozen branches are skipped.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		return engine.Squash(ctx, engine.SquashOpts{
			Message: squashFlagMessage,
			Edit:    squashFlagEdit,
			NoEdit:  squashFlagNoEdit,
			Restack: squashFlagRestack,
			Stack:   squashFlagStack,
		})
	},
}

var (
	squashFlagMessage string
	squashFlagEdit    bool
	squashFlagNoEdit  bool
	squashFlagRestack bool
	squashFlagStack   bool
)

func init() {
	squashCmd.Flags().StringVarP(&squashFlagMessage, "message", "m", "", "squash commit message")
	squashCmd.Flags().BoolVar(&squashFlagEdit, "edit", false, "open editor for commit message")
	squashCmd.Flags().BoolVar(&squashFlagNoEdit, "no-edit", false, "use default commit message")
	squashCmd.Flags().BoolVarP(&squashFlagRestack, "restack", "r", false,
		"restack descendant branches after squashing")
	squashCmd.Flags().BoolVarP(&squashFlagStack, "stack", "s", false,
		"squash every branch in the stack, bottom to top, restacking between")
	// --stack restacks branch by branch as part of the sweep, so --restack has
	// nothing left to add; one message or one editor session also cannot
	// describe N different squashes.
	squashCmd.MarkFlagsMutuallyExclusive("stack", "restack")
	squashCmd.MarkFlagsMutuallyExclusive("stack", "message")
	squashCmd.MarkFlagsMutuallyExclusive("stack", "edit")
	rootCmd.AddCommand(squashCmd)
}
