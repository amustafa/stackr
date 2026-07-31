package engine

import (
	"testing"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/git"
)

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
