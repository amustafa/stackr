package cmd

import (
	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var submitCmd = &cobra.Command{
	Use:     "submit",
	Aliases: []string{"s"},
	Short:   "Push branches and create PRs",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		return engine.Submit(ctx, engine.SubmitOpts{
			Draft:      submitFlagDraft,
			Stack:      submitFlagStack,
			UpdateOnly: submitFlagUpdate,
			Force:      submitFlagForce,
			NoForce:    submitFlagNoForce,
			DryRun:     submitFlagDryRun,
			Title:     submitFlagTitle,
			Body:      submitFlagBody,
			BodyFile:  submitFlagBodyFile,
			AI:        submitFlagAI,
			AIPrepare: submitFlagAIPrepare,
		})
	},
}

var (
	submitFlagDraft     bool
	submitFlagStack     bool
	submitFlagUpdate    bool
	submitFlagForce     bool
	submitFlagNoForce   bool
	submitFlagDryRun    bool
	submitFlagTitle     string
	submitFlagBody      string
	submitFlagBodyFile  string
	submitFlagAI        bool
	submitFlagAIPrepare bool
)

func init() {
	submitCmd.Flags().BoolVarP(&submitFlagDraft, "draft", "d", false, "mark as draft")
	submitCmd.Flags().BoolVarP(&submitFlagStack, "stack", "s", false, "push all branches in the stack")
	submitCmd.Flags().BoolVarP(&submitFlagUpdate, "update-only", "u", false,
		"only submit branches that already have a PR or a remote branch")
	submitCmd.Flags().BoolVarP(&submitFlagForce, "force", "f", false,
		"overwrite the remote when it has content you don't, without prompting (the push is still lease-pinned)")
	submitCmd.Flags().BoolVar(&submitFlagNoForce, "no-force", false,
		"never force-push; fail instead")
	submitCmd.MarkFlagsMutuallyExclusive("force", "no-force")
	submitCmd.Flags().BoolVar(&submitFlagDryRun, "dry-run", false, "show what would be pushed, changing nothing")
	submitCmd.Flags().StringVar(&submitFlagTitle, "title", "", "PR title (skips interactive prompts)")
	submitCmd.Flags().StringVar(&submitFlagBody, "body", "", "PR body (used with --title)")
	submitCmd.Flags().StringVar(&submitFlagBodyFile, "body-file", "", "read PR body from file (used with --title)")
	submitCmd.Flags().BoolVar(&submitFlagAI, "ai", false, "launch Claude to generate and submit PR")
	submitCmd.Flags().BoolVar(&submitFlagAIPrepare, "aiprepare", false, "output PR context as JSON (for agents)")
	rootCmd.AddCommand(submitCmd)
}
