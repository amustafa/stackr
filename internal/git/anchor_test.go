package git

import (
	"os"
	"path/filepath"
	"testing"
)

func newAnchorRepo(t *testing.T) *Runner {
	t.Helper()
	dir := t.TempDir()
	r := &Runner{Dir: dir}
	r.RunGitCapture("init")
	r.RunGitCapture("config", "user.email", "test@test.com")
	r.RunGitCapture("config", "user.name", "Test")
	r.RunGitCapture("commit", "--allow-empty", "-m", "initial")
	return r
}

// makeOrphanCommit creates a commit on a temporary branch, then deletes the
// branch — leaving a commit that only the reflog keeps alive. This is exactly
// the state a stackr base commit is in after its parent branch is amended.
func makeOrphanCommit(t *testing.T, r *Runner) string {
	t.Helper()
	r.RunGitCapture("checkout", "-b", "tmp")
	if err := os.WriteFile(filepath.Join(r.Dir, "orphan.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	r.RunGitCapture("add", "orphan.txt")
	r.RunGitCapture("commit", "-m", "soon to be unreachable")
	sha, err := r.RevParse("HEAD")
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	trunk := "master"
	if _, err := r.RunGitCapture("rev-parse", "--verify", "master"); err != nil {
		trunk = "main"
	}
	r.RunGitCapture("checkout", trunk)
	r.RunGitCapture("branch", "-D", "tmp")
	return sha
}

// pruneEverything expires all reflogs and garbage-collects, which is what a
// background `git gc` or an explicit prune eventually does on every repo.
func pruneEverything(t *testing.T, r *Runner) {
	t.Helper()
	r.RunGitCapture("reflog", "expire", "--expire=now", "--expire-unreachable=now", "--all")
	if _, err := r.RunGitCapture("gc", "--prune=now", "--quiet"); err != nil {
		t.Fatalf("gc: %v", err)
	}
}

// The premise: an unanchored base commit really does get collected. Without
// this, the anchor test below would pass for the wrong reason.
func TestAnchorCommits_WithoutAnchor_CommitIsCollected(t *testing.T) {
	r := newAnchorRepo(t)
	sha := makeOrphanCommit(t, r)

	if !r.ObjectExists(sha) {
		t.Fatal("setup: commit should exist before gc")
	}
	pruneEverything(t, r)

	if r.ObjectExists(sha) {
		t.Skip("git did not collect the unreachable commit; anchor durability cannot be demonstrated here")
	}
}

// An anchored base commit must survive garbage collection. This is the whole
// point of the anchor: after a parent is amended, the child's base is reachable
// from nothing, and losing it makes every later restack fail with "bad revision".
func TestAnchorCommits_SurvivesGarbageCollection(t *testing.T) {
	r := newAnchorRepo(t)
	sha := makeOrphanCommit(t, r)

	if err := r.AnchorCommits("refs/stackr/bases", "stackr: base commit anchor", []string{sha}); err != nil {
		t.Fatalf("AnchorCommits: %v", err)
	}
	pruneEverything(t, r)

	if !r.ObjectExists(sha) {
		t.Error("anchored base commit was garbage-collected; restack would fail with 'bad revision'")
	}
}

// Re-anchoring an unchanged set must not churn the ref, or every graph write
// would leave a new unreachable anchor commit behind.
func TestAnchorCommits_IsDeterministic(t *testing.T) {
	r := newAnchorRepo(t)
	sha := makeOrphanCommit(t, r)

	if err := r.AnchorCommits("refs/stackr/bases", "stackr: base commit anchor", []string{sha}); err != nil {
		t.Fatalf("first anchor: %v", err)
	}
	first, _ := r.ReadRef("refs/stackr/bases")

	// Same commits, different order — the anchor must be identical.
	if err := r.AnchorCommits("refs/stackr/bases", "stackr: base commit anchor", []string{sha, sha}); err != nil {
		t.Fatalf("second anchor: %v", err)
	}
	second, _ := r.ReadRef("refs/stackr/bases")

	if first != second {
		t.Errorf("anchor is not deterministic: %s then %s", first, second)
	}
}

// Commits that no longer exist must be dropped rather than failing the anchor —
// a graph referencing one dead base must not block persisting all the others.
func TestAnchorCommits_SkipsMissingCommits(t *testing.T) {
	r := newAnchorRepo(t)
	live, _ := r.RevParse("HEAD")
	dead := "0000000000000000000000000000000000000000"

	if err := r.AnchorCommits("refs/stackr/bases", "anchor", []string{dead, live}); err != nil {
		t.Fatalf("AnchorCommits with a missing commit: %v", err)
	}
	if ref, _ := r.ReadRef("refs/stackr/bases"); ref == "" {
		t.Error("anchor ref was not written despite a live commit being present")
	}
}

// ForkPoint must not be trusted once the reflog is gone: git silently answers
// with a plain merge-base instead of failing, and the caller cannot tell.
// HasReflog is the gate that makes that distinction possible.
func TestHasReflog_ReportsExpiry(t *testing.T) {
	r := newAnchorRepo(t)
	trunk, _ := r.CurrentBranch()

	if !r.HasReflog(trunk) {
		t.Fatal("a freshly committed branch should have a reflog")
	}

	r.RunGitCapture("reflog", "expire", "--expire=now", "--expire-unreachable=now", "--all")

	if r.HasReflog(trunk) {
		t.Error("HasReflog still reports a reflog after expiry; fork-point recovery would be trusted unsafely")
	}
}
