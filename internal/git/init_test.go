package git

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestInit(t *testing.T) {
	dir := t.TempDir()
	r := &Runner{Dir: dir}

	if err := r.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	gitDir := filepath.Join(dir, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		t.Fatalf(".git should exist after Init: %v", err)
	}
	if !info.IsDir() {
		t.Fatal(".git should be a directory")
	}
}

func TestIsHeadUnborn(t *testing.T) {
	dir := t.TempDir()
	r := &Runner{Dir: dir}
	r.Init()
	r.RunGitCapture("config", "user.email", "test@test.com")
	r.RunGitCapture("config", "user.name", "Test")

	if !r.IsHeadUnborn() {
		t.Fatal("expected HEAD to be unborn on fresh repo")
	}

	// Create a commit so HEAD is no longer unborn.
	r.RunGitCapture("commit", "--allow-empty", "-m", "initial")

	if r.IsHeadUnborn() {
		t.Fatal("expected HEAD to NOT be unborn after commit")
	}
}

func TestAddRemote(t *testing.T) {
	r := tempRunner(t)

	if err := r.AddRemote("upstream", "https://example.com/repo.git"); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}

	remotes, err := r.ListRemotes()
	if err != nil {
		t.Fatalf("ListRemotes: %v", err)
	}

	if !slices.Contains(remotes, "upstream") {
		t.Fatalf("expected 'upstream' in remotes, got %v", remotes)
	}
}

func TestAddRemoteDuplicate(t *testing.T) {
	r := tempRunner(t)

	if err := r.AddRemote("origin", "https://example.com/a.git"); err != nil {
		t.Fatalf("first AddRemote: %v", err)
	}

	err := r.AddRemote("origin", "https://example.com/b.git")
	if err == nil {
		t.Fatal("expected error when adding duplicate remote")
	}
}
