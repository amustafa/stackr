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

	cleaned := cleanMergedBranches(c, g, trunk, false)

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

// setupSquashMergedFeatureWorktree builds the shape the worktree-sync tests
// need: a "feature" branch living in its own worktree, tracked in the
// graph, whose work has already been squash-merged onto remote main and whose
// remote branch is gone — exactly what a merged-and-deleted GitHub PR leaves
// behind. The primary checkout is left on main; callers move it if the case
// under test needs trunk free.
func setupSquashMergedFeatureWorktree(t *testing.T) (c *context.Context, featureCtx *context.Context, wtDir, remoteDir string) {
	t.Helper()

	c, remoteDir = setupGetTestEnv(t)

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
	return c, featureCtx, wtDir, remoteDir
}

// When sync runs from inside the very worktree holding the branch that just
// got merged, cleanMergedBranches can't remove that worktree: a running process
// can't delete the directory it's executing in. It vacates the branch instead,
// and sync must say so rather than silently leaving a stale checkout with no
// explanation.
//
// Here trunk is free, so vacating lands the worktree on trunk itself.
func TestSync_NotesStaleWorktreeWhenOwnBranchMerges(t *testing.T) {
	c, featureCtx, _, _ := setupSquashMergedFeatureWorktree(t)

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
	c, featureCtx, _, _ := setupSquashMergedFeatureWorktree(t)

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
	// git reports its own canonical path for a worktree, which differs from
	// t.TempDir() whenever TMPDIR is a symlink — as /tmp effectively is on
	// macOS. Compare resolved paths, the way sameWorktree does.
	blocking := root
	if resolved, rerr := filepath.EvalSymlinks(root); rerr == nil {
		blocking = resolved
	}
	if !strings.Contains(err.Error(), blocking) {
		t.Errorf("error should name the blocking worktree %s, got: %v", blocking, err)
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
	_, featureCtx, _, _ := setupSquashMergedFeatureWorktree(t)

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

// A merged branch may refuse to let go: if vacating the current worktree fails,
// git still holds the branch. Sync must leave it tracked rather than report it
// cleaned — dropping it from the graph while it lives on in git strands it,
// invisible to `sr log` and restacked by nothing.
func TestSync_KeepsBranchTrackedWhenVacateIsBlocked(t *testing.T) {
	c, featureCtx, wtDir, remoteDir := setupSquashMergedFeatureWorktree(t)

	// Put a file on trunk that the feature worktree holds as *untracked*
	// content. Vacating that worktree onto trunk would overwrite it, so git
	// refuses the checkout — the branch stays checked out and undeletable.
	blockerDir := t.TempDir()
	blocker := &git.Runner{Dir: blockerDir}
	blocker.RunGitCapture("clone", remoteDir, ".")
	blocker.RunGitCapture("config", "user.email", "test@test.com")
	blocker.RunGitCapture("config", "user.name", "Test")
	if err := blocker.RunGit("checkout", "main"); err != nil {
		t.Fatalf("checkout main: %v", err)
	}
	if err := os.WriteFile(filepath.Join(blockerDir, "blocker.txt"), []byte("from trunk"), 0o644); err != nil {
		t.Fatalf("write blocker on trunk: %v", err)
	}
	blocker.RunGitCapture("add", "blocker.txt")
	if err := blocker.RunGit("commit", "-m", "add blocker"); err != nil {
		t.Fatalf("commit blocker: %v", err)
	}
	if err := blocker.RunGit("push", "origin", "main"); err != nil {
		t.Fatalf("push blocker: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wtDir, "blocker.txt"), []byte("local scratch"), 0o644); err != nil {
		t.Fatalf("write untracked blocker in worktree: %v", err)
	}

	out := captureStdout(t, func() {
		if err := Sync(featureCtx, SyncOpts{}); err != nil {
			t.Fatalf("Sync should not fail over one unvacatable branch: %v", err)
		}
	})

	if !bytes.Contains([]byte(out), []byte("could not be vacated")) {
		t.Errorf("expected sync to report the blocked vacate, got output:\n%s", out)
	}
	if bytes.Contains([]byte(out), []byte("Cleaned up branch: feature")) {
		t.Errorf("sync reported a branch cleaned that git still holds, output:\n%s", out)
	}
	if exists, _ := featureCtx.Git.BranchExists("feature"); !exists {
		t.Error("branch was deleted despite the vacate failing")
	}
	g, err := c.Store.ReadGraph()
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	if !g.Has("feature") {
		t.Error("branch was dropped from the graph while it still exists in git — stranded")
	}
}

// squashMergeFeatureLocally builds the smallest landed-branch shape: a
// tracked "feature" branch whose single commit re-lands on trunk as a
// squash-style rewrite, leaving the repo checked out on trunk.
func squashMergeFeatureLocally(t *testing.T, c *context.Context, trunk string) {
	t.Helper()
	if err := Create(c, CreateOpts{Name: "feature"}); err != nil {
		t.Fatalf("create feature: %v", err)
	}
	commitFile(t, c, "feature.txt", "the feature", "feat: add feature")
	syncTip(t, c, "feature")
	if err := c.Git.Checkout(trunk); err != nil {
		t.Fatalf("checkout trunk: %v", err)
	}
	commitFile(t, c, "feature.txt", "the feature", "feat: add feature (#7)")
}

// stubConfirm replaces the deletion prompt for one test, recording every
// prompt it was shown and answering uniformly.
func stubConfirm(t *testing.T, answer bool) *[]string {
	t.Helper()
	var prompts []string
	orig := confirmDelete
	confirmDelete = func(prompt string) (bool, error) {
		prompts = append(prompts, prompt)
		return answer, nil
	}
	t.Cleanup(func() { confirmDelete = orig })
	return &prompts
}

// Deleting a branch is the most destructive thing sync does, so with an
// operator present it must be their call: a declined prompt leaves the branch
// in git, in the graph, and out of the cleaned list.
func TestCleanMergedBranches_DeclinedPrompt_KeepsBranch(t *testing.T) {
	c, trunk := newBaseRepo(t)
	squashMergeFeatureLocally(t, c, trunk)
	prompts := stubConfirm(t, false)

	g, _ := c.Store.ReadGraph()
	cleaned := cleanMergedBranches(c, g, trunk, true)

	if len(cleaned) != 0 {
		t.Fatalf("declined deletion still reported cleaned: %v", cleaned)
	}
	if len(*prompts) != 1 || !strings.Contains((*prompts)[0], "feature") {
		t.Errorf("expected one prompt naming the branch, got %q", *prompts)
	}
	if exists, _ := c.Git.BranchExists("feature"); !exists {
		t.Error("branch was deleted despite the operator declining")
	}
	if !g.Has("feature") {
		t.Error("declined branch was dropped from the graph")
	}
}

func TestCleanMergedBranches_AcceptedPrompt_Deletes(t *testing.T) {
	c, trunk := newBaseRepo(t)
	squashMergeFeatureLocally(t, c, trunk)
	prompts := stubConfirm(t, true)

	g, _ := c.Store.ReadGraph()
	cleaned := cleanMergedBranches(c, g, trunk, true)

	if len(cleaned) != 1 || cleaned[0] != "feature" {
		t.Fatalf("expected [feature] cleaned, got %v", cleaned)
	}
	if exists, _ := c.Git.BranchExists("feature"); exists {
		t.Error("accepted branch still exists after cleanup")
	}
	// The prompt must carry the evidence that nominated the branch, or the
	// operator is being asked to rubber-stamp a blind deletion.
	if len(*prompts) != 1 || !strings.Contains((*prompts)[0], trunk) {
		t.Errorf("expected the prompt to say why the branch is deletable, got %q", *prompts)
	}
}

// A branch whose PR was closed WITHOUT merging holds commits that exist on no
// other ref. It may be offered for deletion when an operator can consent, and
// must never be touched on the unprompted path.
func TestCleanMergedBranches_ClosedPR_NeedsExplicitConsent(t *testing.T) {
	c, trunk := newBaseRepo(t)
	if err := Create(c, CreateOpts{Name: "feature"}); err != nil {
		t.Fatalf("create feature: %v", err)
	}
	commitFile(t, c, "feature.txt", "unmerged work", "feat: never landed")
	syncTip(t, c, "feature")
	if err := c.Git.Checkout(trunk); err != nil {
		t.Fatalf("checkout trunk: %v", err)
	}

	origClosed := closedUnmergedHeads
	closedUnmergedHeads = func(string) map[string]int { return map[string]int{"feature": 12} }
	t.Cleanup(func() { closedUnmergedHeads = origClosed })

	// Unprompted path: the closed PR must not be acted on at all.
	g, _ := c.Store.ReadGraph()
	if cleaned := cleanMergedBranches(c, g, trunk, false); len(cleaned) != 0 {
		t.Fatalf("closed-PR branch deleted without consent: %v", cleaned)
	}
	if exists, _ := c.Git.BranchExists("feature"); !exists {
		t.Fatal("closed-PR branch deleted on the unprompted path")
	}

	// Prompted and accepted: the branch goes, and the prompt said what the
	// operator was agreeing to lose.
	prompts := stubConfirm(t, true)
	if cleaned := cleanMergedBranches(c, g, trunk, true); len(cleaned) != 1 || cleaned[0] != "feature" {
		t.Fatalf("expected [feature] cleaned after consent, got %v", cleaned)
	}
	if len(*prompts) != 1 || !strings.Contains((*prompts)[0], "closed without merging") {
		t.Errorf("expected the prompt to warn the PR closed unmerged, got %q", *prompts)
	}
	if exists, _ := c.Git.BranchExists("feature"); exists {
		t.Error("consented closed-PR branch still exists")
	}
}
