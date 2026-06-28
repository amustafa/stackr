package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var infoCmd = &cobra.Command{
	Use:   "info [branch]",
	Short: "Show branch details",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		g, err := ctx.Store.ReadGraph()
		if err != nil {
			return err
		}

		branch := ""
		if len(args) > 0 {
			branch = args[0]
		} else {
			branch, err = ctx.Git.CurrentBranch()
			if err != nil {
				return err
			}
		}

		b := g.Branches[branch]
		if b == nil {
			return fmt.Errorf("branch %q not tracked", branch)
		}

		fmt.Printf("Branch:   %s\n", branch)
		if b.IsTrunk {
			fmt.Println("Type:     trunk")
		} else {
			fmt.Printf("Parent:   %s\n", b.ParentBranchName)
		}
		if len(b.Children) > 0 {
			fmt.Printf("Children: %s\n", strings.Join(b.Children, ", "))
		}
		fmt.Printf("Revision: %s\n", b.BranchRevision[:min(12, len(b.BranchRevision))])
		if b.Frozen {
			fmt.Println("Status:   frozen")
		}
		if b.Description != "" {
			fmt.Printf("Objective: %s\n", b.Description)
		}
		if len(b.Context) > 0 {
			fmt.Printf("\nContext:\n")
			for _, ctx := range b.Context {
				fmt.Printf("  [%s] %s\n", ctx.Key, ctx.Text)
				for _, src := range ctx.Sources {
					fmt.Printf("    source: %s (%s)\n", src.Reference, src.Type)
				}
				if len(ctx.Tickets) > 0 {
					fmt.Printf("    tickets: %s\n", strings.Join(ctx.Tickets, ", "))
				}
			}
		}

		// Show commits.
		if !b.IsTrunk {
			entries, err := ctx.Git.CommitsBetween(b.ParentBranchName, branch)
			if err == nil && len(entries) > 0 {
				fmt.Printf("\nCommits (%d):\n", len(entries))
				for _, e := range entries {
					fmt.Printf("  %s %s\n", e.SHA[:min(7, len(e.SHA))], e.Subject)
				}
			}
		}

		// Show diff stat if requested.
		if infoFlagStat && !b.IsTrunk {
			stat, err := ctx.Git.DiffStat(b.ParentBranchName, branch)
			if err == nil && stat != "" {
				fmt.Printf("\n%s\n", stat)
			}
		}

		// Show full diff if requested.
		if infoFlagDiff && !b.IsTrunk {
			diff, err := ctx.Git.DiffPatch(b.ParentBranchName, branch)
			if err == nil && diff != "" {
				fmt.Printf("\n%s\n", diff)
			}
		}

		return nil
	},
}

var (
	infoFlagDiff bool
	infoFlagStat bool
)

func init() {
	infoCmd.Flags().BoolVarP(&infoFlagDiff, "diff", "d", false, "show full diff")
	infoCmd.Flags().BoolVarP(&infoFlagStat, "stat", "s", false, "show diff stat")
	rootCmd.AddCommand(infoCmd)
}
