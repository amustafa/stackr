package cmd

import (
	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var addressReviewCmd = &cobra.Command{
	Use:     "address-review",
	Aliases: []string{"ar"},
	Short:   "Walk and address PR review comments across the stack",
	Long: `Walk the stack bottom-to-top, present unresolved review comments,
and help you address them. After addressing comments on a branch,
commit changes, restack, and move up the stack.

Three modes:
  sr address-review              Interactive walkthrough
  sr address-review --aiprepare  Output all comments as JSON (for agents)
  sr address-review --ai         Launch Claude to handle everything`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		return engine.Review(ctx, engine.ReviewOpts{
			AI:        arFlagAI,
			AIPrepare: arFlagAIPrepare,
			DryRun:    arFlagDryRun,
		})
	},
}

var (
	arFlagAI        bool
	arFlagAIPrepare bool
	arFlagDryRun    bool
)

func init() {
	addressReviewCmd.Flags().BoolVar(&arFlagAI, "ai", false, "launch Claude to address all comments")
	addressReviewCmd.Flags().BoolVar(&arFlagAIPrepare, "aiprepare", false, "output review context as JSON (for agents)")
	addressReviewCmd.Flags().BoolVar(&arFlagDryRun, "dry-run", false, "show what would be done")
	rootCmd.AddCommand(addressReviewCmd)
}
