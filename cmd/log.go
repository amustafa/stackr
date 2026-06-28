package cmd

import (
	"fmt"

	"github.com/amustafa/stackr/internal/graph"
	"github.com/spf13/cobra"
)

var logCmd = &cobra.Command{
	Use:     "log",
	Aliases: []string{"l"},
	Short:   "Visualize the stack tree",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		g, err := ctx.Store.ReadGraph()
		if err != nil {
			return err
		}
		current, err := ctx.Git.CurrentBranch()
		if err != nil {
			return err
		}

		opts := graph.RenderOpts{
			CurrentBranch: current,
			ShowAll:       logFlagAll,
			Reverse:       logFlagReverse,
		}

		if logFlagLong {
			opts.CommitsFn = func(branch string) []graph.CommitInfo {
				b := g.Branches[branch]
				if b == nil || b.IsTrunk {
					return nil
				}
				parent := b.ParentBranchName
				entries, err := ctx.Git.CommitsBetween(parent, branch)
				if err != nil {
					return nil
				}
				var commits []graph.CommitInfo
				for _, e := range entries {
					commits = append(commits, graph.CommitInfo{
						ShortSHA: e.SHA[:min(7, len(e.SHA))],
						Subject:  e.Subject,
					})
				}
				return commits
			}
		}

		fmt.Print(g.RenderTree(opts))
		return nil
	},
}

var (
	logFlagAll     bool
	logFlagReverse bool
	logFlagLong    bool
	logFlagStack   bool
)

func init() {
	logCmd.Flags().BoolVarP(&logFlagAll, "all", "a", false, "show all stacks")
	logCmd.Flags().BoolVarP(&logFlagReverse, "reverse", "r", false, "reverse order (trunk at bottom)")
	logCmd.Flags().BoolVarP(&logFlagLong, "long", "l", false, "show commits for each branch")
	logCmd.Flags().BoolVarP(&logFlagStack, "stack", "s", false, "show only current stack")
	rootCmd.AddCommand(logCmd)
}
