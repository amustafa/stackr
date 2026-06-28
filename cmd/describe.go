package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var describeCmd = &cobra.Command{
	Use:     "describe [description]",
	Aliases: []string{"desc"},
	Short:   "Set the objective for the current branch",
	Long: `Set or show the objective/description for the current branch.

  sr describe "Add JWT refresh token rotation"
  sr describe -m "Add JWT refresh token rotation"
  sr describe                                      # show current`,
	Args: cobra.MaximumNArgs(1),
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

		// Resolve description from positional arg or -m flag.
		desc := ""
		if len(args) > 0 {
			desc = args[0]
		} else if describeFlagMessage != "" {
			desc = describeFlagMessage
		}

		// No input — show current description.
		if desc == "" {
			current := g.Description(branch)
			if current == "" {
				fmt.Printf("No objective set for %s\n", branch)
			} else {
				fmt.Printf("Objective for %s: %s\n", branch, current)
			}
			return nil
		}

		if err := g.SetDescription(branch, desc); err != nil {
			return err
		}
		if err := ctx.Store.WriteGraph(g); err != nil {
			return err
		}
		fmt.Printf("Set objective for %s: %s\n", branch, desc)
		return nil
	},
}

var describeFlagMessage string

func init() {
	describeCmd.Flags().StringVarP(&describeFlagMessage, "message", "m", "", "branch objective")
	rootCmd.AddCommand(describeCmd)
}
