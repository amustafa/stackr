package engine

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/git"
)

// captureStdout runs fn with os.Stdout redirected and returns what it wrote.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

// A branch created and worked on in its own worktree (as `git worktree add -b`
// or `sr create --worktree` would leave it) must not survive sync as an
// orphaned checkout once its branch is merged and cleaned up: git refuses to
// force-delete a branch checked out elsewhere, and even where it doesn't, a
// worktree pointing at a branch the graph no longer knows about is stray
// debris. cleanMergedBranches must remove the worktree, not just the branch.
func TestCleanMergedBranches_RemovesWorktreeOfMergedBranch(t *testing.T) {
	c, trunk := newBaseRepo(t)
	trunkRev, err := c.Git.RevParse(trunk)
	if err != nil {
		t.Fatalf("rev-parse trunk: %v", err)
	}

	wtDir := t.TempDir()
	if _, err := c.Git.RunGitCapture("worktree", "add", wtDir, "-b", "feature"); err != nil {
		t.Fatalf("worktree add: %v", err)
	}

	wtCtx := &context.Context{Git: &git.Runner{Dir: wtDir}, Quiet: true}
	commitFile(t, wtCtx, "feature.txt", "the feature", "feat: add feature")

	featureRev, err := wtCtx.Git.RevParse("feature")
	if err != nil {
		t.Fatalf("rev-parse feature: %v", err)
	}

	g, err := c.Store.ReadGraph()
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	if err := g.AddBranch("feature", trunk, trunkRev, featureRev); err != nil {
		t.Fatalf("add branch: %v", err)
	}
	if err := c.Store.WriteGraph(g); err != nil {
		t.Fatalf("write graph: %v", err)
	}

	// Simulate GitHub's "Squash and merge" on trunk, in the primary checkout —
	// same content, a brand-new commit with a different SHA.
	commitFile(t, c, "feature.txt", "the feature", "feat: add feature (#7)")

	cleaned := cleanMergedBranches(c, g, trunk)

	if len(cleaned) != 1 || cleaned[0] != "feature" {
		t.Fatalf("expected [feature] cleaned, got %v", cleaned)
	}
	if exists, _ := c.Git.BranchExists("feature"); exists {
		t.Error("merged branch still exists after cleanup")
	}
	if wtPath, _ := c.Git.WorktreeForBranch("feature"); wtPath != "" {
		t.Errorf("worktree %s for merged branch was not removed", wtPath)
	}
	entries, err := c.Git.WorktreeList()
	if err != nil {
		t.Fatalf("worktree list: %v", err)
	}
	for _, e := range entries {
		if sameWorktree(e.Path, wtDir) {
			t.Errorf("worktree %s still registered after merged-branch cleanup", wtDir)
		}
	}
}

// setupSquashMergedFeatureWorktree builds the shape that both worktree-sync
// tests need: a "feature" branch living in its own worktree, tracked in the
// graph, whose work has already been squash-merged onto remote main and whose
// remote branch is gone — exactly what a merged-and-deleted GitHub PR leaves
// behind. The primary checkout is left on main; callers move it if the case
// under test needs trunk free.
func setupSquashMergedFeatureWorktree(t *testing.T) (c *context.Context, featureCtx *context.Context, wtDir string) {
	t.Helper()

	c, remoteDir := setupGetTestEnv(t)

	wtDir = t.TempDir()
	if _, err := c.Git.RunGitCapture("worktree", "add", wtDir, "-b", "feature", "main"); err != nil {
		t.Fatalf("worktree add: %v", err)
	}

	featureCtx = &context.Context{Git: &git.Runner{Dir: wtDir}, Store: c.Store, Quiet: false}
	commitFile(t, featureCtx, "feature.txt", "the feature", "feat: add feature")
	if err := featureCtx.Git.RunGit("push", "-u", "origin", "feature"); err != nil {
		t.Fatalf("push feature: %v", err)
	}

	g, err := c.Store.ReadGraph()
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	mainRev, _ := c.Git.RevParse("main")
	featureRev, _ := featureCtx.Git.RevParse("feature")
	if err := g.AddBranch("feature", "main", mainRev, featureRev); err != nil {
		t.Fatalf("add branch: %v", err)
	}
	if err := c.Store.WriteGraph(g); err != nil {
		t.Fatalf("write graph: %v", err)
	}

	// Simulate GitHub's "Squash and merge": push the equivalent content to
	// remote main directly, from a throwaway clone, and delete remote feature
	// — exactly what happens when a PR is squash-merged and its branch
	// deleted on GitHub.
	mergeDir := t.TempDir()
	mergeRunner := &git.Runner{Dir: mergeDir}
	mergeRunner.RunGitCapture("clone", remoteDir, ".")
	mergeRunner.RunGitCapture("config", "user.email", "test@test.com")
	mergeRunner.RunGitCapture("config", "user.name", "Test")
	if err := mergeRunner.RunGit("checkout", "main"); err != nil {
		t.Fatalf("checkout main: %v", err)
	}
	if err := mergeRunner.RunGit("merge", "--squash", "origin/feature"); err != nil {
		t.Fatalf("squash merge: %v", err)
	}
	if err := mergeRunner.RunGit("commit", "-m", "feat: add feature (#7)"); err != nil {
		t.Fatalf("commit squash: %v", err)
	}
	if err := mergeRunner.RunGit("push", "origin", "main"); err != nil {
		t.Fatalf("push main: %v", err)
	}
	if err := mergeRunner.RunGit("push", "origin", "--delete", "feature"); err != nil {
		t.Fatalf("delete remote feature: %v", err)
	}
	return c, featureCtx, wtDir
}

// When sync runs from inside the very worktree holding the branch that just
// got merged, cleanMergedBranches can't remove that worktree: a running process
// can't delete the directory it's executing in. It vacates the branch instead,
// and sync must say so rather than silently leaving a stale checkout with no
// explanation.
//
// Here trunk is free, so vacating lands the worktree on trunk itself.
func TestSync_NotesStaleWorktreeWhenOwnBranchMerges(t *testing.T) {
	c, featureCtx, _ := setupSquashMergedFeatureWorktree(t)

	// Free up "main" in the primary checkout so the feature worktree's own
	// sync can check it out — mirroring a primary checkout that's on some
	// other branch when the feature worktree runs its own sync.
	if err := c.Git.RunGit("checkout", "-b", "other-work"); err != nil {
		t.Fatalf("checkout other-work: %v", err)
	}

	// Now sync from inside the feature worktree itself — the exact scenario
	// that left a stale worktree behind in practice.
	out := captureStdout(t, func() {
		if err := Sync(featureCtx, SyncOpts{}); err != nil {
			t.Fatalf("Sync: %v", err)
		}
	})

	if branch, _ := featureCtx.Git.CurrentBranch(); branch != "main" {
		t.Errorf("expected feature worktree to end up on main, got %q", branch)
	}
	if !bytes.Contains([]byte(out), []byte("feature")) || !bytes.Contains([]byte(out), []byte("sr worktree remove")) {
		t.Errorf("expected sync to note the stale worktree, got output:\n%s", out)
	}
}

// Trunk can only be fast-forwarded inside the worktree that holds it, so that
// worktree's state can block the update. Everything downstream measures against
// trunk — which branches have landed, what the stack rebases onto — so sync
// stops and says which worktree is in the way rather than quietly doing less
// than asked and reporting success.
func TestSync_FailsWhenTrunkWorktreeBlocksFastForward(t *testing.T) {
	c, featureCtx, _ := setupSquashMergedFeatureWorktree(t)

	// The primary checkout holds main and has an untracked feature.txt, which
	// the incoming squash commit adds — git refuses to fast-forward over it.
	root := c.Git.Dir
	if err := os.WriteFile(filepath.Join(root, "feature.txt"), []byte("local scratch"), 0o644); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}

	err := Sync(featureCtx, SyncOpts{})
	if err == nil {
		t.Fatal("expected sync to fail when trunk's worktree blocks the fast-forward")
	}
	if !strings.Contains(err.Error(), root) {
		t.Errorf("error should name the blocking worktree %s, got: %v", root, err)
	}
	if exists, _ := featureCtx.Git.BranchExists("feature"); !exists {
		t.Error("sync deleted the merged branch despite failing to update trunk")
	}
}

// The ordinary shape of a stackr repo: the primary checkout sits on trunk while
// feature branches live in worktrees. Sync used to open by checking out trunk
// unconditionally, so running it from a worktree died on git's "'main' is
// already used by worktree" before it fetched, restacked, or cleaned anything —
// and the merged branch survived. Sync must complete without ever claiming
// trunk here, vacating this worktree by detaching instead.
func TestSync_FromWorktreeWhileTrunkCheckedOutElsewhere(t *testing.T) {
	_, featureCtx, _ := setupSquashMergedFeatureWorktree(t)

	// Primary checkout stays on main — no freeing trunk this time.
	out := captureStdout(t, func() {
		if err := Sync(featureCtx, SyncOpts{}); err != nil {
			t.Fatalf("Sync from worktree while trunk is checked out elsewhere: %v", err)
		}
	})

	if exists, _ := featureCtx.Git.BranchExists("feature"); exists {
		t.Error("merged branch survived sync run from its own worktree")
	}
	// Trunk belongs to the primary checkout, so this worktree can only detach.
	if branch, err := featureCtx.Git.CurrentBranch(); err == nil {
		t.Errorf("expected detached HEAD, got branch %q", branch)
	}
	head, _ := featureCtx.Git.RevParse("HEAD")
	trunkRev, _ := featureCtx.Git.RevParse("main")
	if head != trunkRev {
		t.Errorf("expected worktree detached at trunk %s, got %s", trunkRev, head)
	}
	if !bytes.Contains([]byte(out), []byte("sr worktree remove")) {
		t.Errorf("expected sync to note the stale worktree, got output:\n%s", out)
	}
}
