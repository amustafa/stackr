package engine

import (
	"fmt"

	"github.com/amustafa/stackr/internal/context"
)

// Reorder provides an interactive way to reorder branches in a stack.
// For now, this is a placeholder that shows the stack order.
func Reorder(c *context.Context) error {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}

	current, err := c.Git.CurrentBranch()
	if err != nil {
		return err
	}

	stack := g.StackOf(current)
	if len(stack) <= 1 {
		return fmt.Errorf("stack has only %d branch(es) — nothing to reorder", len(stack))
	}

	fmt.Println("Current stack order:")
	for i, name := range stack {
		marker := "  "
		if name == current {
			marker = "→ "
		}
		fmt.Printf("%s%d. %s\n", marker, i+1, name)
	}

	fmt.Println("\n(Interactive reorder coming soon — use `sr move` to move individual branches)")
	return nil
}
