package cmd

import (
	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var (
	implementSource   string
	implementBranch   string
	implementParent   string
	implementWorktree bool
	implementSandbox  bool
	implementAI       bool
	implementComments bool
	implementNetwork  string
)

var implementCmd = &cobra.Command{
	Use:   "implement <issue>",
	Short: "Implement a GitHub issue or Jira ticket on a new branch",
	Long: `Fetch a GitHub issue or Jira ticket, create a new tracked branch for it,
and drive implementation.

The issue reference is auto-detected: a number (123, #123) or GitHub issue URL
is a GitHub issue; a KEY-N (PROJ-456) or browse URL is a Jira ticket. Use
--source to force it.

By default the branch is named <ref>-<title-slug> and stacks on the current
branch. How it implements depends on context:

  - inside a Claude session or with --ai: scaffold the branch and emit the brief
    as JSON for the caller to implement (no nested agent spawned)
  - a bare terminal: spawn an interactive claude seeded with the brief
  - --sandbox: run the implementation in a disposable container (implies a
    worktree); attaches in a terminal, or launches detached + emits JSON when
    driven by a Claude session / --ai

Examples:
  sr implement 123                       # GitHub issue on a new branch
  sr implement PROJ-456 --worktree       # Jira ticket in a worktree
  sr implement 123 --sandbox             # implement in a sandbox
  sr implement 123 --ai                  # emit {branch, worktreePath, prompt}
  sr implement 123 --source jira --branch fix-login`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		return engine.Implement(ctx, engine.ImplementOpts{
			Ref:      args[0],
			Source:   implementSource,
			Branch:   implementBranch,
			Parent:   implementParent,
			Worktree: implementWorktree,
			Sandbox:  implementSandbox,
			AI:       implementAI,
			Comments: implementComments,
			Network:  implementNetwork,
		})
	},
}

func init() {
	f := implementCmd.Flags()
	f.StringVar(&implementSource, "source", "", "force the issue source: github | jira (default: auto-detect)")
	f.StringVar(&implementBranch, "branch", "", "override the derived branch name")
	f.StringVar(&implementParent, "parent", "", "parent branch to stack on (default: current branch)")
	f.BoolVar(&implementWorktree, "worktree", false, "create the branch in a worktree")
	f.BoolVar(&implementSandbox, "sandbox", false, "implement in a sandbox (implies --worktree)")
	f.BoolVar(&implementAI, "ai", false, "emit JSON {branch, worktreePath, issueRef, prompt} and exit")
	f.BoolVar(&implementComments, "comments", false, "include the issue discussion in the prompt")
	f.StringVar(&implementNetwork, "network", "", "sandbox network mode: allowlist (default) | full")
	rootCmd.AddCommand(implementCmd)
}
