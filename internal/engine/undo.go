package engine

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/graph"
)

// Undo restores the branch graph to the state before the last mutation.
func Undo(c *context.Context) error {
	event, data, err := c.Store.PopSnapshot()
	if err != nil {
		return err
	}

	// Parse the snapshot.
	var g graph.Graph
	if err := json.Unmarshal(data, &g); err != nil {
		return fmt.Errorf("corrupt snapshot: %w", err)
	}

	// Write restored graph.
	if err := c.Store.WriteGraph(&g); err != nil {
		return err
	}

	// Try to restore git branches.
	// For branches in the snapshot that don't exist in git, we can't fully restore.
	// This is best-effort for the graph state.

	if !c.Quiet {
		fmt.Printf("Undid %s on %s\n", event.Operation, event.Branch)
	}
	return nil
}

// SaveUndoPoint creates a snapshot before a mutation.
func SaveUndoPoint(c *context.Context, operation, branch string) {
	// Best effort — don't fail the operation if undo save fails.
	if _, err := os.Stat(c.Store.Root()); err != nil {
		return
	}
	_ = c.Store.SaveSnapshot(operation, branch)
}
