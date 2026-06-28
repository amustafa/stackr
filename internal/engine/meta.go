package engine

import (
	"fmt"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/store"
)

// PushMeta pushes stackr metadata to the remote.
func PushMeta(c *context.Context) error {
	rs, ok := c.Store.(*store.RefStore)
	if !ok {
		return fmt.Errorf("metadata push requires ref-based store — run `sr migrate` to upgrade")
	}
	cfg, err := c.Store.ReadConfig()
	if err != nil {
		return err
	}
	if !c.Quiet {
		fmt.Printf("Pushing stackr metadata to %s...\n", cfg.Remote)
	}
	if err := rs.Push(cfg.Remote); err != nil {
		return fmt.Errorf("failed to push metadata: %w", err)
	}
	if !c.Quiet {
		fmt.Println("Metadata pushed")
	}
	return nil
}

// PullMeta fetches stackr metadata from the remote and merges.
func PullMeta(c *context.Context) error {
	rs, ok := c.Store.(*store.RefStore)
	if !ok {
		return fmt.Errorf("metadata pull requires ref-based store — run `sr migrate` to upgrade")
	}
	cfg, err := c.Store.ReadConfig()
	if err != nil {
		return err
	}
	if !c.Quiet {
		fmt.Printf("Pulling stackr metadata from %s...\n", cfg.Remote)
	}
	if err := rs.Pull(cfg.Remote); err != nil {
		return fmt.Errorf("failed to pull metadata: %w", err)
	}
	if !c.Quiet {
		fmt.Println("Metadata synced")
	}
	return nil
}

// TryPushMeta is a best-effort push for use in sync/submit flows.
// Logs a warning on failure but does not return an error.
func TryPushMeta(c *context.Context) {
	rs, ok := c.Store.(*store.RefStore)
	if !ok {
		return
	}
	cfg, err := c.Store.ReadConfig()
	if err != nil {
		return
	}
	if err := rs.Push(cfg.Remote); err != nil && !c.Quiet {
		fmt.Printf("Warning: metadata push failed: %v\n", err)
	}
}

// TryPullMeta is a best-effort pull for use in sync flows.
// Logs a warning on failure but does not return an error.
func TryPullMeta(c *context.Context) {
	rs, ok := c.Store.(*store.RefStore)
	if !ok {
		return
	}
	cfg, err := c.Store.ReadConfig()
	if err != nil {
		return
	}
	if err := rs.Pull(cfg.Remote); err != nil && !c.Quiet {
		fmt.Printf("Warning: metadata pull failed: %v\n", err)
	}
}
