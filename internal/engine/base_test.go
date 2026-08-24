package engine

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/git"
	"github.com/amustafa/stackr/internal/graph"
	"github.com/amustafa/stackr/internal/store"
)

// newBaseRepo creates an initialized repo with a single trunk commit and
// returns the context plus the trunk branch name. The working tree is on trunk.
func newBaseRepo(t *testing.T) (*context.Context, string) {
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
	trunkRev, _ := r.RevParse(trunk)
	s.WriteConfig(&store.Config{Trunk: trunk, Remote: "origin"})

	g := graph.New()
	g.AddTrunk(trunk, trunkRev)
	s.WriteGraph(g)

	return &context.Context{Git: r, Store: s, Quiet: true}, trunk
}

// commitFile writes a file and commits it with raw git, without touching the
// stackr graph — the same drift a user creates by reaching for git directly.
func commitFile(t *testing.T, c *context.Context, name, content, msg string) {
	t.Helper()
	path := filepath.Join(c.Git.Dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	if _, err := c.Git.RunGitCapture("add", name); err != nil {
		t.Fatalf("add %s: %v", name, err)
	}
	if _, err := c.Git.RunGitCapture("commit", "-m", msg); err != nil {
		t.Fatalf("commit %s: %v", name, err)
	}
}

// syncTip records a branch's real tip into the graph.
func syncTip(t *testing.T, c *context.Context, name string) {
	t.Helper()
	g, _ := c.Store.ReadGraph()
	rev, _ := c.Git.RevParse(name)
	g.Branches[name].BranchRevision = rev
	if err := c.Store.WriteGraph(g); err != nil {
		t.Fatalf("write graph: %v", err)
	}
}

// filesTouchedBy returns the paths changed by a single commit.
func filesTouchedBy(t *testing.T, c *context.Context, rev string) []string {
	t.Helper()
	out, err := c.Git.RunGitCapture("show", "--name-only", "--format=", rev)
	if err != nil {
		t.Fatalf("show %s: %v", rev, err)
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			files = append(files, s)
		}
	}
	sort.Strings(files)
	return files
}

// Squash must derive its commit range from the branch's recorded BASE, not from
// the parent branch's current tip.
//
// When the parent has moved on since the branch was last restacked, the parent's
// tip is not an ancestor of HEAD. Soft-resetting there leaves the index holding
// the old tree, so the squashed commit's diff REVERTS whatever the parent gained
// in the meantime — the parent's work silently disappears inside the child's
// squashed commit.
func TestSquash_ParentMovedAhead_DoesNotRevertParentWork(t *testing.T) {
	c, trunk := newBaseRepo(t)

	if err := Create(c, CreateOpts{Name: "a"}); err != nil {
		t.Fatalf("create a: %v", err)
	}
	commitFile(t, c, "a.txt", "from a", "a: first")
	syncTip(t, c, "a")

	if err := Create(c, CreateOpts{Name: "b"}); err != nil {
		t.Fatalf("create b: %v", err)
	}
	commitFile(t, c, "b1.txt", "b one", "b: one")
	commitFile(t, c, "b2.txt", "b two", "b: two")
	syncTip(t, c, "b")

	// The parent gains a commit that `b` has not been restacked onto yet.
	if err := c.Git.Checkout("a"); err != nil {
		t.Fatalf("checkout a: %v", err)
	}
	commitFile(t, c, "a2.txt", "later work on a", "a: second")
	syncTip(t, c, "a")

	if err := c.Git.Checkout("b"); err != nil {
		t.Fatalf("checkout b: %v", err)
	}
	if err := Squash(c, SquashOpts{Message: "b: squashed", NoEdit: true}); err != nil {
		t.Fatalf("squash: %v", err)
	}

	got := filesTouchedBy(t, c, "b")
	want := []string{"b1.txt", "b2.txt"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("squashed commit touched %v, want %v\n"+
			"a2.txt appearing here means the squash reverted the parent's work", got, want)
	}

	// The parent's later commit must still be intact on the parent.
	if _, err := c.Git.RunGitCapture("cat-file", "-e", "a:a2.txt"); err != nil {
		t.Errorf("parent branch lost a2.txt: %v", err)
	}
	_ = trunk
}

// Restack must decide whether a branch is correctly stacked by asking git about
// ancestry, not by comparing the recorded base against the parent's tip.
//
// A recorded base that happens to equal the parent's current tip is not proof
// the branch sits on that tip. The old equality check treated it as proof and
// skipped the rebase, leaving the branch permanently out of date.
func TestRestack_StaleBaseMatchingParentTip_StillRestacks(t *testing.T) {
	c, _ := newBaseRepo(t)

	if err := Create(c, CreateOpts{Name: "a"}); err != nil {
		t.Fatalf("create a: %v", err)
	}
	commitFile(t, c, "a.txt", "from a", "a: first")
	syncTip(t, c, "a")

	if err := Create(c, CreateOpts{Name: "b"}); err != nil {
		t.Fatalf("create b: %v", err)
	}
	commitFile(t, c, "b.txt", "from b", "b: first")
	syncTip(t, c, "b")

	bBefore, _ := c.Git.RevParse("b")

	// Parent moves ahead; `b` is left behind.
	c.Git.Checkout("a")
	commitFile(t, c, "a2.txt", "later", "a: second")
	syncTip(t, c, "a")
	aTip, _ := c.Git.RevParse("a")

	// Corrupt the graph exactly the way the old skip-check could not survive:
	// claim b's base is a's *current* tip, which it demonstrably is not.
	g, _ := c.Store.ReadGraph()
	g.Branches["b"].ParentBranchRevision = aTip
	if err := c.Store.WriteGraph(g); err != nil {
		t.Fatalf("write graph: %v", err)
	}

	if err := Restack(c, RestackOpts{Branch: "b"}); err != nil {
		t.Fatalf("restack: %v", err)
	}

	bAfter, _ := c.Git.RevParse("b")
	if bAfter == bBefore {
		t.Fatal("restack skipped a branch that was not stacked on its parent")
	}
	if ok, _ := c.Git.IsAncestor(aTip, "b"); !ok {
		t.Error("after restack, b is still not built on a's tip")
	}
	// b must own exactly its own commit — no duplicate of the parent's work.
	if files := filesTouchedBy(t, c, "b"); strings.Join(files, ",") != "b.txt" {
		t.Errorf("restacked b touches %v, want [b.txt]", files)
	}
}

// A branch already sitting on its parent's tip needs no rebase, and restack
// should repair the recorded base rather than leave the graph lying.
func TestRestack_AlreadyStacked_SelfHealsRecordedBase(t *testing.T) {
	c, _ := newBaseRepo(t)

	if err := Create(c, CreateOpts{Name: "a"}); err != nil {
		t.Fatalf("create a: %v", err)
	}
	commitFile(t, c, "a.txt", "from a", "a: first")
	syncTip(t, c, "a")

	if err := Create(c, CreateOpts{Name: "b"}); err != nil {
		t.Fatalf("create b: %v", err)
	}
	commitFile(t, c, "b.txt", "from b", "b: first")
	syncTip(t, c, "b")

	bBefore, _ := c.Git.RevParse("b")
	aTip, _ := c.Git.RevParse("a")

	// Corrupt the base with a value that is not even a real commit.
	g, _ := c.Store.ReadGraph()
	g.Branches["b"].ParentBranchRevision = "0000000000000000000000000000000000000000"
	c.Store.WriteGraph(g)

	if err := Restack(c, RestackOpts{Branch: "b"}); err != nil {
		t.Fatalf("restack: %v", err)
	}

	if bAfter, _ := c.Git.RevParse("b"); bAfter != bBefore {
		t.Error("restack rebased a branch that was already correctly stacked")
	}
	g, _ = c.Store.ReadGraph()
	if got := g.Branches["b"].ParentBranchRevision; got != aTip {
		t.Errorf("recorded base not healed: got %s, want %s", got, aTip)
	}
}

// A squash-merged branch never becomes an ancestor of trunk, because the merge
// rewrote its commits. Ancestry-based detection misses it, the branch survives
// into the restack, and its already-landed commits get replayed onto a trunk
// that contains them — conflicting or duplicating on every commit.
func TestBranchHasLanded_SquashMergedBranch_IsDetected(t *testing.T) {
	c, trunk := newBaseRepo(t)

	if err := Create(c, CreateOpts{Name: "feature"}); err != nil {
		t.Fatalf("create feature: %v", err)
	}
	commitFile(t, c, "feature.txt", "the feature", "feat: add feature")
	syncTip(t, c, "feature")

	// Simulate GitHub's "Squash and merge": the same content lands on trunk as
	// a brand-new commit with a different SHA.
	if err := c.Git.Checkout(trunk); err != nil {
		t.Fatalf("checkout trunk: %v", err)
	}
	commitFile(t, c, "feature.txt", "the feature", "feat: add feature (#7)")

	if merged, _ := c.Git.IsMergedInto("feature", trunk); merged {
		t.Fatal("test setup is wrong: a squash merge must not be an ancestor of trunk")
	}

	g, _ := c.Store.ReadGraph()
	if _, landed := branchHasLanded(c, "feature", g.Branches["feature"], trunk, map[string]int{}, false); !landed {
		t.Error("squash-merged branch not detected as landed; sync would replay it onto trunk")
	}
}

// A parked branch — tracked but never seen to hold a commit of its own —
// fast-forwarded along trunk outside sr must not read as landed; the
// parked-branch guard in branchHasLanded explains why every other test
// would ratify deleting it.
func TestBranchHasLanded_ParkedBranchDriftedAlongTrunk_IsNotDetected(t *testing.T) {
	c, trunk := newBaseRepo(t)
	oldRev, err := c.Git.RevParse(trunk)
	if err != nil {
		t.Fatalf("rev-parse trunk: %v", err)
	}

	if _, err := c.Git.RunGitCapture("branch", "parked", oldRev); err != nil {
		t.Fatalf("branch parked: %v", err)
	}
	g, _ := c.Store.ReadGraph()
	if err := g.AddBranch("parked", trunk, oldRev, oldRev); err != nil {
		t.Fatalf("add branch: %v", err)
	}
	if err := c.Store.WriteGraph(g); err != nil {
		t.Fatalf("write graph: %v", err)
	}

	// Trunk moves on; the user keeps the parked branch fresh by hand.
	commitFile(t, c, "t1.txt", "trunk work\n", "trunk commit 1")
	commitFile(t, c, "t2.txt", "more trunk work\n", "trunk commit 2")
	newTip, _ := c.Git.RevParse(trunk)
	if _, err := c.Git.RunGitCapture("branch", "-f", "parked", newTip); err != nil {
		t.Fatalf("fast-forward parked: %v", err)
	}

	g, _ = c.Store.ReadGraph()
	if reason, landed := branchHasLanded(c, "parked", g.Branches["parked"], trunk, map[string]int{}, false); landed {
		t.Errorf("parked branch detected as landed (%s); sync would delete a branch that never held work", reason)
	}
}

// When GitHub answered the batched merged-PR query, its "not merged" is
// trusted outright and the local patch-equivalence fallbacks are skipped —
// branchHasLanded's forge-answered early return explains the cost trade-off.
func TestBranchHasLanded_ForgeAnswered_SkipsLocalFallbacks(t *testing.T) {
	c, trunk := newBaseRepo(t)

	if err := Create(c, CreateOpts{Name: "feature"}); err != nil {
		t.Fatalf("create feature: %v", err)
	}
	commitFile(t, c, "feature.txt", "the feature", "feat: add feature")
	syncTip(t, c, "feature")

	// A squash merge the local fallbacks WOULD detect (as
	// TestBranchHasLanded_SquashMergedBranch_IsDetected proves), so a
	// "landed" result here could only come from them running.
	if err := c.Git.Checkout(trunk); err != nil {
		t.Fatalf("checkout trunk: %v", err)
	}
	commitFile(t, c, "feature.txt", "the feature", "feat: add feature (#7)")

	g, _ := c.Store.ReadGraph()
	if _, landed := branchHasLanded(c, "feature", g.Branches["feature"], trunk, map[string]int{}, true); landed {
		t.Error("local fallbacks ran despite an authoritative forge answer")
	}
	if _, landed := branchHasLanded(c, "feature", g.Branches["feature"], trunk, map[string]int{"feature": 7}, true); !landed {
		t.Error("branch the forge reports merged not detected as landed")
	}
}

// git cherry — the tier-3 fallback's original mechanism — compares patch IDs
// one commit at a time, so it never matches when several branch commits are
// squashed into trunk's single commit. branchHasLanded must fall through to
// the many-to-one comparison instead of giving up.
func TestBranchHasLanded_MultiCommitSquashMergedBranch_IsDetected(t *testing.T) {
	c, trunk := newBaseRepo(t)

	if err := Create(c, CreateOpts{Name: "feature"}); err != nil {
		t.Fatalf("create feature: %v", err)
	}
	commitFile(t, c, "feature.txt", "line one\n", "feat: part one")
	commitFile(t, c, "feature.txt", "line one\nline two\n", "feat: part two")
	syncTip(t, c, "feature")

	// Simulate GitHub's "Squash and merge": both commits land on trunk as one
	// brand-new commit with the combined content and a different SHA.
	if err := c.Git.Checkout(trunk); err != nil {
		t.Fatalf("checkout trunk: %v", err)
	}
	commitFile(t, c, "feature.txt", "line one\nline two\n", "feat: add feature (#7)")

	if merged, _ := c.Git.IsMergedInto("feature", trunk); merged {
		t.Fatal("test setup is wrong: a squash merge must not be an ancestor of trunk")
	}

	g, _ := c.Store.ReadGraph()
	base, err := resolveBase(c, "feature", g.Branches["feature"])
	if err != nil {
		t.Fatalf("resolveBase: %v", err)
	}
	if landed, _ := c.Git.AllCommitsUpstream(trunk, "feature", base.SHA); landed {
		t.Fatal("test setup is wrong: per-commit patch-id comparison must not match a multi-commit squash")
	}

	if _, landed := branchHasLanded(c, "feature", g.Branches["feature"], trunk, map[string]int{}, false); !landed {
		t.Error("multi-commit squash-merged branch not detected as landed; sync would replay it onto trunk")
	}
}

// The inverse: a branch with genuinely unlanded work must never be reported as
// merged, or sync would delete unmerged commits.
func TestBranchHasLanded_UnmergedBranch_IsNotDetected(t *testing.T) {
	c, trunk := newBaseRepo(t)

	if err := Create(c, CreateOpts{Name: "feature"}); err != nil {
		t.Fatalf("create feature: %v", err)
	}
	commitFile(t, c, "feature.txt", "the feature", "feat: add feature")
	syncTip(t, c, "feature")

	g, _ := c.Store.ReadGraph()
	if _, landed := branchHasLanded(c, "feature", g.Branches["feature"], trunk, map[string]int{}, false); landed {
		t.Error("unmerged branch reported as landed; sync would delete unmerged work")
	}
}

// A branch with no commits of its own has not landed — it is simply empty — and
// must not be cleaned up as merged.
func TestBranchHasLanded_EmptyBranch_IsNotDetected(t *testing.T) {
	c, trunk := newBaseRepo(t)

	if err := Create(c, CreateOpts{Name: "empty"}); err != nil {
		t.Fatalf("create empty: %v", err)
	}

	g, _ := c.Store.ReadGraph()
	if _, landed := branchHasLanded(c, "empty", g.Branches["empty"], trunk, map[string]int{}, false); landed {
		t.Error("branch with no commits reported as landed")
	}
}

// resolveBase must refuse to guess when the recorded base is unusable and the
// reflog cannot recover it. Returning a plausible-but-wrong base is worse than
// failing: it silently duplicates or drops commits.
func TestResolveBase_Unrecoverable_FailsLoudly(t *testing.T) {
	c, _ := newBaseRepo(t)

	if err := Create(c, CreateOpts{Name: "a"}); err != nil {
		t.Fatalf("create a: %v", err)
	}
	commitFile(t, c, "a.txt", "from a", "a: first")
	syncTip(t, c, "a")

	g, _ := c.Store.ReadGraph()
	g.Branches["a"].ParentBranchRevision = ""
	c.Store.WriteGraph(g)

	// Destroy the reflog so fork-point recovery cannot succeed either.
	c.Git.RunGitCapture("reflog", "expire", "--expire=now", "--all")

	_, err := resolveBase(c, "a", g.Branches["a"])
	if err == nil {
		t.Fatal("resolveBase invented a base instead of reporting it as unrecoverable")
	}
	var bue *BaseUnresolvedError
	if !asBaseUnresolved(err, &bue) {
		t.Fatalf("wrong error type: %T (%v)", err, err)
	}
	if !strings.Contains(err.Error(), "--base") {
		t.Error("error should point the user at the --base escape hatch")
	}
}

func asBaseUnresolved(err error, target **BaseUnresolvedError) bool {
	if e, ok := err.(*BaseUnresolvedError); ok {
		*target = e
		return true
	}
	return false
}
