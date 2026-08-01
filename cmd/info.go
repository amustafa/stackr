package cmd

import (
	"fmt"
	"strings"

	"github.com/amustafa/stackr/internal/engine"
	"github.com/amustafa/stackr/internal/graph"
	"github.com/amustafa/stackr/internal/store"
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

		// PR metadata is best-effort: a branch that was never submitted simply
		// has none, which is not an error worth surfacing here.
		if prInfo, err := ctx.Store.ReadPRInfo(); err == nil {
			printPRSection(g, prInfo, branch)
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

		// Show commits and their contexts.
		if !b.IsTrunk {
			entries, err := ctx.Git.CommitsBetween(b.ParentBranchName, branch)
			if err == nil && len(entries) > 0 {
				fmt.Printf("\nCommits (%d):\n", len(entries))
				for _, e := range entries {
					shortSHA := e.SHA[:min(7, len(e.SHA))]
					fmt.Printf("  %s %s\n", shortSHA, e.Subject)
					if commitCtxs := g.GetCommitContexts(branch, shortSHA); len(commitCtxs) > 0 {
						for _, cc := range commitCtxs {
							fmt.Printf("    [%s] %s\n", cc.Key, cc.Text)
						}
					}
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

// printPRSection renders the pull request block of `sr info`, including the
// GitHub stack the PR belongs to and its position within it.
func printPRSection(g *graph.Graph, prInfo *store.PRInfo, branch string) {
	pr := prInfo.Branches[branch]
	if pr == nil || pr.Number == 0 {
		return
	}

	state := pr.State
	if state == "" {
		state = "open"
	}
	if pr.Draft {
		state += ", draft"
	}

	fmt.Printf("\nPull Request:\n")
	fmt.Printf("  PR:     #%d (%s)\n", pr.Number, strings.ToLower(state))
	if pr.Title != "" {
		fmt.Printf("  Title:  %s\n", pr.Title)
	}
	if pr.BaseBranch != "" {
		fmt.Printf("  Base:   %s\n", pr.BaseBranch)
	}
	if pr.URL != "" {
		fmt.Printf("  URL:    %s\n", pr.URL)
	}

	if pr.StackNumber == 0 {
		return
	}

	pos, size := engine.StackPosition(g, prInfo, branch)
	fmt.Printf("  Stack:  GitHub stack #%d (%d of %d)\n", pr.StackNumber, pos, size)

	// Show the whole stack bottom-to-top so the PR's place in it is obvious.
	for i, name := range engine.StackMembers(g, prInfo, pr.StackNumber) {
		marker := " "
		if name == branch {
			marker = "*"
		}
		label := name
		if member := prInfo.Branches[name]; member != nil && member.Number > 0 {
			label = fmt.Sprintf("%s (#%d)", name, member.Number)
		}
		fmt.Printf("          %s %d. %s\n", marker, i+1, label)
	}
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
