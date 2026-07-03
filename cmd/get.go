package cmd

import (
	"fmt"

	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var getCmd = &cobra.Command{
	Use:   "get [branch|PR#]",
	Short: "Sync branches from remote along the dependency path",
	Long: `Sync branches from remote along the dependency path.

For a given branch or PR number, sync branches from trunk to the target from
remote, prompting to resolve any conflicts. By default, locally existing upstack
branches are also synced. With no argument, syncs the current stack.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}

		branch := ""
		if len(args) > 0 {
			branch = args[0]
		}

		if getFlagWorktree && branch == "" {
			return fmt.Errorf("--worktree requires a branch argument")
		}

		result, err := engine.Get(ctx, engine.GetOpts{
			Branch:        branch,
			Downstack:     getFlagDownstack,
			RemoteUpstack: getFlagRemoteUpstack,
			Worktree:      getFlagWorktree,
			Stay:          getFlagStay,
			Force:         getFlagForce,
		})
		if err != nil {
			return err
		}

		if result != nil && !result.Conflicts {
			handleNavigateResult(result.NavigateResult)
		}

		return nil
	},
}

var (
	getFlagDownstack     bool
	getFlagRemoteUpstack bool
	getFlagWorktree      bool
	getFlagStay          bool
	getFlagForce         bool
)

func init() {
	getCmd.Flags().BoolVar(&getFlagDownstack, "downstack", false, "only sync trunk to target, skip upstack")
	getCmd.Flags().BoolVarP(&getFlagRemoteUpstack, "remote-upstack", "u", false, "also pull upstack branches that only exist on remote")
	getCmd.Flags().BoolVar(&getFlagWorktree, "worktree", false, "place target in a worktree and CD there")
	getCmd.Flags().BoolVar(&getFlagStay, "stay", false, "don't navigate to target after sync")
	getCmd.Flags().BoolVarP(&getFlagForce, "force", "f", false, "always replace with remote, no prompts")
	rootCmd.AddCommand(getCmd)
}
