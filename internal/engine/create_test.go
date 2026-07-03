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

func setupCreateTest(t *testing.T) *context.Context {
	t.Helper()
	dir := t.TempDir()
	r := &git.Runner{Dir: dir}

	r.RunGitCapture("init")
	r.RunGitCapture("config", "user.email", "test@test.com")
	r.RunGitCapture("config", "user.name", "Test")
	r.RunGitCapture("commit", "--allow-empty", "-m", "initial commit")

	gitDir, err := r.GitCommonDir()
	if err != nil {
		t.Fatalf("GitCommonDir: %v", err)
	}

	s := store.NewRefStore(r, gitDir)
	if err := s.Init(); err != nil {
		t.Fatalf("store init: %v", err)
	}

	trunk, _ := r.CurrentBranch()
	rev, _ := r.RevParse(trunk)
	cfg := &store.Config{Trunk: trunk, Remote: "origin"}
	s.WriteConfig(cfg)

	g := graph.New()
	g.AddTrunk(trunk, rev)
	s.WriteGraph(g)

	return &context.Context{Git: r, Store: s, Quiet: true}
}

func TestCreate_Default(t *testing.T) {
	c := setupCreateTest(t)
	trunk, _ := c.Git.CurrentBranch()

	err := Create(c, CreateOpts{Name: "feat-a"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	current, _ := c.Git.CurrentBranch()
	if current != "feat-a" {
		t.Fatalf("expected to be on feat-a, got %s", current)
	}

	g, _ := c.Store.ReadGraph()
	if !g.Has("feat-a") {
		t.Fatal("feat-a not in graph")
	}
	if g.Branches["feat-a"].ParentBranchName != trunk {
		t.Fatalf("expected parent %s, got %s", trunk, g.Branches["feat-a"].ParentBranchName)
	}
}

func TestCreate_Stay(t *testing.T) {
	c := setupCreateTest(t)
	trunk, _ := c.Git.CurrentBranch()

	err := Create(c, CreateOpts{Name: "feat-stay", Stay: true})
	if err != nil {
		t.Fatalf("Create --stay: %v", err)
	}

	current, _ := c.Git.CurrentBranch()
	if current != trunk {
		t.Fatalf("expected to stay on %s, got %s", trunk, current)
	}

	exists, _ := c.Git.BranchExists("feat-stay")
	if !exists {
		t.Fatal("feat-stay branch should exist")
	}

	g, _ := c.Store.ReadGraph()
	if !g.Has("feat-stay") {
		t.Fatal("feat-stay not in graph")
	}
	if g.Branches["feat-stay"].ParentBranchName != trunk {
		t.Fatalf("expected parent %s, got %s", trunk, g.Branches["feat-stay"].ParentBranchName)
	}
}

func TestCreate_Worktree(t *testing.T) {
	c := setupCreateTest(t)
	trunk, _ := c.Git.CurrentBranch()

	err := Create(c, CreateOpts{Name: "feat-wt", Worktree: true})
	if err != nil {
		t.Fatalf("Create --worktree: %v", err)
	}

	current, _ := c.Git.CurrentBranch()
	if current != trunk {
		t.Fatalf("expected to be on %s after --worktree, got %s", trunk, current)
	}

	wtPath := filepath.Join(c.Git.Dir, ".worktrees", "feat-wt")
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		t.Fatalf("worktree directory should exist at %s", wtPath)
	}

	g, _ := c.Store.ReadGraph()
	if !g.Has("feat-wt") {
		t.Fatal("feat-wt not in graph")
	}
}

func TestCreate_StayWorktree(t *testing.T) {
	c := setupCreateTest(t)
	trunk, _ := c.Git.CurrentBranch()

	err := Create(c, CreateOpts{Name: "feat-sw", Stay: true, Worktree: true})
	if err != nil {
		t.Fatalf("Create --stay --worktree: %v", err)
	}

	current, _ := c.Git.CurrentBranch()
	if current != trunk {
		t.Fatalf("expected to be on %s, got %s", trunk, current)
	}

	wtPath := filepath.Join(c.Git.Dir, ".worktrees", "feat-sw")
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		t.Fatalf("worktree directory should exist at %s", wtPath)
	}

	g, _ := c.Store.ReadGraph()
	if !g.Has("feat-sw") {
		t.Fatal("feat-sw not in graph")
	}
}

func TestCreate_StayInsert(t *testing.T) {
	c := setupCreateTest(t)
	trunk, _ := c.Git.CurrentBranch()

	if err := Create(c, CreateOpts{Name: "child1"}); err != nil {
		t.Fatalf("Create child1: %v", err)
	}
	c.Git.Checkout(trunk)
	if err := Create(c, CreateOpts{Name: "child2"}); err != nil {
		t.Fatalf("Create child2: %v", err)
	}
	c.Git.Checkout(trunk)

	err := Create(c, CreateOpts{Name: "middle", Stay: true, Insert: true})
	if err != nil {
		t.Fatalf("Create --stay --insert: %v", err)
	}

	current, _ := c.Git.CurrentBranch()
	if current != trunk {
		t.Fatalf("expected to stay on %s, got %s", trunk, current)
	}

	g, _ := c.Store.ReadGraph()
	if !g.Has("middle") {
		t.Fatal("middle not in graph")
	}

	children := g.ChildrenOf(trunk)
	if len(children) != 1 || children[0] != "middle" {
		t.Fatalf("trunk should have only 'middle' as child, got %v", children)
	}

	middleChildren := g.ChildrenOf("middle")
	if len(middleChildren) != 2 {
		t.Fatalf("middle should have 2 children, got %d: %v", len(middleChildren), middleChildren)
	}
}

func TestCreate_StayGraphCorrectness(t *testing.T) {
	c := setupCreateTest(t)
	trunk, _ := c.Git.CurrentBranch()
	trunkRev, _ := c.Git.RevParse(trunk)

	err := Create(c, CreateOpts{Name: "feat-graph", Stay: true})
	if err != nil {
		t.Fatalf("Create --stay: %v", err)
	}

	g, _ := c.Store.ReadGraph()
	b := g.Branches["feat-graph"]
	if b.ParentBranchName != trunk {
		t.Fatalf("parent should be %s, got %s", trunk, b.ParentBranchName)
	}
	if b.ParentBranchRevision != trunkRev {
		t.Fatalf("parent rev mismatch: got %s, want %s", b.ParentBranchRevision, trunkRev)
	}
	if b.BranchRevision != trunkRev {
		t.Fatalf("branch rev should equal parent rev for --stay: got %s, want %s", b.BranchRevision, trunkRev)
	}
}
