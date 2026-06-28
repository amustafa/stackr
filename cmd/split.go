package cmd

import (
	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var splitCmd = &cobra.Command{
	Use:     "split",
	Aliases: []string{"sp"},
	Short:   "Split the current branch into multiple branches",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		return engine.Split(ctx, engine.SplitOpts{
			ByCommit: splitFlagByCommit,
			ByHunk:   splitFlagByHunk,
			ByFile:   splitFlagByFile,
		})
	},
}

var (
	splitFlagByCommit bool
	splitFlagByHunk   bool
	splitFlagByFile   bool
)

func init() {
	splitCmd.Flags().BoolVarP(&splitFlagByCommit, "by-commit", "c", true, "split by commit")
	splitCmd.Flags().BoolVarP(&splitFlagByHunk, "by-hunk", "h", false, "split by hunk")
	splitCmd.Flags().BoolVarP(&splitFlagByFile, "by-file", "f", false, "split by file")
	rootCmd.AddCommand(splitCmd)
}
