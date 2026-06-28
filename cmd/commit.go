package cmd

import (
	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var commitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Commit with stackr context tracking",
	Long: `Wraps git commit with stackr integration: stages changes, commits,
and optionally attaches structured context and updates the branch description.

  sr commit -a -m "add validation"
  sr commit -m "add auth" --desc "JWT auth middleware"
  sr commit -a -m "step 3" --context '{"key":"step-3","text":"reason","sources":[{"type":"file","reference":"plan.md"}]}'`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		return engine.StackrCommit(ctx, engine.StackrCommitOpts{
			Message:   commitFlagMessage,
			All:       commitFlagAll,
			Untracked: commitFlagUntracked,
			Patch:     commitFlagPatch,
			Desc:      commitFlagDesc,
			Contexts:  commitFlagContexts,
		})
	},
}

var (
	commitFlagMessage   string
	commitFlagAll       bool
	commitFlagUntracked bool
	commitFlagPatch     bool
	commitFlagDesc      string
	commitFlagContexts  []string
)

func init() {
	commitCmd.Flags().StringVarP(&commitFlagMessage, "message", "m", "", "commit message (required)")
	commitCmd.Flags().BoolVarP(&commitFlagAll, "all", "a", false, "stage all tracked changes")
	commitCmd.Flags().BoolVarP(&commitFlagUntracked, "untracked", "u", false, "stage tracked file changes")
	commitCmd.Flags().BoolVarP(&commitFlagPatch, "patch", "p", false, "interactive patch selection")
	commitCmd.Flags().StringVar(&commitFlagDesc, "desc", "", "update branch description")
	commitCmd.Flags().StringArrayVar(&commitFlagContexts, "context", nil, "commit context JSON blob (repeatable)")
	rootCmd.AddCommand(commitCmd)
}
