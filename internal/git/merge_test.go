package git

import (
	"os"
	"path/filepath"
	"testing"
)

// commitFile creates or modifies a file and commits it. Returns the commit SHA.
func commitFile(t *testing.T, r *Runner, name, content, msg string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.Dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	if _, err := r.RunGitCapture("add", name); err != nil {
		t.Fatalf("git add %s: %v", name, err)
	}
	if err := r.RunGit("commit", "-m", msg); err != nil {
		t.Fatalf("git commit: %v", err)
	}
	sha, err := r.RevParse("HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	return sha
}

func TestMergeFF_NotCheckedOut(t *testing.T) {
	r := tempRunner(t)

	// Create initial commit on main.
	commitFile(t, r, "a.txt", "initial", "initial commit")
	r.RunGitCapture("branch", "-M", "main")

	// Create a feature branch at the same point.
	r.RunGitCapture("branch", "feature")

	// Add a commit on main.
	commitFile(t, r, "b.txt", "second", "second commit")
	mainSHA, _ := r.RevParse("main")

	// Fast-forward feature (not checked out) to main.
	if err := r.MergeFF("feature", "main"); err != nil {
		t.Fatalf("MergeFF: %v", err)
	}

	featureSHA, _ := r.RevParse("feature")
	if featureSHA != mainSHA {
		t.Errorf("feature SHA = %s, want %s", featureSHA, mainSHA)
	}
}

func TestMergeFF_CurrentBranch(t *testing.T) {
	r := tempRunner(t)

	commitFile(t, r, "a.txt", "initial", "initial commit")
	r.RunGitCapture("branch", "-M", "main")

	// Create feature branch ahead of main.
	r.RunGitCapture("checkout", "-b", "feature")
	featureSHA := commitFile(t, r, "b.txt", "feature work", "feature commit")

	// Go back to main.
	r.RunGitCapture("checkout", "main")

	// Fast-forward main (current branch) to feature.
	if err := r.MergeFF("main", "feature"); err != nil {
		t.Fatalf("MergeFF: %v", err)
	}

	mainSHA, _ := r.RevParse("main")
	if mainSHA != featureSHA {
		t.Errorf("main SHA = %s, want %s", mainSHA, featureSHA)
	}
}

func TestMergeFF_NotDescendant(t *testing.T) {
	r := tempRunner(t)

	commitFile(t, r, "a.txt", "initial", "initial commit")
	r.RunGitCapture("branch", "-M", "main")
	r.RunGitCapture("branch", "feature")

	// Diverge: commit on main.
	commitFile(t, r, "b.txt", "main work", "main commit")

	// Diverge: commit on feature.
	r.RunGitCapture("checkout", "feature")
	commitFile(t, r, "c.txt", "feature work", "feature commit")
	r.RunGitCapture("checkout", "main")

	// MergeFF should fail — not a fast-forward.
	if err := r.MergeFF("feature", "main"); err == nil {
		t.Fatal("expected error for non-fast-forward, got nil")
	}
}

func TestMerge_Success(t *testing.T) {
	r := tempRunner(t)

	commitFile(t, r, "a.txt", "initial", "initial commit")
	r.RunGitCapture("branch", "-M", "main")

	// Create diverged branches.
	r.RunGitCapture("checkout", "-b", "feature")
	commitFile(t, r, "b.txt", "feature work", "feature commit")

	r.RunGitCapture("checkout", "main")
	commitFile(t, r, "c.txt", "main work", "main commit")

	// Merge feature into main.
	if err := r.Merge("feature"); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	// Both files should exist after merge.
	if _, err := os.Stat(filepath.Join(r.Dir, "b.txt")); err != nil {
		t.Error("b.txt missing after merge")
	}
	if _, err := os.Stat(filepath.Join(r.Dir, "c.txt")); err != nil {
		t.Error("c.txt missing after merge")
	}
}

func TestMerge_Conflict(t *testing.T) {
	r := tempRunner(t)

	commitFile(t, r, "a.txt", "initial", "initial commit")
	r.RunGitCapture("branch", "-M", "main")

	// Create diverged branches editing the same file.
	r.RunGitCapture("checkout", "-b", "feature")
	commitFile(t, r, "a.txt", "feature version", "feature commit")

	r.RunGitCapture("checkout", "main")
	commitFile(t, r, "a.txt", "main version", "main commit")

	// Merge should return MergeConflictError.
	err := r.Merge("feature")
	if err == nil {
		t.Fatal("expected merge conflict error, got nil")
	}
	if !IsMergeConflict(err) {
		t.Fatalf("expected MergeConflictError, got: %v", err)
	}

	// Verify merge is still in progress.
	if !r.IsMergeInProgress() {
		t.Error("expected merge to be in progress")
	}

	// Clean up: abort the merge.
	r.RunGit("merge", "--abort")
}

func TestHasDiverged_Identical(t *testing.T) {
	r := tempRunner(t)

	sha := commitFile(t, r, "a.txt", "initial", "initial commit")

	diverged, err := r.HasDiverged(sha, sha)
	if err != nil {
		t.Fatalf("HasDiverged: %v", err)
	}
	if diverged {
		t.Error("identical commits should not be diverged")
	}
}

func TestHasDiverged_Ancestor(t *testing.T) {
	r := tempRunner(t)

	commitFile(t, r, "a.txt", "initial", "initial commit")
	r.RunGitCapture("branch", "-M", "main")

	oldSHA, _ := r.RevParse("main")
	commitFile(t, r, "b.txt", "second", "second commit")
	newSHA, _ := r.RevParse("main")

	// old is ancestor of new — not diverged.
	diverged, err := r.HasDiverged(oldSHA, newSHA)
	if err != nil {
		t.Fatalf("HasDiverged: %v", err)
	}
	if diverged {
		t.Error("ancestor relationship should not be diverged")
	}

	// new is descendant of old — not diverged (reverse check).
	diverged, err = r.HasDiverged(newSHA, oldSHA)
	if err != nil {
		t.Fatalf("HasDiverged: %v", err)
	}
	if diverged {
		t.Error("descendant relationship should not be diverged")
	}
}

func TestHasDiverged_Diverged(t *testing.T) {
	r := tempRunner(t)

	commitFile(t, r, "a.txt", "initial", "initial commit")
	r.RunGitCapture("branch", "-M", "main")
	r.RunGitCapture("branch", "feature")

	// Diverge.
	commitFile(t, r, "b.txt", "main work", "main commit")
	mainSHA, _ := r.RevParse("main")

	r.RunGitCapture("checkout", "feature")
	commitFile(t, r, "c.txt", "feature work", "feature commit")
	featureSHA, _ := r.RevParse("feature")

	diverged, err := r.HasDiverged(mainSHA, featureSHA)
	if err != nil {
		t.Fatalf("HasDiverged: %v", err)
	}
	if !diverged {
		t.Error("branches should be diverged")
	}
}
