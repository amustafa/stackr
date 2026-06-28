package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show or modify stackr configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		cfg, err := ctx.Store.ReadConfig()
		if err != nil {
			return err
		}
		fmt.Printf("trunk:  %s\n", cfg.Trunk)
		fmt.Printf("remote: %s\n", cfg.Remote)
		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set [key] [value]",
	Short: "Set a config value",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		cfg, err := ctx.Store.ReadConfig()
		if err != nil {
			return err
		}
		key, value := args[0], args[1]
		switch key {
		case "trunk":
			// Validate the new trunk branch exists in git.
			exists, err := ctx.Git.BranchExists(value)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("branch %q does not exist", value)
			}

			// Update the graph: swap trunk designation.
			g, err := ctx.Store.ReadGraph()
			if err != nil {
				return err
			}

			oldTrunk := g.TrunkName()

			// Resolve new trunk revision.
			rev, err := ctx.Git.RevParse(value)
			if err != nil {
				return fmt.Errorf("could not resolve %s: %w", value, err)
			}

			if oldTrunk != "" && oldTrunk != value {
				old := g.Branches[oldTrunk]
				if old != nil {
					old.IsTrunk = false
					// Old trunk becomes a regular tracked branch under the new trunk.
					old.ParentBranchName = value
					old.ParentBranchRevision = rev
				}
			}

			if g.Has(value) {
				// Promote existing branch to trunk.
				b := g.Branches[value]

				// Remove from old parent's children list before clearing parent ref.
				if b.ParentBranchName != "" {
					if oldParent := g.Branches[b.ParentBranchName]; oldParent != nil {
						oldParent.Children = withoutStr(oldParent.Children, value)
					}
				}

				b.IsTrunk = true
				b.ParentBranchName = ""
				b.ParentBranchRevision = ""
				b.BranchRevision = rev

				// If old trunk is now under new trunk, add it as child.
				if oldTrunk != "" && oldTrunk != value {
					b.Children = appendUnique(b.Children, oldTrunk)
				}
			} else {
				// Add new trunk to graph.
				g.AddTrunk(value, rev)
				// Reparent old trunk's children that match.
				if oldTrunk != "" && oldTrunk != value {
					g.Branches[value].Children = appendUnique(g.Branches[value].Children, oldTrunk)
				}
			}

			if err := ctx.Store.WriteGraph(g); err != nil {
				return err
			}
			cfg.Trunk = value
		case "remote":
			cfg.Remote = value
		default:
			return fmt.Errorf("unknown config key %q (valid: trunk, remote)", key)
		}
		if err := ctx.Store.WriteConfig(cfg); err != nil {
			return err
		}
		fmt.Printf("Set %s = %s\n", key, value)
		return nil
	},
}

func init() {
	configCmd.AddCommand(configSetCmd)
	rootCmd.AddCommand(configCmd)
}

func withoutStr(s []string, val string) []string {
	result := make([]string, 0, len(s))
	for _, v := range s {
		if v != val {
			result = append(result, v)
		}
	}
	return result
}

func appendUnique(s []string, val string) []string {
	for _, v := range s {
		if v == val {
			return s
		}
	}
	return append(s, val)
}
