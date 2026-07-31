package engine

import (
	"bytes"
	"io"
	"os"
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

// When sync runs from inside the very worktree holding the branch that just
// got merged, cleanMergedBranches can't remove that worktree: Sync's earlier
// checkout to trunk has already repurposed it, and a running process can't
// delete the directory it's executing in. Sync must at least say so instead
// of silently leaving a stale trunk checkout with no explanation.
func TestSync_NotesStaleWorktreeWhenOwnBranchMerges(t *testing.T) {
	c, remoteDir := setupGetTestEnv(t)

	wtDir := t.TempDir()
	if _, err := c.Git.RunGitCapture("worktree", "add", wtDir, "-b", "feature", "main"); err != nil {
		t.Fatalf("worktree add: %v", err)
	}

	featureCtx := &context.Context{Git: &git.Runner{Dir: wtDir}, Store: c.Store, Quiet: false}
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

	// Free up "main" in the primary checkout so the feature worktree's own
	// sync can check it out — mirroring a primary checkout that's on some
	// other branch when the feature worktree runs its own sync.
	if err := c.Git.RunGit("checkout", "-b", "other-work"); err != nil {
		t.Fatalf("checkout other-work: %v", err)
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
