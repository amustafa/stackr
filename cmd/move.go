package cmd

import (
	"fmt"

	"github.com/amustafa/stackr/internal/engine"
	"github.com/amustafa/stackr/internal/graph"
	"github.com/amustafa/stackr/internal/ui"
	"github.com/spf13/cobra"
)

var moveCmd = &cobra.Command{
	Use:   "move",
	Short: "Move a branch onto a new parent",
	Long: `Move a branch onto a new parent.

A move is a graph repoint followed by a restack: once the parent changes,
ordinary restack mechanics take hold, including conflict handling and the
frozen wall. Pass --no-restack to repoint without replaying commits.

With no --onto, an interactive picker shows the stacks with illegal targets
greyed out and the reason alongside.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}

		onto := moveFlagOnto
		if onto == "" {
			var err error
			onto, err = pickMoveTarget(moveFlagSource)
			if err != nil {
				return err
			}
		}

		return engine.Move(ctx, engine.MoveOpts{
			Onto:      onto,
			Source:    moveFlagSource,
			NoRestack: moveFlagNoRestack,
		})
	},
}

// pickMoveTarget resolves the move target interactively.
//
// The source is validated before the picker opens: making someone choose a
// target and only then telling them the branch cannot be moved wastes the
// choice. Likewise a source with no legal target gets a sentence rather than a
// menu it cannot answer — that is the normal outcome for the bottom branch of a
// single stack, where trunk is already the parent and everything else is a
// descendant.
func pickMoveTarget(source string) (string, error) {
	if !ctx.Interactive {
		return "", fmt.Errorf("--onto is required")
	}

	g, err := ctx.Store.ReadGraph()
	if err != nil {
		return "", err
	}
	if source == "" {
		source, err = ctx.Git.CurrentBranch()
		if err != nil {
			return "", err
		}
	}
	if err := engine.ValidateMoveSource(g, source); err != nil {
		return "", err
	}

	reasons := make(map[string]string, len(g.Branches))
	eligible := 0
	for _, t := range engine.MoveTargets(g, source) {
		reasons[t.Branch] = t.Reason
		if t.Reason == "" {
			eligible++
		}
	}
	if eligible == 0 {
		return "", fmt.Errorf("no valid target for %q — trunk is already its parent and every other branch depends on it", source)
	}

	// ShowAll because a move may cross stacks, and a picker that cannot reach
	// what --onto can reach is one people learn to distrust.
	rows := g.RenderTreeRows(graph.RenderOpts{
		CurrentBranch: source,
		ShowAll:       true,
	})

	items := make([]ui.TreeItem, len(rows))
	for i, r := range rows {
		items[i] = ui.TreeItem{Line: r.Line, Value: r.Branch, Reason: reasons[r.Branch]}
	}

	// SkipWhenSingle is deliberately not set: a lone candidate still gets
	// confirmed, because this picker commits to a rebase.
	return ui.TreeSelect(ui.TreeSelectOpts{
		Title: fmt.Sprintf("Move %s onto:", source),
		Items: items,
	})
}

var (
	moveFlagOnto      string
	moveFlagSource    string
	moveFlagNoRestack bool
)

func init() {
	moveCmd.Flags().StringVarP(&moveFlagOnto, "onto", "o", "", "target parent branch (prompts if omitted)")
	moveCmd.Flags().StringVarP(&moveFlagSource, "source", "s", "", "branch to move (default: current)")
	moveCmd.Flags().BoolVar(&moveFlagNoRestack, "no-restack", false, "repoint the graph without restacking")
	rootCmd.AddCommand(moveCmd)
}
