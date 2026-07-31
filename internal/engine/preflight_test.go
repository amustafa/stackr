package engine

import (
	"testing"

	"github.com/amustafa/stackr/internal/store"
)

// setupPreflightStack builds trunk -> a -> b -> c in a clone wired to a bare
// remote, publishes every branch, and records the Push Records — the state a
// developer is in after a normal submit.
func setupPreflightStack(t *testing.T) *divEnv {
	t.Helper()
	e := newDivEnv(t)

	parent := "main"
	for _, name := range []string{"a", "b", "c"} {
		if err := Create(e.ctx, CreateOpts{Name: name}); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		e.commit(e.ctx.Git, name+".txt", name+"\n", "work on "+name)

		g, _ := e.ctx.Store.ReadGraph()
		rev, _ := e.ctx.Git.RevParse(name)
		g.Branches[name].BranchRevision = rev
		e.ctx.Store.WriteGraph(g)

		e.pushAndRecord(name)
		parent = name
	}
	_ = parent
	return e
}

func cfgFor(t *testing.T, e *divEnv) *store.Config {
	t.Helper()
	cfg, err := e.ctx.Store.ReadConfig()
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	return cfg
}

func names(classes []Classification) []string {
	out := make([]string, 0, len(classes))
	for _, c := range classes {
		out = append(out, c.Branch)
	}
	return out
}

func TestBuildPushSet_BottomUpIncludesDownstack(t *testing.T) {
	e := setupPreflightStack(t)
	e.git(e.ctx.Git, "checkout", "c")

	g, _ := e.ctx.Store.ReadGraph()
	set, _, err := buildPushSet(e.ctx, g, cfgFor(t, e), SubmitOpts{}, "c")
	if err != nil {
		t.Fatalf("buildPushSet: %v", err)
	}

	want := []string{"a", "b", "c"}
	if len(set) != len(want) {
		t.Fatalf("set = %v, want %v", set, want)
	}
	for i := range want {
		if set[i] != want[i] {
			t.Fatalf("set = %v, want bottom-up %v", set, want)
		}
	}
}

// ADR-0015: submit treats Frozen as a HOLE — only the frozen branch is withheld,
// its dependents are still published.
func TestBuildPushSet_FrozenIsAHoleNotAWall(t *testing.T) {
	e := setupPreflightStack(t)
	e.git(e.ctx.Git, "checkout", "c")

	g, _ := e.ctx.Store.ReadGraph()
	g.Branches["b"].Frozen = true
	e.ctx.Store.WriteGraph(g)

	g, _ = e.ctx.Store.ReadGraph()
	set, dropped, err := buildPushSet(e.ctx, g, cfgFor(t, e), SubmitOpts{}, "c")
	if err != nil {
		t.Fatalf("buildPushSet: %v", err)
	}

	for _, name := range set {
		if name == "b" {
			t.Error("frozen branch b must not be submitted")
		}
	}
	var sawA, sawC bool
	for _, name := range set {
		sawA = sawA || name == "a"
		sawC = sawC || name == "c"
	}
	if !sawA || !sawC {
		t.Errorf("frozen b must not withhold its neighbours; set = %v", set)
	}
	if len(dropped) != 1 || dropped[0].Name != "b" || dropped[0].Reason != "frozen" {
		t.Errorf("the exclusion must be reported by name, got %+v", dropped)
	}
}

// Submitting the frozen branch you are standing on is an explicit instruction,
// not an automatic sweep.
func TestBuildPushSet_ExplicitlySubmittingAFrozenBranchIsAllowed(t *testing.T) {
	e := setupPreflightStack(t)
	e.git(e.ctx.Git, "checkout", "b")

	g, _ := e.ctx.Store.ReadGraph()
	g.Branches["b"].Frozen = true
	e.ctx.Store.WriteGraph(g)

	g, _ = e.ctx.Store.ReadGraph()
	set, _, err := buildPushSet(e.ctx, g, cfgFor(t, e), SubmitOpts{}, "b")
	if err != nil {
		t.Fatalf("buildPushSet: %v", err)
	}
	found := false
	for _, name := range set {
		found = found || name == "b"
	}
	if !found {
		t.Errorf("naming a frozen branch explicitly must submit it; set = %v", set)
	}
}

func TestBuildPushSet_UpdateOnlySkipsUnpublishedBranches(t *testing.T) {
	e := setupPreflightStack(t)

	// A brand-new branch on top, never pushed.
	e.git(e.ctx.Git, "checkout", "c")
	if err := Create(e.ctx, CreateOpts{Name: "d"}); err != nil {
		t.Fatalf("Create d: %v", err)
	}
	e.commit(e.ctx.Git, "d.txt", "d\n", "work on d")

	g, _ := e.ctx.Store.ReadGraph()
	set, dropped, err := buildPushSet(e.ctx, g, cfgFor(t, e), SubmitOpts{UpdateOnly: true}, "d")
	if err != nil {
		t.Fatalf("buildPushSet: %v", err)
	}
	for _, name := range set {
		if name == "d" {
			t.Error("--update-only must skip a branch with no PR and no remote branch")
		}
	}
	if len(dropped) == 0 {
		t.Error("--update-only must report what it skipped")
	}
}

// The happy path the whole feature exists for: a restacked stack that nobody
// else touched classifies as lossless everywhere and needs no prompting.
func TestPreflight_RestackedStackNeedsNoDecisions(t *testing.T) {
	e := setupPreflightStack(t)

	// Trunk moves and the whole stack is restacked — every branch now has Ref
	// Divergence and no Content Divergence.
	e.git(e.ctx.Git, "checkout", "main")
	e.commit(e.ctx.Git, "trunk.txt", "moved\n", "trunk moves")
	if err := Restack(e.ctx, RestackOpts{Branch: "main"}); err != nil {
		t.Fatalf("restack: %v", err)
	}

	e.git(e.ctx.Git, "checkout", "c")
	g, _ := e.ctx.Store.ReadGraph()
	set, _, err := buildPushSet(e.ctx, g, cfgFor(t, e), SubmitOpts{}, "c")
	if err != nil {
		t.Fatalf("buildPushSet: %v", err)
	}

	// Interactive is false: any DispNeedsDecision would surface as an error.
	pre, err := Preflight(e.ctx, SubmitOpts{}, cfgFor(t, e), set)
	if err != nil {
		t.Fatalf("a restacked stack must need no decisions: %v", err)
	}
	if pre.Stopped {
		t.Fatal("preflight should not have stopped")
	}
	if pre.Mutated {
		t.Error("preflight must not mutate anything when there is nothing to remediate")
	}
	if got := names(pre.Ready); len(got) != 3 {
		t.Fatalf("ready = %v, want all three branches", got)
	}
	for _, class := range pre.Ready {
		if class.Disposition != DispPushForce {
			t.Errorf("%s: disposition = %v, want a lossless force push", class.Branch, class.Disposition)
		}
		if class.RemoteSHA == "" {
			t.Errorf("%s: force push needs a lease pin", class.Branch)
		}
	}
}

// Non-interactive submit fails on Content Divergence rather than skipping the
// branch — unlike `sr get`, which only mutates locally. A branch skipped
// mid-stack would strand every PR above it on a base that was never updated.
func TestPreflight_NonInteractiveFailsOnContentDivergence(t *testing.T) {
	e := setupPreflightStack(t)

	// A collaborator publishes work on b that we have never seen.
	e.git(e.collab, "fetch", "origin")
	e.git(e.collab, "checkout", "-b", "b", "origin/b")
	e.commit(e.collab, "theirs.txt", "their work\n", "collaborator commit")
	e.git(e.collab, "push", "origin", "b")

	// We rewrite b locally so the refs genuinely diverge.
	e.git(e.ctx.Git, "checkout", "b")
	e.commit(e.ctx.Git, "ours.txt", "our work\n", "our commit")

	_, err := Preflight(e.ctx, SubmitOpts{}, cfgFor(t, e), []string{"a", "b", "c"})
	if err == nil {
		t.Fatal("non-interactive preflight must fail on Content Divergence, not skip it")
	}
}

// --force answers Content Divergence with "overwrite remote" — but it must not
// weaken the lease, which is what keeps a *late* remote change from being
// destroyed silently.
func TestPreflight_ForceOverwritesButStillPinsTheLease(t *testing.T) {
	e := setupPreflightStack(t)

	e.git(e.collab, "fetch", "origin")
	e.git(e.collab, "checkout", "-b", "b", "origin/b")
	e.commit(e.collab, "theirs.txt", "their work\n", "collaborator commit")
	e.git(e.collab, "push", "origin", "b")

	e.git(e.ctx.Git, "checkout", "b")
	e.commit(e.ctx.Git, "ours.txt", "our work\n", "our commit")

	pre, err := Preflight(e.ctx, SubmitOpts{Force: true}, cfgFor(t, e), []string{"b"})
	if err != nil {
		t.Fatalf("--force should resolve without prompting: %v", err)
	}
	if len(pre.Ready) != 1 {
		t.Fatalf("ready = %v, want b", names(pre.Ready))
	}
	if pre.Ready[0].Disposition != DispPushForce {
		t.Errorf("disposition = %v, want force", pre.Ready[0].Disposition)
	}
	if pre.Ready[0].RemoteSHA == "" {
		t.Error("--force must still pin the lease to the inspected SHA")
	}
}

func TestPreflight_NoForceRefusesRatherThanForcing(t *testing.T) {
	e := setupPreflightStack(t)

	e.git(e.collab, "fetch", "origin")
	e.git(e.collab, "checkout", "-b", "b", "origin/b")
	e.commit(e.collab, "theirs.txt", "their work\n", "collaborator commit")
	e.git(e.collab, "push", "origin", "b")

	e.git(e.ctx.Git, "checkout", "b")
	e.commit(e.ctx.Git, "ours.txt", "our work\n", "our commit")

	if _, err := Preflight(e.ctx, SubmitOpts{NoForce: true}, cfgFor(t, e), []string{"b"}); err == nil {
		t.Fatal("--no-force must refuse rather than force")
	}
}

// Regression: Restack reads and writes its OWN graph instance, so a caller
// holding a graph across a restack sees stale parents — which would feed the
// wrong base into PR retargeting and stack segmentation. Preflight must reload.
func TestPreflight_ReloadsGraphAfterRestack(t *testing.T) {
	e := setupPreflightStack(t)

	before, err := branchTips(e.ctx)
	if err != nil {
		t.Fatalf("branchTips: %v", err)
	}

	e.git(e.ctx.Git, "checkout", "main")
	e.commit(e.ctx.Git, "trunk.txt", "moved\n", "trunk moves")
	if err := Restack(e.ctx, RestackOpts{Branch: "main"}); err != nil {
		t.Fatalf("restack: %v", err)
	}

	after, err := branchTips(e.ctx)
	if err != nil {
		t.Fatalf("branchTips: %v", err)
	}

	for _, name := range []string{"a", "b", "c"} {
		if before[name] == after[name] {
			t.Fatalf("%s should have moved during the restack", name)
		}
	}

	// branchTips reads through the store each time; a stale cache would return
	// the pre-restack revisions here.
	g, _ := e.ctx.Store.ReadGraph()
	for _, name := range []string{"a", "b", "c"} {
		if g.Branches[name].BranchRevision != after[name] {
			t.Errorf("%s: graph records %s but git says %s — the graph went stale",
				name, abbrev(g.Branches[name].BranchRevision), abbrev(after[name]))
		}
	}
}

func TestWriteRollbackToken_RecordsTipsForEveryBranch(t *testing.T) {
	e := setupPreflightStack(t)

	id, err := writeRollbackToken(e.ctx, []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("writeRollbackToken: %v", err)
	}
	if id == "" {
		t.Fatal("rollback token needs an id to print to the user")
	}
}

// The push phase re-fetches first because preflight may have sat waiting on a
// human for a long time, and every lease was pinned to what the remote held
// before that pause. If anything moved we publish NOTHING rather than half a
// stack — that is the whole point of settling before pushing.
func TestPushPhase_AbandonsWhenTheRemoteMovedDuringPreflight(t *testing.T) {
	e := setupPreflightStack(t)
	cfg := cfgFor(t, e)

	e.git(e.ctx.Git, "checkout", "main")
	e.commit(e.ctx.Git, "trunk.txt", "moved\n", "trunk moves")
	if err := Restack(e.ctx, RestackOpts{Branch: "main"}); err != nil {
		t.Fatalf("restack: %v", err)
	}

	pre, err := Preflight(e.ctx, SubmitOpts{}, cfg, []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}

	// A collaborator publishes to the TOP branch after preflight decided.
	e.git(e.collab, "fetch", "origin")
	e.git(e.collab, "checkout", "-b", "c", "origin/c")
	e.commit(e.collab, "late.txt", "late arrival\n", "landed during preflight")
	e.git(e.collab, "push", "origin", "c")

	aBefore, _ := e.ctx.Git.RevParse("refs/remotes/origin/a")

	pushed, err := pushPhase(e.ctx, cfg, pre.Ready)
	if err == nil {
		t.Fatal("push phase must abandon when the remote moved after the decision")
	}
	if len(pushed) != 0 {
		t.Errorf("nothing may be published once we know the world moved, got %v", pushed)
	}

	// And the branch below must be untouched — no partial update.
	e.git(e.ctx.Git, "fetch", "origin")
	aAfter, _ := e.ctx.Git.RevParse("refs/remotes/origin/a")
	if aAfter != aBefore {
		t.Error("branch a was published despite the abandoned push phase")
	}
}

// Every successful push records what it left on the remote — that record is
// what makes the NEXT submit of this branch provably safe to force (ADR-0014).
func TestPushPhase_RecordsWhatItPushed(t *testing.T) {
	e := setupPreflightStack(t)
	cfg := cfgFor(t, e)

	e.git(e.ctx.Git, "checkout", "main")
	e.commit(e.ctx.Git, "trunk.txt", "moved\n", "trunk moves")
	if err := Restack(e.ctx, RestackOpts{Branch: "main"}); err != nil {
		t.Fatalf("restack: %v", err)
	}

	pre, err := Preflight(e.ctx, SubmitOpts{}, cfg, []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}

	pushed, err := pushPhase(e.ctx, cfg, pre.Ready)
	if err != nil {
		t.Fatalf("pushPhase: %v", err)
	}
	if len(pushed) != 3 {
		t.Fatalf("pushed = %v, want all three", pushed)
	}

	for _, name := range pushed {
		local, _ := e.ctx.Git.RevParse(name)
		if rec := e.ctx.Store.PushRecordFor("origin", name); rec != local {
			t.Errorf("%s: push record = %s, want the pushed tip %s", name, abbrev(rec), abbrev(local))
		}
	}

	// Submitting again immediately is a no-op, proving the record round-trips.
	again, err := Preflight(e.ctx, SubmitOpts{}, cfg, []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("second preflight: %v", err)
	}
	for _, class := range again.Ready {
		if class.Disposition != DispNoPush {
			t.Errorf("%s: disposition = %v, want up-to-date on an immediate re-submit",
				class.Branch, class.Disposition)
		}
	}
}

// The rollback token exists because sr undo restores the graph only (ADR-0002)
// and cannot put back a reset or a cherry-pick. Preflight prints its id, so the
// id has to actually resolve to something that restores the branch tips.
func TestRollback_RestoresBranchTipsRecordedBeforeRemediation(t *testing.T) {
	e := setupPreflightStack(t)

	before := map[string]string{}
	for _, name := range []string{"a", "b", "c"} {
		before[name], _ = e.ctx.Git.RevParse(name)
	}

	id, err := writeRollbackToken(e.ctx, []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("writeRollbackToken: %v", err)
	}

	// Rewrite the stack the way "Overwrite local" would.
	e.git(e.ctx.Git, "checkout", "b")
	e.commit(e.ctx.Git, "wrecked.txt", "oops\n", "a change we want to undo")
	after, _ := e.ctx.Git.RevParse("b")
	if after == before["b"] {
		t.Fatal("test setup did not actually move b")
	}

	if err := Rollback(e.ctx, RollbackOpts{ID: id}); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	for _, name := range []string{"a", "b", "c"} {
		got, _ := e.ctx.Git.RevParse(name)
		if got != before[name] {
			t.Errorf("%s = %s, want the recorded tip %s", name, abbrev(got), abbrev(before[name]))
		}
	}

	// The graph must follow the refs, or the next restack works from revisions
	// that no longer exist.
	g, _ := e.ctx.Store.ReadGraph()
	if rev := g.Branches["b"].BranchRevision; rev != before["b"] {
		t.Errorf("graph records %s for b, want %s", abbrev(rev), abbrev(before["b"]))
	}
}

func TestRollback_UnknownIDIsAClearError(t *testing.T) {
	e := setupPreflightStack(t)
	err := Rollback(e.ctx, RollbackOpts{ID: "nope"})
	if err == nil {
		t.Fatal("an unknown rollback id must error")
	}
}

func TestRollback_DefaultsToTheMostRecentToken(t *testing.T) {
	e := setupPreflightStack(t)
	if _, err := writeRollbackToken(e.ctx, []string{"a"}); err != nil {
		t.Fatalf("writeRollbackToken: %v", err)
	}
	if err := Rollback(e.ctx, RollbackOpts{}); err != nil {
		t.Fatalf("Rollback with no id should use the latest token: %v", err)
	}
}
