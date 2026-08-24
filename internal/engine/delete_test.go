package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/git"
	"github.com/amustafa/stackr/internal/store"
)

// contextAt returns a context anchored at dir, sharing the repo's ref store —
// the state `sr` sees when run from a linked worktree.
func contextAt(t *testing.T, dir string) *context.Context {
	t.Helper()
	r := &git.Runner{Dir: dir}
	gitDir, err := r.GitCommonDir()
	if err != nil {
		t.Fatalf("git common dir: %v", err)
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(dir, gitDir)
	}
	return &context.Context{Git: r, Store: store.NewRefStore(r, gitDir), Quiet: true}
}

// addWorktree parks a branch in a linked worktree and returns the worktree path.
// The current checkout must not have the branch checked out.
func addWorktree(t *testing.T, c *context.Context, name string) string {
	t.Helper()
	if err := WorktreeAdd(c, WorktreeAddOpts{Name: name}); err != nil {
		t.Fatalf("worktree add %s: %v", name, err)
	}
	wtPath, err := c.Git.WorktreeForBranch(name)
	if err != nil || wtPath == "" {
		t.Fatalf("worktree for %s not found: %v", name, err)
	}
	return wtPath
}

func branchExists(t *testing.T, c *context.Context, name string) bool {
	t.Helper()
	exists, err := c.Git.BranchExists(name)
	if err != nil {
		t.Fatalf("branch exists %s: %v", name, err)
	}
	return exists
}

// Deleting a branch that is checked out in a linked worktree must remove the
// worktree first — git refuses to delete a checked-out branch, so the old
// behavior failed with "checked out in a worktree".
func TestDelete_BranchInWorktree_RemovesWorktreeAndBranch(t *testing.T) {
	c, trunk := newBaseRepo(t)

	if err := Create(c, CreateOpts{Name: "a"}); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if err := c.Git.Checkout(trunk); err != nil {
		t.Fatalf("checkout trunk: %v", err)
	}
	wtPath := addWorktree(t, c, "a")

	if _, err := Delete(c, DeleteOpts{Name: "a"}); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if branchExists(t, c, "a") {
		t.Error("branch a still exists after delete")
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("worktree at %s still exists after delete", wtPath)
	}
	g, _ := c.Store.ReadGraph()
	if g.Has("a") {
		t.Error("graph still tracks a after delete")
	}
}

// A dirty worktree must abort the delete before anything is removed.
func TestDelete_DirtyWorktree_Fails(t *testing.T) {
	c, trunk := newBaseRepo(t)

	if err := Create(c, CreateOpts{Name: "a"}); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if err := c.Git.Checkout(trunk); err != nil {
		t.Fatalf("checkout trunk: %v", err)
	}
	wtPath := addWorktree(t, c, "a")
	if err := os.WriteFile(filepath.Join(wtPath, "wip.txt"), []byte("wip"), 0o644); err != nil {
		t.Fatalf("write wip file: %v", err)
	}

	if _, err := Delete(c, DeleteOpts{Name: "a"}); err == nil {
		t.Fatal("delete of branch with dirty worktree succeeded; want error")
	}

	if !branchExists(t, c, "a") {
		t.Error("branch a was deleted despite dirty worktree")
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("worktree at %s was removed despite being dirty", wtPath)
	}
	g, _ := c.Store.ReadGraph()
	if !g.Has("a") {
		t.Error("graph dropped a despite failed delete")
	}
}

func TestDelete_Trunk_Fails(t *testing.T) {
	c, trunk := newBaseRepo(t)
	if _, err := Delete(c, DeleteOpts{Name: trunk}); err == nil {
		t.Fatal("deleting trunk succeeded; want error")
	}
}

func TestDelete_Untracked_Fails(t *testing.T) {
	c, _ := newBaseRepo(t)
	if _, err := Delete(c, DeleteOpts{Name: "nope"}); err == nil {
		t.Fatal("deleting untracked branch succeeded; want error")
	}
}

// Deleting the current branch steps down the stack to the parent, like `sr down`.
func TestDelete_CurrentBranch_NavigatesDown(t *testing.T) {
	c, _ := newBaseRepo(t)

	if err := Create(c, CreateOpts{Name: "a"}); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if err := Create(c, CreateOpts{Name: "b"}); err != nil {
		t.Fatalf("create b: %v", err)
	}

	nav, err := Delete(c, DeleteOpts{})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	if nav.Branch != "a" {
		t.Errorf("navigated to %q, want %q", nav.Branch, "a")
	}
	current, _ := c.Git.CurrentBranch()
	if current != "a" {
		t.Errorf("current branch is %q, want %q", current, "a")
	}
	if branchExists(t, c, "b") {
		t.Error("branch b still exists after delete")
	}
}

// --upstack removes the worktrees of every deleted branch too.
func TestDelete_Upstack_RemovesWorktrees(t *testing.T) {
	c, trunk := newBaseRepo(t)

	if err := Create(c, CreateOpts{Name: "a"}); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if err := Create(c, CreateOpts{Name: "b"}); err != nil {
		t.Fatalf("create b: %v", err)
	}
	if err := c.Git.Checkout(trunk); err != nil {
		t.Fatalf("checkout trunk: %v", err)
	}
	wtPath := addWorktree(t, c, "b")

	if _, err := Delete(c, DeleteOpts{Name: "a", Upstack: true}); err != nil {
		t.Fatalf("delete --upstack: %v", err)
	}

	for _, b := range []string{"a", "b"} {
		if branchExists(t, c, b) {
			t.Errorf("branch %s still exists after upstack delete", b)
		}
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("worktree at %s still exists after upstack delete", wtPath)
	}
	g, _ := c.Store.ReadGraph()
	if g.Has("a") || g.Has("b") {
		t.Error("graph still tracks deleted branches")
	}
}

// Running `sr delete` from inside the target's own worktree, with the parent
// checked out in another worktree: the target's worktree is removed, the
// navigation points at the parent's worktree, and the operation survives its
// own cwd disappearing.
func TestDelete_FromOwnWorktree_ParentInAnotherWorktree(t *testing.T) {
	c, trunk := newBaseRepo(t)

	if err := Create(c, CreateOpts{Name: "a"}); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if err := Create(c, CreateOpts{Name: "b"}); err != nil {
		t.Fatalf("create b: %v", err)
	}
	if err := c.Git.Checkout(trunk); err != nil {
		t.Fatalf("checkout trunk: %v", err)
	}
	wtA := addWorktree(t, c, "a")
	wtB := addWorktree(t, c, "b")

	cB := contextAt(t, canonicalPath(wtB))
	nav, err := Delete(cB, DeleteOpts{})
	if err != nil {
		t.Fatalf("delete from own worktree: %v", err)
	}

	if nav.Branch != "a" {
		t.Errorf("navigated to %q, want %q", nav.Branch, "a")
	}
	if got := canonicalPath(nav.WorktreePath); got != canonicalPath(wtA) {
		t.Errorf("navigation points at %q, want parent worktree %q", got, wtA)
	}
	if _, err := os.Stat(wtB); !os.IsNotExist(err) {
		t.Errorf("worktree at %s still exists after delete", wtB)
	}
	if branchExists(t, c, "b") {
		t.Error("branch b still exists after delete")
	}
	// Read the graph through a fresh store — c.Store's blob cache predates the
	// write and would show the old graph, which a new process never sees.
	g, _ := contextAt(t, c.Git.Dir).Store.ReadGraph()
	if g.Has("b") {
		t.Error("graph still tracks b after delete")
	}
	current, _ := c.Git.CurrentBranch()
	if current != trunk {
		t.Errorf("main checkout moved to %q, want to stay on trunk %q", current, trunk)
	}
}

// The same scenario with uncommitted changes in the target's worktree must
// refuse and leave everything in place.
func TestDelete_FromOwnWorktree_Dirty_Fails(t *testing.T) {
	c, trunk := newBaseRepo(t)

	if err := Create(c, CreateOpts{Name: "a"}); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if err := Create(c, CreateOpts{Name: "b"}); err != nil {
		t.Fatalf("create b: %v", err)
	}
	if err := c.Git.Checkout(trunk); err != nil {
		t.Fatalf("checkout trunk: %v", err)
	}
	addWorktree(t, c, "a")
	wtB := addWorktree(t, c, "b")
	if err := os.WriteFile(filepath.Join(wtB, "wip.txt"), []byte("wip"), 0o644); err != nil {
		t.Fatalf("write wip file: %v", err)
	}

	cB := contextAt(t, canonicalPath(wtB))
	if _, err := Delete(cB, DeleteOpts{}); err == nil {
		t.Fatal("delete from dirty own worktree succeeded; want error")
	}

	if !branchExists(t, c, "b") {
		t.Error("branch b was deleted despite dirty worktree")
	}
	if _, err := os.Stat(wtB); err != nil {
		t.Errorf("worktree at %s was removed despite being dirty", wtB)
	}
}

// Deleting the current branch from the main checkout when the parent lives in
// a worktree: the checkout must be parked off the doomed branch (on trunk) and
// the navigation must point at the parent's worktree.
func TestDelete_CurrentBranch_ParentInWorktree(t *testing.T) {
	c, trunk := newBaseRepo(t)

	if err := Create(c, CreateOpts{Name: "a"}); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if err := Create(c, CreateOpts{Name: "b"}); err != nil {
		t.Fatalf("create b: %v", err)
	}
	// Park the parent in a worktree; the main checkout stays on b.
	wtPath := addWorktree(t, c, "a")

	nav, err := Delete(c, DeleteOpts{})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	if !nav.IsWorktree() {
		t.Fatalf("expected navigation into parent worktree, got %+v", nav)
	}
	if got := canonicalPath(nav.WorktreePath); got != canonicalPath(wtPath) {
		t.Errorf("navigated to %q, want %q", got, wtPath)
	}
	current, _ := c.Git.CurrentBranch()
	if current != trunk {
		t.Errorf("main checkout is on %q, want trunk %q", current, trunk)
	}
	if branchExists(t, c, "b") {
		t.Error("branch b still exists after delete")
	}
}
