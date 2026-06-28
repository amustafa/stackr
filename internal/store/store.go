package store

import (
	"encoding/json"
	"os"
	"path/filepath"

	srerr "github.com/amustafa/stackr/internal/errors"
)

const stackrDir = ".stackr"

// Store manages local-only file I/O under .git/.stackr/ (undo snapshots,
// rebase state). Used internally by RefStore; not a Backend on its own.
type Store struct {
	root string // Path to .git/.stackr/
}

// New creates a Store rooted at the given .git directory.
func New(gitDir string) *Store {
	return &Store{root: filepath.Join(gitDir, stackrDir)}
}

// Root returns the path to the .stackr directory.
func (s *Store) Root() string {
	return s.root
}

// Init creates the local .stackr directory structure for ephemeral data.
func (s *Store) Init() error {
	dirs := []string{
		s.root,
		filepath.Join(s.root, "undo"),
		filepath.Join(s.root, "undo", "snapshots"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return &srerr.StoreError{Op: "init", Path: d, Err: err}
		}
	}
	return nil
}

// Exists returns true if the local .stackr directory exists.
func (s *Store) Exists() bool {
	info, err := os.Stat(s.root)
	return err == nil && info.IsDir()
}

func (s *Store) path(name string) string {
	return filepath.Join(s.root, name)
}

func (s *Store) readJSON(name string, v any) error {
	data, err := os.ReadFile(s.path(name))
	if err != nil {
		return &srerr.StoreError{Op: "read", Path: name, Err: err}
	}
	if err := json.Unmarshal(data, v); err != nil {
		return &srerr.StoreError{Op: "parse", Path: name, Err: err}
	}
	return nil
}

func (s *Store) writeJSON(name string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return &srerr.StoreError{Op: "marshal", Path: name, Err: err}
	}
	data = append(data, '\n')
	if err := os.WriteFile(s.path(name), data, 0o644); err != nil {
		return &srerr.StoreError{Op: "write", Path: name, Err: err}
	}
	return nil
}
