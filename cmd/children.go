package cmd

import (
	"fmt"

	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var childrenCmd = &cobra.Command{
	Use:   "children",
	Short: "Show child branches",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		children, err := engine.GetChildren(ctx)
		if err != nil {
			return err
		}
		for _, child := range children {
			fmt.Println(child)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(childrenCmd)
}
