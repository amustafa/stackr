package engine

import (
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

// Bug #2 (downstream): a rebase that never STARTS (branch checked out in another
// worktree -> git fatal) must not be reported as a merge conflict, and must not
// leave a bogus rebase state that `sr continue` would later act on.
func TestRestack_WorktreeCollision_NoBogusState(t *testing.T) {
	c, _ := setupRestackStack(t)

	// Check `a` out in a separate worktree so git refuses to rebase it in place.
	wt := t.TempDir() + "/wt-a"
	if _, err := c.Git.RunGitCapture("worktree", "add", wt, "a"); err != nil {
		t.Fatalf("worktree add: %v", err)
	}

	err := Restack(c, RestackOpts{Branch: "b", Downstack: true})
	if err == nil {
		t.Fatal("expected an error restacking a branch checked out in another worktree")
	}

	if c.Store.HasRebaseState() {
		t.Error("precondition failure wrote a bogus rebase state; `sr continue` would corrupt the graph")
	}
}
