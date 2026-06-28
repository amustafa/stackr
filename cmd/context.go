package cmd

import (
	"fmt"
	"strings"

	"github.com/amustafa/stackr/internal/graph"
	"github.com/spf13/cobra"
)

var contextCmd = &cobra.Command{
	Use:   "context [set <key> <text> | rm <key> | list]",
	Short: "Manage branch context entries for AI agents",
	Long: `Add, remove, or list structured context entries on the current branch.
Context entries are keyed records that AI agents can use to track
decisions, references, and related tickets.

  sr context set arch-decision "Using JWT for stateless auth"
  sr context set arch-decision "Using JWT" --source file:internal/auth/jwt.go --ticket PROJ-123
  sr context rm arch-decision
  sr context list
  sr context              (same as list)`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		g, err := ctx.Store.ReadGraph()
		if err != nil {
			return err
		}

		branch, err := ctx.Git.CurrentBranch()
		if err != nil {
			return err
		}
		if !g.Has(branch) {
			return fmt.Errorf("branch %q not tracked", branch)
		}

		action := "list"
		if len(args) > 0 {
			action = args[0]
		}

		switch action {
		case "set":
			if len(args) < 3 {
				return fmt.Errorf("usage: sr context set <key> <text>")
			}
			entry := graph.BranchContext{
				Key:  args[1],
				Text: args[2],
			}
			for _, s := range contextFlagSources {
				parts := strings.SplitN(s, ":", 2)
				if len(parts) != 2 {
					return fmt.Errorf("source must be type:reference (e.g. file:path/to/file.go), got %q", s)
				}
				entry.Sources = append(entry.Sources, graph.Source{
					Type:      parts[0],
					Reference: parts[1],
				})
			}
			if len(contextFlagTickets) > 0 {
				entry.Tickets = contextFlagTickets
			}
			if err := g.SetContext(branch, entry); err != nil {
				return err
			}
			if err := ctx.Store.WriteGraph(g); err != nil {
				return err
			}
			fmt.Printf("Set context %q on %s\n", entry.Key, branch)

		case "rm", "remove":
			if len(args) < 2 {
				return fmt.Errorf("usage: sr context rm <key>")
			}
			if err := g.RemoveContext(branch, args[1]); err != nil {
				return err
			}
			if err := ctx.Store.WriteGraph(g); err != nil {
				return err
			}
			fmt.Printf("Removed context %q from %s\n", args[1], branch)

		case "list":
			entries := g.GetContext(branch)
			if len(entries) == 0 {
				fmt.Println("No context entries")
				return nil
			}
			for _, e := range entries {
				fmt.Printf("  [%s] %s\n", e.Key, e.Text)
				for _, src := range e.Sources {
					fmt.Printf("    source: %s (%s)\n", src.Reference, src.Type)
				}
				if len(e.Tickets) > 0 {
					fmt.Printf("    tickets: %s\n", strings.Join(e.Tickets, ", "))
				}
			}

		default:
			return fmt.Errorf("unknown action %q (use set, rm, or list)", action)
		}

		return nil
	},
}

var (
	contextFlagSources []string
	contextFlagTickets []string
)

func init() {
	contextCmd.Flags().StringArrayVar(&contextFlagSources, "source", nil, "source reference as type:ref (repeatable)")
	contextCmd.Flags().StringSliceVar(&contextFlagTickets, "ticket", nil, "related ticket IDs (comma-separated or repeatable)")
	rootCmd.AddCommand(contextCmd)
}
