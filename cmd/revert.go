package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var revertCmd = &cobra.Command{
	Use:   "revert [sha]",
	Short: "Create a revert branch for a commit",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		sha := args[0]

		// Create a revert commit.
		if err := ctx.Git.RunGit("revert", "--no-commit", sha); err != nil {
			return fmt.Errorf("revert failed: %w", err)
		}

		shortSHA, _ := ctx.Git.RevParseShort(sha)
		branchName := fmt.Sprintf("revert-%s", shortSHA)

		// Commit the revert.
		if err := ctx.Git.RunGit("commit", "-m", fmt.Sprintf("Revert %s", shortSHA)); err != nil {
			return err
		}

		fmt.Printf("Created revert commit for %s on current branch\n", shortSHA)
		_ = branchName
		return nil
	},
}

func init() {
	rootCmd.AddCommand(revertCmd)
}
