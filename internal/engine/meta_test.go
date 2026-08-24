package engine

import (
	"strings"
	"testing"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/git"
	"github.com/amustafa/stackr/internal/graph"
	"github.com/amustafa/stackr/internal/store"
)

// setupSeededRemote creates a bare remote holding stackr metadata (as if a
// collaborator initialized and pushed), plus a fresh clone that — like every
// real clone — has no refs/stackr/data and no .stackr dir.
func setupSeededRemote(t *testing.T) (fresh *context.Context) {
	t.Helper()

	remoteDir := t.TempDir()
	remote := &git.Runner{Dir: remoteDir}
	if _, err := remote.RunGitCapture("init", "--bare"); err != nil {
		t.Fatalf("git init bare: %v", err)
	}

	// Seeder clone: initialize metadata and push it.
	seedDir := t.TempDir()
	seeder := &git.Runner{Dir: seedDir}
	if _, err := seeder.RunGitCapture("clone", remoteDir, "."); err != nil {
		t.Fatalf("clone seeder: %v", err)
	}
	seeder.RunGitCapture("config", "user.email", "seed@test.com")
	seeder.RunGitCapture("config", "user.name", "Seeder")
	seedGitDir, _ := seeder.GitCommonDir()
	seedStore := store.NewRefStore(seeder, seedGitDir)
	if err := seedStore.WriteConfig(&store.Config{Trunk: "main", Remote: "origin"}); err != nil {
		t.Fatalf("seed WriteConfig: %v", err)
	}
	g := graph.New()
	g.AddTrunk("main", "aaa")
	g.AddBranch("feat-a", "main", "aaa", "bbb")
	if err := seedStore.WriteGraph(g); err != nil {
		t.Fatalf("seed WriteGraph: %v", err)
	}
	if err := seedStore.Push("origin"); err != nil {
		t.Fatalf("seed Push: %v", err)
	}

	// Fresh clone: what a new collaborator actually has.
	freshDir := t.TempDir()
	runner := &git.Runner{Dir: freshDir}
	if _, err := runner.RunGitCapture("clone", remoteDir, "."); err != nil {
		t.Fatalf("clone fresh: %v", err)
	}
	runner.RunGitCapture("config", "user.email", "new@test.com")
	runner.RunGitCapture("config", "user.name", "Newcomer")
	gitDir, _ := runner.GitCommonDir()

	return &context.Context{
		Git:   runner,
		Store: store.NewRefStore(runner, gitDir),
		Quiet: true,
	}
}

// A fresh clone of an sr-managed repo must be able to bootstrap itself with
// pull-meta alone — requiring init first was a catch-22, and running init
// would shadow the shared graph with a blank one.
func TestPullMeta_BootstrapsUninitializedClone(t *testing.T) {
	c := setupSeededRemote(t)

	if c.Store.Exists() {
		t.Fatal("precondition: fresh clone must start uninitialized")
	}

	if err := PullMeta(c); err != nil {
		t.Fatalf("PullMeta on uninitialized clone: %v", err)
	}

	if !c.Store.Exists() {
		t.Fatal("store must exist after bootstrap pull")
	}
	g, err := c.Store.ReadGraph()
	if err != nil {
		t.Fatalf("ReadGraph after bootstrap: %v", err)
	}
	if !g.Has("main") || !g.Has("feat-a") {
		t.Fatalf("shared graph not adopted; branches: %v", g.Branches)
	}
	cfg, err := c.Store.ReadConfig()
	if err != nil {
		t.Fatalf("ReadConfig after bootstrap: %v", err)
	}
	if cfg.Trunk != "main" {
		t.Fatalf("trunk = %q, want main", cfg.Trunk)
	}
	// Local scaffolding (undo/rollback) must exist too — pull replaces init.
	if rs, ok := c.Store.(*store.RefStore); ok {
		if !rs.Exists() {
			t.Fatal("RefStore.Exists false after bootstrap")
		}
	}
}

// A clone whose remote has no stackr metadata cannot bootstrap; pull-meta must
// say so and point at sr init instead of reporting a silent success.
func TestPullMeta_UninitializedWithEmptyRemoteErrors(t *testing.T) {
	remoteDir := t.TempDir()
	remote := &git.Runner{Dir: remoteDir}
	if _, err := remote.RunGitCapture("init", "--bare"); err != nil {
		t.Fatalf("git init bare: %v", err)
	}
	dir := t.TempDir()
	runner := &git.Runner{Dir: dir}
	if _, err := runner.RunGitCapture("clone", remoteDir, "."); err != nil {
		t.Fatalf("clone: %v", err)
	}
	gitDir, _ := runner.GitCommonDir()
	c := &context.Context{
		Git:   runner,
		Store: store.NewRefStore(runner, gitDir),
		Quiet: true,
	}

	err := PullMeta(c)
	if err == nil {
		t.Fatal("PullMeta must fail when neither clone nor remote has metadata")
	}
	if !strings.Contains(err.Error(), "sr init") {
		t.Fatalf("error should point at sr init, got: %v", err)
	}
	if c.Store.Exists() {
		t.Fatal("failed bootstrap must not leave a half-initialized store")
	}
}
