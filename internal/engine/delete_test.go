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
// worktree first — git refuses to delete a checked-out branch.
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

// setupTargetInMainCheckout leaves the main checkout on b (child of a) and
// parks a in a linked worktree, returning a context anchored in that worktree —
// the shape where `sr delete b` runs somewhere other than the checkout holding b.
func setupTargetInMainCheckout(t *testing.T, c *context.Context) *context.Context {
	t.Helper()
	if err := Create(c, CreateOpts{Name: "a"}); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if err := Create(c, CreateOpts{Name: "b"}); err != nil {
		t.Fatalf("create b: %v", err)
	}
	wtA := addWorktree(t, c, "a")
	return contextAt(t, canonicalPath(wtA))
}

// Deleting a branch the main checkout holds, from a worktree: the main
// checkout can't be removed, so it is moved onto trunk and the branch deleted.
// Uncommitted work in the main checkout rides along with the move.
func TestDelete_FromWorktree_TargetInMainCheckout_MovesMainOntoTrunk(t *testing.T) {
	c, trunk := newBaseRepo(t)
	cA := setupTargetInMainCheckout(t, c)
	wip := filepath.Join(c.Git.Dir, "wip.txt")
	if err := os.WriteFile(wip, []byte("wip"), 0o644); err != nil {
		t.Fatalf("write wip file: %v", err)
	}

	nav, err := Delete(cA, DeleteOpts{Name: "b"})
	if err != nil {
		t.Fatalf("delete b from a's worktree: %v", err)
	}

	if nav.Branch != "" || nav.WorktreePath != "" {
		t.Errorf("unexpected navigation %+v; the running checkout never held b", nav)
	}
	if branchExists(t, c, "b") {
		t.Error("branch b still exists after delete")
	}
	if current, _ := c.Git.CurrentBranch(); current != trunk {
		t.Errorf("main checkout is on %q, want trunk %q", current, trunk)
	}
	if data, err := os.ReadFile(wip); err != nil || string(data) != "wip" {
		t.Errorf("uncommitted file in main checkout lost across the move: %v", err)
	}
	g, _ := contextAt(t, c.Git.Dir).Store.ReadGraph()
	if g.Has("b") {
		t.Error("graph still tracks b after delete")
	}
}

// When moving the main checkout onto trunk would overwrite uncommitted work,
// git refuses — and so must delete, leaving the branch and the work in place.
func TestDelete_FromWorktree_TargetInMainCheckout_BlockedByOverwrite(t *testing.T) {
	c, trunk := newBaseRepo(t)
	commitFile(t, c, "keep.txt", "from trunk", "add keep")
	cA := setupTargetInMainCheckout(t, c)
	// b drops keep.txt, then an untracked keep.txt reappears in the main
	// checkout: checking out trunk would clobber it.
	if _, err := c.Git.RunGitCapture("rm", "-q", "keep.txt"); err != nil {
		t.Fatalf("git rm: %v", err)
	}
	if _, err := c.Git.RunGitCapture("commit", "-m", "drop keep"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	keep := filepath.Join(c.Git.Dir, "keep.txt")
	if err := os.WriteFile(keep, []byte("local scratch"), 0o644); err != nil {
		t.Fatalf("write untracked keep: %v", err)
	}

	if _, err := Delete(cA, DeleteOpts{Name: "b"}); err == nil {
		t.Fatal("delete succeeded although moving the main checkout would overwrite a local file; want error")
	}

	if !branchExists(t, c, "b") {
		t.Error("branch b was deleted despite the blocked move")
	}
	if current, _ := c.Git.CurrentBranch(); current != "b" {
		t.Errorf("main checkout is on %q, want to stay on b", current)
	}
	if data, _ := os.ReadFile(keep); string(data) != "local scratch" {
		t.Errorf("untracked keep.txt was overwritten: %q", data)
	}
	g, _ := contextAt(t, c.Git.Dir).Store.ReadGraph()
	if !g.Has("b") {
		t.Error("graph dropped b despite failed delete")
	}
	_ = trunk
}

// When trunk is already checked out in the worktree running the delete, the
// main checkout can't take the trunk branch — it detaches at trunk instead.
func TestDelete_FromTrunkWorktree_TargetInMainCheckout_DetachesMain(t *testing.T) {
	c, trunk := newBaseRepo(t)
	if err := Create(c, CreateOpts{Name: "b"}); err != nil {
		t.Fatalf("create b: %v", err)
	}
	wtTrunk := t.TempDir()
	if _, err := c.Git.RunGitCapture("worktree", "add", wtTrunk, trunk); err != nil {
		t.Fatalf("worktree add trunk: %v", err)
	}
	cTrunk := contextAt(t, canonicalPath(wtTrunk))

	if _, err := Delete(cTrunk, DeleteOpts{Name: "b"}); err != nil {
		t.Fatalf("delete b from trunk's worktree: %v", err)
	}

	if branchExists(t, c, "b") {
		t.Error("branch b still exists after delete")
	}
	if branch, err := c.Git.CurrentBranch(); err == nil {
		t.Errorf("expected main checkout detached, got branch %q", branch)
	}
	head, _ := c.Git.RevParse("HEAD")
	trunkRev, _ := c.Git.RevParse(trunk)
	if head != trunkRev {
		t.Errorf("main checkout detached at %s, want trunk %s", head, trunkRev)
	}
}
