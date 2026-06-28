package cmd

import (
	"fmt"

	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var parentCmd = &cobra.Command{
	Use:   "parent",
	Short: "Show the parent branch",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		parent, err := engine.GetParent(ctx)
		if err != nil {
			return err
		}
		fmt.Println(parent)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(parentCmd)
}
