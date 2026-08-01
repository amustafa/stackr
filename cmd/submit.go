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
			PRMeta:    submitFlagPRMeta,
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
	submitFlagPRMeta    []string
	submitFlagAI        bool
	submitFlagAIPrepare bool
)

const prMetaHelp = `PR content as a JSON blob or a path to a file of JSON (repeatable).

A submit creates a PR for every branch it pushes, so content is addressed per
branch. Each entry takes {"branch", "title", "body" | "bodyFile", "draft"};
a value may hold one object or an array of them. "branch" may be omitted only
when the submit creates a single PR.

  sr submit --stack --pr-meta prs.json
  sr submit --pr-meta '{"title":"Add validation","bodyFile":"/tmp/body.md"}'`

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
	submitCmd.Flags().StringArrayVar(&submitFlagPRMeta, "pr-meta", nil, prMetaHelp)
	// --title is the one-branch shorthand for --pr-meta and is folded into it.
	// Accepting both would leave two entries competing for the same branch with
	// no stated precedence, so the conflict is refused at the flag layer instead.
	submitCmd.MarkFlagsMutuallyExclusive("pr-meta", "title")
	submitCmd.MarkFlagsMutuallyExclusive("pr-meta", "body")
	submitCmd.MarkFlagsMutuallyExclusive("pr-meta", "body-file")
	submitCmd.Flags().BoolVar(&submitFlagAI, "ai", false, "launch Claude to generate and submit PR")
	submitCmd.Flags().BoolVar(&submitFlagAIPrepare, "aiprepare", false, "output PR context as JSON (for agents)")
	rootCmd.AddCommand(submitCmd)
}
