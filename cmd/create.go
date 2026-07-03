package cmd

import (
	"github.com/amustafa/stackr/internal/engine"
	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:     "create [name]",
	Aliases: []string{"c"},
	Short:   "Create a new stacked branch",
	Long:    "Creates a new branch on top of the current branch, optionally with a commit.",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		opts := engine.CreateOpts{
			Message:   createFlagMessage,
			All:       createFlagAll,
			Untracked: createFlagUntracked,
			Patch:     createFlagPatch,
			Insert:    createFlagInsert,
			Desc:      createFlagDesc,
			Worktree:  createFlagWorktree,
			Stay:      createFlagStay,
		}
		if len(args) > 0 {
			opts.Name = args[0]
		}
		return engine.Create(ctx, opts)
	},
}

var (
	createFlagMessage   string
	createFlagAll       bool
	createFlagUntracked bool
	createFlagPatch     bool
	createFlagInsert    bool
	createFlagDesc      string
	createFlagWorktree  bool
	createFlagStay      bool
)

func init() {
	createCmd.Flags().StringVarP(&createFlagMessage, "message", "m", "", "commit message")
	createCmd.Flags().BoolVarP(&createFlagAll, "all", "a", false, "stage all changes")
	createCmd.Flags().BoolVarP(&createFlagUntracked, "untracked", "u", false, "stage tracked file changes")
	createCmd.Flags().BoolVarP(&createFlagPatch, "patch", "p", false, "interactive patch selection")
	createCmd.Flags().BoolVarP(&createFlagInsert, "insert", "i", false, "insert between current and its children")
	createCmd.Flags().StringVar(&createFlagDesc, "desc", "", "set branch description/objective")
	createCmd.Flags().BoolVar(&createFlagWorktree, "worktree", false, "create in a worktree instead of checking out")
	createCmd.Flags().BoolVar(&createFlagStay, "stay", false, "create branch without checking it out")
	rootCmd.AddCommand(createCmd)
}
