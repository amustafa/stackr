package engine

import (
	"os"
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
