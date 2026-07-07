package sandbox

import (
	"path/filepath"
	"testing"
)

func TestProjectSlug(t *testing.T) {
	// Verified against a real ~/.claude/projects entry.
	cases := map[string]string{
		"/home/amustafa/workspace/ftron/.worktrees/am-add-observer-struct": "-home-amustafa-workspace-ftron--worktrees-am-add-observer-struct",
		"/home/amustafa/workspace":                                         "-home-amustafa-workspace",
	}
	for in, want := range cases {
		if got := ProjectSlug(in); got != want {
			t.Errorf("ProjectSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMainRoot(t *testing.T) {
	if got := MainRoot("/home/amustafa/workspace/stackr/.git"); got != "/home/amustafa/workspace/stackr" {
		t.Errorf("MainRoot from .git = %q", got)
	}
	// Non-".git" tail (unusual layout) is returned unchanged.
	if got := MainRoot("/some/bare/repo"); got != "/some/bare/repo" {
		t.Errorf("MainRoot bare = %q", got)
	}
}

func TestWorktreePathCanonicalizes(t *testing.T) {
	root := t.TempDir()
	// Non-existent worktree → unresolved join (no error).
	want := filepath.Join(root, ".worktrees", "feat-x")
	if got := WorktreePath(root, "feat-x"); got != want {
		t.Errorf("WorktreePath(non-existent) = %q, want %q", got, want)
	}
}

func TestLocalConfigMissingIsEmpty(t *testing.T) {
	c, err := LoadLocalConfig(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil || c == nil || len(c.PathMounts) != 0 {
		t.Fatalf("missing local config should be empty, no error: %v %+v", err, c)
	}
}

func TestLocalConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sandbox.local.json")
	in := &LocalConfig{
		CachePaths: []string{"/home/me/.cache/go-build"},
		PathMounts: []string{"/home/me/tools/bin"},
		ExtraMounts: []Mount{{Source: "/data", Target: "/data", ReadOnly: true}},
	}
	if err := in.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := LoadLocalConfig(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.PathMounts) != 1 || got.PathMounts[0] != "/home/me/tools/bin" || !got.ExtraMounts[0].ReadOnly {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}
