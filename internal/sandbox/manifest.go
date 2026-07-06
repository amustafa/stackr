// Package sandbox holds the Local-Data types for sr sandbox — the per-branch
// manifest and status. These live under <main .git>/.stackr/sandboxes/ and are
// never shared (not in refs/stackr/data). Callers pass the sandboxes directory
// (resolved from the shared git dir, e.g. ctx.Store.Root()+"/sandboxes"), so
// these types stay decoupled from git resolution.
package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Mount records one bind mount so a destroyed container can be reconstructed.
type Mount struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"readOnly,omitempty"`
}

// Manifest is the reconstruction record for a sandbox, keyed by branch.
type Manifest struct {
	Branch    string   `json:"branch"`
	Image     string   `json:"image"`
	Container string   `json:"container,omitempty"`
	Mounts    []Mount  `json:"mounts"`
	Command   []string `json:"command"`
	SessionID string   `json:"sessionId,omitempty"`
}

// manifestPath returns the manifest file path for a branch within dir.
func manifestPath(dir, branch string) string {
	return filepath.Join(dir, encodeBranch(branch)+".json")
}

// WriteManifest persists a manifest under dir (creating dir if needed).
func WriteManifest(dir string, m *Manifest) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating sandboxes dir: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}
	data = append(data, '\n')
	return writeFileAtomic(manifestPath(dir, m.Branch), data)
}

// ReadManifest loads the manifest for a branch. Returns os.ErrNotExist if none.
func ReadManifest(dir, branch string) (*Manifest, error) {
	data, err := os.ReadFile(manifestPath(dir, branch))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest for %q: %w", branch, err)
	}
	return &m, nil
}

// RemoveManifest deletes a branch's manifest. Missing is not an error.
func RemoveManifest(dir, branch string) error {
	err := os.Remove(manifestPath(dir, branch))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
