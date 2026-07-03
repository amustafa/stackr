package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/git"
	"github.com/amustafa/stackr/internal/graph"
	"github.com/amustafa/stackr/internal/store"
)

func setupGetTestEnv(t *testing.T) (local *context.Context, remoteDir string) {
	t.Helper()

	remoteDir = t.TempDir()
	remote := &git.Runner{Dir: remoteDir}
	if _, err := remote.RunGitCapture("init", "--bare"); err != nil {
		t.Fatalf("bare init: %v", err)
	}

	localDir := t.TempDir()
	localRunner := &git.Runner{Dir: localDir}
	if _, err := localRunner.RunGitCapture("clone", remoteDir, "."); err != nil {
		t.Fatalf("clone: %v", err)
	}
	localRunner.RunGitCapture("config", "user.email", "test@test.com")
	localRunner.RunGitCapture("config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(localDir, "init.txt"), []byte("init"), 0o644); err != nil {
		t.Fatalf("write init.txt: %v", err)
	}
	localRunner.RunGitCapture("add", "init.txt")
	if err := localRunner.RunGit("commit", "-m", "initial"); err != nil {
		t.Fatalf("initial commit: %v", err)
	}
	localRunner.RunGitCapture("branch", "-M", "main")
	if err := localRunner.RunGit("push", "origin", "main"); err != nil {
		t.Fatalf("push main: %v", err)
	}

	gitDir, _ := localRunner.GitCommonDir()
	s := store.NewRefStore(localRunner, gitDir)
	s.Init()

	g := graph.New()
	mainRev, _ := localRunner.RevParse("main")
	g.AddTrunk("main", mainRev)
	s.WriteGraph(g)
	s.WriteConfig(&store.Config{Remote: "origin"})

	ctx := &context.Context{
		Git:         localRunner,
		Store:       s,
		Interactive: false,
		Quiet:       true,
	}

	return ctx, remoteDir
}

func addRemoteBranch(t *testing.T, remoteDir, branch, file, content string) {
	t.Helper()
	tmpDir := t.TempDir()
	r := &git.Runner{Dir: tmpDir}
	r.RunGitCapture("clone", remoteDir, ".")
	r.RunGitCapture("config", "user.email", "test@test.com")
	r.RunGitCapture("config", "user.name", "Test")
	r.RunGitCapture("checkout", "-b", branch, "origin/main")
	os.WriteFile(filepath.Join(tmpDir, file), []byte(content), 0o644)
	r.RunGitCapture("add", file)
	r.RunGit("commit", "-m", "add "+file)
	r.RunGit("push", "origin", branch)
}

func TestGet_SimpleFFBranch(t *testing.T) {
	c, remoteDir := setupGetTestEnv(t)

	addRemoteBranch(t, remoteDir, "feat-a", "a.txt", "feature a")

	g, _ := c.Store.ReadGraph()
	mainRev, _ := c.Git.RevParse("main")
	g.AddBranch("feat-a", "main", mainRev, mainRev)
	c.Store.WriteGraph(g)

	c.Git.RunGit("branch", "feat-a", "main")

	result, err := Get(c, GetOpts{Branch: "feat-a", Force: true, Stay: true})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if len(result.Synced) != 1 || result.Synced[0] != "feat-a" {
		t.Errorf("expected feat-a synced, got %v", result.Synced)
	}

	if _, err := os.Stat(filepath.Join(c.Git.Dir, "a.txt")); err == nil {
		t.Error("a.txt should not be in working dir (--stay, not checked out)")
	}
}

func TestGet_NewBranchFromRemote(t *testing.T) {
	c, remoteDir := setupGetTestEnv(t)

	addRemoteBranch(t, remoteDir, "feat-new", "new.txt", "new feature")

	result, err := Get(c, GetOpts{Branch: "feat-new", Force: true, Stay: true})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	exists, _ := c.Git.BranchExists("feat-new")
	if !exists {
		t.Error("feat-new should exist locally after get")
	}

	g, _ := c.Store.ReadGraph()
	if !g.Has("feat-new") {
		t.Error("feat-new should be tracked in the graph")
	}

	_ = result
}

func TestGet_UpToDate(t *testing.T) {
	c, remoteDir := setupGetTestEnv(t)

	addRemoteBranch(t, remoteDir, "feat-a", "a.txt", "feature a")

	c.Git.Fetch("origin")
	c.Git.RunGit("branch", "feat-a", "origin/feat-a")

	g, _ := c.Store.ReadGraph()
	rev, _ := c.Git.RevParse("feat-a")
	mainRev, _ := c.Git.RevParse("main")
	g.AddBranch("feat-a", "main", mainRev, rev)
	c.Store.WriteGraph(g)

	result, err := Get(c, GetOpts{Branch: "feat-a", Stay: true})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if len(result.Skipped) != 1 || result.Skipped[0] != "feat-a" {
		t.Errorf("expected feat-a skipped (up-to-date), got synced=%v skipped=%v", result.Synced, result.Skipped)
	}
}

func TestGet_GuardsRebaseState(t *testing.T) {
	c, _ := setupGetTestEnv(t)

	c.Store.WriteRebaseState(&store.RebaseState{
		Operation:     "restack",
		OrigBranch:    "main",
		CurrentBranch: "feat-a",
	})

	_, err := Get(c, GetOpts{Branch: "main"})
	if err == nil {
		t.Fatal("expected error when rebase state exists")
	}

	c.Store.ClearRebaseState()
}

func TestGet_GuardsGetState(t *testing.T) {
	c, _ := setupGetTestEnv(t)

	c.Store.WriteGetState(&store.GetState{
		Operation: "get",
		Target:    "feat-a",
	})

	_, err := Get(c, GetOpts{Branch: "main"})
	if err == nil {
		t.Fatal("expected error when get state exists")
	}

	c.Store.ClearGetState()
}

func TestGet_DownstackOnly(t *testing.T) {
	c, remoteDir := setupGetTestEnv(t)

	addRemoteBranch(t, remoteDir, "feat-a", "a.txt", "feature a")
	addRemoteBranch(t, remoteDir, "feat-b", "b.txt", "feature b")

	c.Git.Fetch("origin")
	c.Git.RunGit("branch", "feat-a", "origin/feat-a")
	c.Git.RunGit("branch", "feat-b", "origin/feat-b")

	g, _ := c.Store.ReadGraph()
	mainRev, _ := c.Git.RevParse("main")
	aRev, _ := c.Git.RevParse("feat-a")
	bRev, _ := c.Git.RevParse("feat-b")
	g.AddBranch("feat-a", "main", mainRev, aRev)
	g.AddBranch("feat-b", "feat-a", aRev, bRev)
	c.Store.WriteGraph(g)

	result, err := Get(c, GetOpts{Branch: "feat-a", Downstack: true, Stay: true})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	for _, s := range result.Synced {
		if s == "feat-b" {
			t.Error("feat-b should not be synced with --downstack")
		}
	}
	for _, s := range result.Created {
		if s == "feat-b" {
			t.Error("feat-b should not be created with --downstack")
		}
	}
}

func TestComputeWalkPath(t *testing.T) {
	g := graph.New()
	g.AddTrunk("main", "abc")
	g.AddBranch("feat-a", "main", "abc", "def")
	g.AddBranch("feat-b", "feat-a", "def", "ghi")
	g.AddBranch("feat-c", "feat-b", "ghi", "jkl")

	path := computeWalkPath(g, "feat-c")
	expected := []string{"feat-a", "feat-b", "feat-c"}
	if len(path) != len(expected) {
		t.Fatalf("walk path length = %d, want %d", len(path), len(expected))
	}
	for i, name := range expected {
		if path[i] != name {
			t.Errorf("walk path[%d] = %q, want %q", i, path[i], name)
		}
	}
}

func TestComputeWalkPath_SingleBranch(t *testing.T) {
	g := graph.New()
	g.AddTrunk("main", "abc")
	g.AddBranch("feat-a", "main", "abc", "def")

	path := computeWalkPath(g, "feat-a")
	if len(path) != 1 || path[0] != "feat-a" {
		t.Errorf("walk path = %v, want [feat-a]", path)
	}
}
