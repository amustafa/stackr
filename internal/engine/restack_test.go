package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/git"
	"github.com/amustafa/stackr/internal/graph"
	"github.com/amustafa/stackr/internal/store"
)

// setupRestackStack builds trunk -> a -> b -> c, each with one commit, all
// tracked in the graph with parent revisions recorded as of creation time.
// It returns the context and the trunk name. The working tree is left on trunk.
func setupRestackStack(t *testing.T) (*context.Context, string) {
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

	c := &context.Context{Git: r, Store: s, Quiet: true}

	// Build the stack via the engine so parent revisions are recorded honestly.
	for _, name := range []string{"a", "b", "c"} {
		if err := Create(c, CreateOpts{Name: name}); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		if _, err := r.RunGitCapture("commit", "--allow-empty", "-m", name); err != nil {
			t.Fatalf("commit on %s: %v", name, err)
		}
		// Re-record the branch tip after committing so the graph matches reality.
		g, _ := s.ReadGraph()
		rev, _ := r.RevParse(name)
		g.Branches[name].BranchRevision = rev
		s.WriteGraph(g)
	}

	// Advance trunk so downstack branch `a` is genuinely out of date and must
	// be restacked (its stored parent revision no longer matches trunk's tip).
	r.Checkout(trunk)
	r.RunGitCapture("commit", "--allow-empty", "-m", "trunk moves")

	return c, trunk
}

// Bug #1 (root): `sr restack -d` used to ignore the flag and restack UPSTACK,
// reaching descendants it should never touch. Downstack from `b` must restack
// `b` and its ancestor `a`, and must leave the upstack branch `c` untouched.
func TestRestack_Downstack_ExcludesUpstack(t *testing.T) {
	c, _ := setupRestackStack(t)

	cBefore, _ := c.Git.RevParse("c")
	aBefore, _ := c.Git.RevParse("a")

	if err := Restack(c, RestackOpts{Branch: "b", Downstack: true}); err != nil {
		t.Fatalf("restack -d: %v", err)
	}

	// The upstack branch must be identical — downstack never rebases it.
	cAfter, _ := c.Git.RevParse("c")
	if cAfter != cBefore {
		t.Errorf("downstack restack rebased upstack branch c: %s -> %s", cBefore, cAfter)
	}

	// The ancestor must have moved — proving -d actually reached downstack
	// (the old buggy behavior would have restacked c instead of a).
	aAfter, _ := c.Git.RevParse("a")
	if aAfter == aBefore {
		t.Errorf("downstack restack did not rebase ancestor a (tip unchanged %s)", aBefore)
	}
}

// A no-flag restack is the union of --downstack and --upstack: the straight
// lineage down to trunk, the branch itself, and the full upstack subtree.
// Siblings hanging off an ancestor belong to a different lineage and stay put.
//
// Topology: trunk -> a -> b -> c -> d, with forks b -> b2 and c -> c2.
// Restacking from `c` must move a, b, c, d, and c2 — and must not touch b2.
func TestRestack_Default_LineageAndUpstack_NotAncestorSiblings(t *testing.T) {
	c, _ := setupRestackStack(t) // trunk -> a -> b -> c, trunk already advanced

	fork := func(parent, name string) {
		t.Helper()
		if err := c.Git.Checkout(parent); err != nil {
			t.Fatalf("checkout %s: %v", parent, err)
		}
		if err := Create(c, CreateOpts{Name: name}); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		if _, err := c.Git.RunGitCapture("commit", "--allow-empty", "-m", name); err != nil {
			t.Fatalf("commit on %s: %v", name, err)
		}
		g, _ := c.Store.ReadGraph()
		rev, _ := c.Git.RevParse(name)
		g.Branches[name].BranchRevision = rev
		c.Store.WriteGraph(g)
	}
	fork("c", "d")
	fork("b", "b2")
	fork("c", "c2")

	before := map[string]string{}
	for _, name := range []string{"a", "b", "c", "d", "b2", "c2"} {
		before[name], _ = c.Git.RevParse(name)
	}

	if err := Restack(c, RestackOpts{Branch: "c"}); err != nil {
		t.Fatalf("default restack: %v", err)
	}

	for _, name := range []string{"a", "b", "c", "d", "c2"} {
		if after, _ := c.Git.RevParse(name); after == before[name] {
			t.Errorf("default restack from c did not rebase %s (tip unchanged %s)", name, before[name])
		}
	}
	if after, _ := c.Git.RevParse("b2"); after != before["b2"] {
		t.Errorf("default restack from c rebased ancestor-sibling b2: %s -> %s", before["b2"], after)
	}

	// Everything restacked must actually sit on its parent's new tip; the
	// untouched sibling must now report that it needs a restack.
	g, _ := c.Store.ReadGraph()
	for _, name := range []string{"a", "b", "c", "d", "c2"} {
		if NeedsRestack(c, g, name) {
			t.Errorf("%s still needs a restack after the default restack", name)
		}
	}
	if !NeedsRestack(c, g, "b2") {
		t.Error("b2 was left behind by design but does not report needing a restack")
	}
}

// A branch checked out in another (clean) worktree must be restacked in that
// worktree rather than failing on git's "already used by worktree" lock, and
// must never leave a bogus rebase state that `sr continue` would later act on.
func TestRestack_CleanWorktree_RestacksInPlace(t *testing.T) {
	c, _ := setupRestackStack(t)

	aBefore, _ := c.Git.RevParse("a")

	// Check `a` out in a separate, clean worktree.
	wt := t.TempDir() + "/wt-a"
	if _, err := c.Git.RunGitCapture("worktree", "add", wt, "a"); err != nil {
		t.Fatalf("worktree add: %v", err)
	}

	if err := Restack(c, RestackOpts{Branch: "b", Downstack: true}); err != nil {
		t.Fatalf("restack should succeed by rebasing `a` in its own worktree: %v", err)
	}

	aAfter, _ := c.Git.RevParse("a")
	if aAfter == aBefore {
		t.Errorf("branch `a` in another worktree was not restacked (tip unchanged %s)", aBefore)
	}

	if c.Store.HasRebaseState() {
		t.Error("clean worktree restack wrote a bogus rebase state; `sr continue` would corrupt the graph")
	}
}

// A branch checked out in a DIRTY worktree cannot be cleanly restacked. Under
// sync's skip-blocked policy it and its descendants are left as-is while the
// rest of the stack still restacks; no bogus rebase state is written.
func TestRestack_DirtyWorktree_SkipsLineage(t *testing.T) {
	c, _ := setupRestackStack(t)

	aBefore, _ := c.Git.RevParse("a")
	bBefore, _ := c.Git.RevParse("b")

	wt := t.TempDir() + "/wt-a"
	if _, err := c.Git.RunGitCapture("worktree", "add", wt, "a"); err != nil {
		t.Fatalf("worktree add: %v", err)
	}
	// Dirty the worktree so `a` can't be safely rebased there.
	if err := os.WriteFile(wt+"/dirty.txt", []byte("uncommitted"), 0o644); err != nil {
		t.Fatalf("dirty worktree: %v", err)
	}

	if err := Restack(c, RestackOpts{Branch: "a", Upstack: true, SkipBlocked: true}); err != nil {
		t.Fatalf("skip-blocked restack should not error: %v", err)
	}

	// `a` (dirty worktree) and its descendant `b` must be left untouched.
	if aAfter, _ := c.Git.RevParse("a"); aAfter != aBefore {
		t.Errorf("dirty-worktree branch `a` was rebased anyway")
	}
	if bAfter, _ := c.Git.RevParse("b"); bAfter != bBefore {
		t.Errorf("descendant `b` of a blocked branch was rebased anyway")
	}
	if c.Store.HasRebaseState() {
		t.Error("skip-blocked restack wrote a rebase state; nothing is resumable here")
	}
}

// freeze marks a branch frozen in the graph.
func freeze(t *testing.T, c *context.Context, name string) {
	t.Helper()
	g, err := c.Store.ReadGraph()
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g.Branches[name].Frozen = true
	if err := c.Store.WriteGraph(g); err != nil {
		t.Fatalf("write graph: %v", err)
	}
}

// ADR-0015: Restack treats a frozen branch as a WALL. Rebasing its dependents
// onto a parent tip that was deliberately left in place is meaningless, so the
// exclusion spreads up the lineage.
func TestRestack_FrozenBranchIsAWall(t *testing.T) {
	c, trunk := setupRestackStack(t)
	freeze(t, c, "b")

	aBefore, _ := c.Git.RevParse("a")
	bBefore, _ := c.Git.RevParse("b")
	cBefore, _ := c.Git.RevParse("c")

	if err := Restack(c, RestackOpts{Branch: trunk}); err != nil {
		t.Fatalf("restack: %v", err)
	}

	aAfter, _ := c.Git.RevParse("a")
	if aAfter == aBefore {
		t.Error("branch a is below the wall and should still have been restacked")
	}
	if bAfter, _ := c.Git.RevParse("b"); bAfter != bBefore {
		t.Error("frozen branch b must not be rebased")
	}
	if cAfter, _ := c.Git.RevParse("c"); cAfter != cBefore {
		t.Error("branch c is stacked on frozen b and must not be rebased either")
	}
}

// Freezing withdraws a branch from operations that sweep over it, not from a
// direct instruction naming it.
func TestRestack_ExplicitlyNamedFrozenBranchIsRestacked(t *testing.T) {
	c, _ := setupRestackStack(t)
	freeze(t, c, "a")

	aBefore, _ := c.Git.RevParse("a")

	if err := Restack(c, RestackOpts{Branch: "a", Only: true}); err != nil {
		t.Fatalf("restack --only a: %v", err)
	}

	if aAfter, _ := c.Git.RevParse("a"); aAfter == aBefore {
		t.Error("naming a frozen branch explicitly must restack it")
	}
}

// A frozen branch is an intention, not a failure, so it must never turn a
// restack into an error — including when SkipBlocked is false.
func TestRestack_FrozenNeverErrorsWithoutSkipBlocked(t *testing.T) {
	c, trunk := setupRestackStack(t)
	freeze(t, c, "b")

	if err := Restack(c, RestackOpts{Branch: trunk, SkipBlocked: false}); err != nil {
		t.Fatalf("a frozen branch must not fail the restack: %v", err)
	}
	if c.Store.HasRebaseState() {
		t.Error("a frozen branch must not leave resumable rebase state")
	}
}

// Regression: a conflict partway through a restack used to discard the graph
// updates for the branches that had ALREADY been restacked successfully. The
// graph then claimed a base the branch no longer sat on, so the next restack
// replayed commits it already contained and conflicted for no reason.
func TestRestack_PersistsProgressWhenALaterBranchConflicts(t *testing.T) {
	c, trunk := setupRestackStack(t)

	// Make `c` conflict with trunk by touching the same file trunk will move.
	if err := c.Git.Checkout("c"); err != nil {
		t.Fatal(err)
	}
	writeAndCommit(t, c, "clash.txt", "from c\n", "c edits clash.txt")

	if err := c.Git.Checkout(trunk); err != nil {
		t.Fatal(err)
	}
	writeAndCommit(t, c, "clash.txt", "from trunk\n", "trunk edits clash.txt")

	// Restacking the whole stack: a and b succeed, c conflicts.
	err := Restack(c, RestackOpts{Branch: trunk})
	if err == nil {
		t.Skip("expected a conflict on c; environment merged it cleanly")
	}

	g, gerr := c.Store.ReadGraph()
	if gerr != nil {
		t.Fatalf("read graph: %v", gerr)
	}

	// Whatever happened to c, the branches that DID move must be recorded at
	// their new revisions — otherwise the next restack works from a stale base.
	for _, name := range []string{"a", "b"} {
		actual, rerr := c.Git.RevParse(name)
		if rerr != nil {
			t.Fatalf("rev-parse %s: %v", name, rerr)
		}
		if got := g.Branches[name].BranchRevision; got != actual {
			t.Errorf("%s: graph records %s but git says %s — progress was discarded",
				name, abbrev(got), abbrev(actual))
		}
	}

	// And a's recorded parent revision must match trunk's tip, or the next
	// restack replays a's commits onto a base it already has.
	trunkRev, _ := c.Git.RevParse(trunk)
	if got := g.Branches["a"].ParentBranchRevision; got != trunkRev {
		t.Errorf("a: recorded parent %s, want trunk tip %s", abbrev(got), abbrev(trunkRev))
	}
}

// writeAndCommit writes a file in the context's worktree and commits it.
func writeAndCommit(t *testing.T, c *context.Context, name, content, msg string) {
	t.Helper()
	if err := os.WriteFile(c.Git.Dir+"/"+name, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	if _, err := c.Git.RunGitCapture("add", name); err != nil {
		t.Fatalf("add %s: %v", name, err)
	}
	if err := c.Git.RunGit("commit", "-m", msg); err != nil {
		t.Fatalf("commit %q: %v", msg, err)
	}
}

// A branch already sitting on its parent's tip is skipped without a
// "Restacking" line. In a long stack that silence reads as the restack having
// stopped short — the lower branches simply vanish from the output — so the
// summary must account for them.
func TestRestack_ReportsAlreadyCurrentBranches(t *testing.T) {
	c, trunk := setupRestackStack(t)
	c.Quiet = false

	// Bring `a` and `b` up to date by hand; only `c` still needs to move.
	// Restack `a` and `b`, then advance `b` so `c` is stale again.
	if err := Restack(c, RestackOpts{Branch: "b", Downstack: true}); err != nil {
		t.Fatalf("prime restack: %v", err)
	}
	c.Git.Checkout("b")
	if _, err := c.Git.RunGitCapture("commit", "--allow-empty", "-m", "b moves"); err != nil {
		t.Fatalf("commit on b: %v", err)
	}
	c.Git.Checkout(trunk)

	out := captureStdout(t, func() {
		if err := Restack(c, RestackOpts{Branch: "a"}); err != nil {
			t.Fatalf("Restack: %v", err)
		}
	})

	if !strings.Contains(out, "Restacking c onto b") {
		t.Errorf("expected c to be restacked, got:\n%s", out)
	}
	if strings.Contains(out, "Restacking a onto") || strings.Contains(out, "Restacking b onto") {
		t.Errorf("a and b were already current and must not be rebased, got:\n%s", out)
	}
	if !strings.Contains(out, "Restacked 1 branch; 2 already up to date") {
		t.Errorf("expected a summary accounting for the silent branches, got:\n%s", out)
	}
	if strings.Contains(out, "Switched to branch") {
		t.Errorf("returning to the original branch must be silent, got:\n%s", out)
	}
}

// When every branch is already current the restack does nothing visible, and
// must say so rather than exit in silence.
func TestRestack_NothingToDo_SaysSo(t *testing.T) {
	c, _ := setupRestackStack(t)
	if err := Restack(c, RestackOpts{Branch: "a"}); err != nil {
		t.Fatalf("prime restack: %v", err)
	}
	c.Quiet = false

	out := captureStdout(t, func() {
		if err := Restack(c, RestackOpts{Branch: "a"}); err != nil {
			t.Fatalf("Restack: %v", err)
		}
	})
	if !strings.Contains(out, "Nothing to restack: 3 branches already up to date") {
		t.Errorf("expected an all-current summary, got:\n%s", out)
	}
}

// A restack that left branches behind still accounts for the current ones, so
// the three counts add up to the stack the user sees in `sr log`.
func TestRestack_SummaryCountsSkippedAndCurrent(t *testing.T) {
	c, trunk := setupRestackStack(t)
	if err := Restack(c, RestackOpts{Branch: "a"}); err != nil {
		t.Fatalf("prime restack: %v", err)
	}
	// Freeze b: it and c are walled off. Advance trunk so a needs a restack.
	g, _ := c.Store.ReadGraph()
	g.Branches["b"].Frozen = true
	c.Store.WriteGraph(g)
	c.Git.Checkout(trunk)
	c.Git.RunGitCapture("commit", "--allow-empty", "-m", "trunk moves again")
	c.Quiet = false

	out := captureStdout(t, func() {
		if err := Restack(c, RestackOpts{Branch: trunk}); err != nil {
			t.Fatalf("Restack: %v", err)
		}
	})
	if !strings.Contains(out, "Restacked 1 branch; left 2 unrestacked:") {
		t.Errorf("expected restacked+skipped summary, got:\n%s", out)
	}
	if !strings.Contains(out, "- b (frozen)") {
		t.Errorf("expected frozen b listed, got:\n%s", out)
	}
}

// squashMergedParentWithRebuiltChild scripts the way a stack parent lands on
// a forge: trunk T0 → parent with one commit P1 → child with one commit C1.
// P1 is squash-merged as T1; the forge rebuilds the child's branch onto T1
// (`git rebase --onto T1 P1 child`, identical patch); trunk then advances to
// T3 in a single fast-forward, so trunk's reflog never visits T1 and
// `merge-base --fork-point` has nothing to see through. The working tree is
// left on trunk and the graph still records the child under the parent.
type rebuiltChildScenario struct {
	c      *context.Context
	trunk  string
	p1     string // parent's tip, the child's recorded base
	t1     string // the squash of P1 on trunk — the child's true base after the rebuild
	t3     string // trunk's tip
	before string // combined patch-id of the child's own work before anything runs
}

func squashMergedParentWithRebuiltChild(t *testing.T) rebuiltChildScenario {
	t.Helper()
	c, trunk := newBaseRepo(t)
	t0, _ := c.Git.RevParse(trunk)

	if err := Create(c, CreateOpts{Name: "parent"}); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	commitFile(t, c, "p.txt", "parent work", "parent: P1")
	syncTip(t, c, "parent")
	p1, _ := c.Git.RevParse("parent")

	if err := Create(c, CreateOpts{Name: "child"}); err != nil {
		t.Fatalf("create child: %v", err)
	}
	commitFile(t, c, "c.txt", "child work", "child: C1")
	syncTip(t, c, "child")

	// Squash-merge P1 and keep landing other work, all off a scratch line so
	// trunk can take it in one fast-forward — the shape of `merge --ff-only
	// origin/<trunk>` after a few PRs merged while the developer was away.
	if _, err := c.Git.RunGitCapture("checkout", "-b", "scratch", t0); err != nil {
		t.Fatalf("checkout scratch: %v", err)
	}
	commitFile(t, c, "p.txt", "parent work", "parent: P1 (#1)")
	t1, _ := c.Git.RevParse("scratch")
	commitFile(t, c, "t2.txt", "t2", "trunk: T2")
	commitFile(t, c, "t3.txt", "t3", "trunk: T3")
	t3, _ := c.Git.RevParse("scratch")

	// The forge rebuilds the child onto the squash commit.
	if _, err := c.Git.RunGitCapture("rebase", "--onto", t1, p1, "child"); err != nil {
		t.Fatalf("rebuild child onto squash: %v", err)
	}
	before := patchID(t, c, t1, "child")

	if err := c.Git.Checkout(trunk); err != nil {
		t.Fatalf("checkout trunk: %v", err)
	}
	if _, err := c.Git.RunGitCapture("merge", "--ff-only", "scratch"); err != nil {
		t.Fatalf("fast-forward trunk: %v", err)
	}
	c.Git.RunGitCapture("branch", "-D", "scratch")

	if got, _ := c.Git.RevParse(trunk); got != t3 {
		t.Fatalf("trunk should sit at T3 %s, got %s", abbrev(t3), abbrev(got))
	}
	if ok, _ := c.Git.IsAncestor(p1, "child"); ok {
		t.Fatal("test setup is wrong: the rebuilt child must no longer contain P1")
	}
	if fp := c.Git.ForkPoint(trunk, "child"); fp != "" {
		t.Fatalf("test setup is wrong: trunk's reflog must not see the child's base, but fork-point found %s", abbrev(fp))
	}
	return rebuiltChildScenario{c: c, trunk: trunk, p1: p1, t1: t1, t3: t3, before: before}
}

// patchID returns the stable patch-id of the combined diff base..branch —
// the identity of a branch's own work, independent of where it sits.
func patchID(t *testing.T, c *context.Context, base, branch string) string {
	t.Helper()
	diff, err := c.Git.RunGitCapture("diff", base, branch)
	if err != nil {
		t.Fatalf("diff %s %s: %v", abbrev(base), branch, err)
	}
	cmd := exec.Command("git", "patch-id", "--stable")
	cmd.Dir = c.Git.Dir
	cmd.Stdin = strings.NewReader(diff)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("patch-id: %v", err)
	}
	return strings.Fields(string(out))[0]
}

// After a stack parent squash-merges and the forge rebuilds the child onto the
// squash commit, sync's cleanup reparents the child onto trunk. Restack must
// then succeed on its own: the child's base is recoverable as the merge-base
// with trunk, and refusing it leaves the user hand-computing a sha that git
// already knows.
func TestRestack_ParentSquashMergedAndChildRebuilt_RestacksWithoutBase(t *testing.T) {
	s := squashMergedParentWithRebuiltChild(t)
	c := s.c

	g, _ := c.Store.ReadGraph()
	cleaned := cleanMergedBranches(c, g, s.trunk, false)
	if len(cleaned) != 1 || cleaned[0].Name != "parent" {
		t.Fatalf("expected cleanup to delete [parent], got %v", cleaned)
	}
	if err := c.Store.WriteGraph(g); err != nil {
		t.Fatalf("write graph: %v", err)
	}

	if err := Restack(c, RestackOpts{Branch: "child"}); err != nil {
		t.Fatalf("restack stopped although the child's base is the merge-base with trunk: %v", err)
	}

	if ok, _ := c.Git.IsAncestor(s.t3, "child"); !ok {
		t.Error("child was not rebuilt onto trunk's tip")
	}
	count, _ := c.Git.RunGitCapture("rev-list", "--count", s.trunk+"..child")
	if count != "1" {
		t.Errorf("child should own exactly its one commit on top of trunk, got %s (P1 duplicated or C1 dropped)", count)
	}
	if after := patchID(t, c, s.trunk, "child"); after != s.before {
		t.Errorf("child's work changed across the restack: patch-id %s → %s", s.before, after)
	}

	g, _ = c.Store.ReadGraph()
	cb := g.Branches["child"]
	if cb.ParentBranchName != s.trunk || cb.ParentBranchRevision != s.t3 {
		t.Errorf("graph record after restack: parent=%s base=%s, want parent=%s base=%s",
			cb.ParentBranchName, abbrev(cb.ParentBranchRevision), s.trunk, abbrev(s.t3))
	}
}

// dirtyCurrentWorktree leaves the current checkout on `c` with an unstaged
// edit to a tracked file — the state git's rebase refuses to start over.
func dirtyCurrentWorktree(t *testing.T, c *context.Context) string {
	t.Helper()
	if err := c.Git.Checkout("c"); err != nil {
		t.Fatalf("checkout c: %v", err)
	}
	commitFile(t, c, "work.txt", "committed", "c: add work.txt")
	syncTip(t, c, "c")
	path := filepath.Join(c.Git.Dir, "work.txt")
	if err := os.WriteFile(path, []byte("in progress"), 0o644); err != nil {
		t.Fatalf("edit work.txt: %v", err)
	}
	return path
}

// Uncommitted changes in the worktree sync runs from used to surface as git's
// "cannot rebase: You have unstaged changes" — a precondition fatal that
// aborted the whole run on the first branch needing a move here. Under sync's
// skip-blocked policy those branches are now skipped like any other dirty
// worktree, and branches that live in clean worktrees still restack.
func TestRestack_DirtyCurrentWorktree_SkipsAndContinuesElsewhere(t *testing.T) {
	c, trunk := setupRestackStack(t)

	// A fourth branch on trunk, parked in its own clean worktree, that also
	// needs restacking once trunk moves again.
	if err := Create(c, CreateOpts{Name: "w"}); err != nil {
		t.Fatalf("create w: %v", err)
	}
	if _, err := c.Git.RunGitCapture("commit", "--allow-empty", "-m", "w"); err != nil {
		t.Fatalf("commit on w: %v", err)
	}
	syncTip(t, c, "w")
	if err := c.Git.Checkout(trunk); err != nil {
		t.Fatalf("checkout trunk: %v", err)
	}
	if _, err := c.Git.RunGitCapture("commit", "--allow-empty", "-m", "trunk moves again"); err != nil {
		t.Fatalf("advance trunk: %v", err)
	}
	if _, err := c.Git.RunGitCapture("worktree", "add", t.TempDir()+"/wt-w", "w"); err != nil {
		t.Fatalf("worktree add: %v", err)
	}

	aBefore, _ := c.Git.RevParse("a")
	wBefore, _ := c.Git.RevParse("w")
	edited := dirtyCurrentWorktree(t, c)

	c.Quiet = false
	out := captureStdout(t, func() {
		if err := Restack(c, RestackOpts{Branch: trunk, Upstack: true, SkipBlocked: true}); err != nil {
			t.Fatalf("skip-blocked restack should not error on a dirty current worktree: %v", err)
		}
	})

	if aAfter, _ := c.Git.RevParse("a"); aAfter != aBefore {
		t.Error("branch `a` was rebased despite uncommitted changes in the current worktree")
	}
	if wAfter, _ := c.Git.RevParse("w"); wAfter == wBefore {
		t.Error("branch `w` in its own clean worktree was not restacked; the dirty current worktree should not block it")
	}
	if !strings.Contains(out, "uncommitted changes in worktree") {
		t.Errorf("expected the summary to name the dirty worktree, got:\n%s", out)
	}
	if cur, _ := c.Git.CurrentBranch(); cur != "c" {
		t.Errorf("current branch is %q, want to stay on c", cur)
	}
	if data, _ := os.ReadFile(edited); string(data) != "in progress" {
		t.Errorf("uncommitted edit was lost or overwritten: %q", data)
	}
	if c.Git.IsRebaseInProgress() || c.Store.HasRebaseState() {
		t.Error("a rebase was left in progress; nothing should have started")
	}
}

// Without skip-blocked, the same state is a clean refusal naming the cause,
// not git's transcript — and nothing is left half-done.
func TestRestack_DirtyCurrentWorktree_RefusesCleanly(t *testing.T) {
	c, _ := setupRestackStack(t)
	aBefore, _ := c.Git.RevParse("a")
	edited := dirtyCurrentWorktree(t, c)

	err := Restack(c, RestackOpts{Branch: "a", Upstack: true})
	if err == nil {
		t.Fatal("restack over uncommitted changes succeeded; want error")
	}
	if !strings.Contains(err.Error(), "uncommitted changes in worktree") {
		t.Errorf("error should name the uncommitted changes, got: %v", err)
	}
	if aAfter, _ := c.Git.RevParse("a"); aAfter != aBefore {
		t.Error("branch `a` was rebased despite the refusal")
	}
	if data, _ := os.ReadFile(edited); string(data) != "in progress" {
		t.Errorf("uncommitted edit was lost or overwritten: %q", data)
	}
	if c.Git.IsRebaseInProgress() || c.Store.HasRebaseState() {
		t.Error("a rebase was left in progress; nothing should have started")
	}
}

// Untracked files are not a reason to hold back: git rebases over them, and a
// scratch file in the checkout must not stop sync from restacking.
func TestRestack_UntrackedFileInCurrentWorktree_DoesNotBlock(t *testing.T) {
	c, _ := setupRestackStack(t)
	aBefore, _ := c.Git.RevParse("a")
	scratch := filepath.Join(c.Git.Dir, "scratch.txt")
	if err := os.WriteFile(scratch, []byte("notes"), 0o644); err != nil {
		t.Fatalf("write scratch: %v", err)
	}

	if err := Restack(c, RestackOpts{Branch: "a", Upstack: true, SkipBlocked: true}); err != nil {
		t.Fatalf("restack with only an untracked file present: %v", err)
	}

	if aAfter, _ := c.Git.RevParse("a"); aAfter == aBefore {
		t.Error("branch `a` was not restacked; an untracked file must not block")
	}
	if _, err := os.Stat(scratch); err != nil {
		t.Errorf("untracked scratch file disappeared: %v", err)
	}
}
