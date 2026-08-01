package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/git"
)

// divEnv is a local clone wired to a bare remote, plus a second clone standing
// in for a collaborator. Everything here runs against real git.
type divEnv struct {
	t      *testing.T
	ctx    *context.Context
	collab *git.Runner
}

func newDivEnv(t *testing.T) *divEnv {
	t.Helper()
	ctx, remoteDir := setupGetTestEnv(t)

	collabDir := t.TempDir()
	collab := &git.Runner{Dir: collabDir}
	if _, err := collab.RunGitCapture("clone", remoteDir, "."); err != nil {
		t.Fatalf("collaborator clone: %v", err)
	}
	collab.RunGitCapture("config", "user.email", "collab@test.com")
	collab.RunGitCapture("config", "user.name", "Collab")

	return &divEnv{t: t, ctx: ctx, collab: collab}
}

func (e *divEnv) write(r *git.Runner, name, content string) {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(r.Dir, name), []byte(content), 0o644); err != nil {
		e.t.Fatalf("write %s: %v", name, err)
	}
}

func (e *divEnv) commit(r *git.Runner, name, content, msg string) string {
	e.t.Helper()
	e.write(r, name, content)
	if _, err := r.RunGitCapture("add", name); err != nil {
		e.t.Fatalf("add %s: %v", name, err)
	}
	if err := r.RunGit("commit", "-m", msg); err != nil {
		e.t.Fatalf("commit %q: %v", msg, err)
	}
	sha, _ := r.RevParse("HEAD")
	return sha
}

func (e *divEnv) git(r *git.Runner, args ...string) {
	e.t.Helper()
	if err := r.RunGit(args...); err != nil {
		e.t.Fatalf("git %v: %v", args, err)
	}
}

// pushAndRecord publishes a branch the way Submit would, including the Push
// Record. Tests that omit the record are simulating a fresh clone.
func (e *divEnv) pushAndRecord(branch string) {
	e.t.Helper()
	e.git(e.ctx.Git, "push", "-u", "origin", branch)
	e.git(e.ctx.Git, "fetch", "origin")
	sha, _ := e.ctx.Git.RevParse("refs/remotes/origin/" + branch)
	if err := e.ctx.Store.SetPushRecord("origin", branch, sha); err != nil {
		e.t.Fatalf("SetPushRecord: %v", err)
	}
}

func (e *divEnv) classify(branch string) Classification {
	e.t.Helper()
	e.git(e.ctx.Git, "fetch", "origin")
	res, err := ClassifyBranch(e.ctx, "origin", branch)
	if err != nil {
		e.t.Fatalf("ClassifyBranch(%s): %v", branch, err)
	}
	return res
}

func (e *divEnv) wantDisposition(got Classification, want Disposition) {
	e.t.Helper()
	if got.Disposition != want {
		e.t.Fatalf("disposition = %v (%s), want %v", got.Disposition, got.Reason, want)
	}
}

// --- Cases that MUST classify as lossless: stackr's own local rewrites -------

func TestClassify_FirstPushHasNoRemote(t *testing.T) {
	e := newDivEnv(t)
	e.git(e.ctx.Git, "checkout", "-b", "feat")
	e.commit(e.ctx.Git, "f.txt", "work\n", "work")

	got := e.classify("feat")
	e.wantDisposition(got, DispPushPlain)
	if got.RemoteSHA != "" {
		t.Errorf("RemoteSHA = %q, want empty so the lease asserts absence", got.RemoteSHA)
	}
}

func TestClassify_UpToDate(t *testing.T) {
	e := newDivEnv(t)
	e.git(e.ctx.Git, "checkout", "-b", "feat")
	e.commit(e.ctx.Git, "f.txt", "work\n", "work")
	e.pushAndRecord("feat")

	e.wantDisposition(e.classify("feat"), DispNoPush)
}

func TestClassify_RebaseOntoNewTrunkIsLossless(t *testing.T) {
	e := newDivEnv(t)
	e.git(e.ctx.Git, "checkout", "-b", "feat")
	e.commit(e.ctx.Git, "f1.txt", "one\n", "c1")
	e.commit(e.ctx.Git, "f2.txt", "two\n", "c2")
	e.pushAndRecord("feat")

	// Trunk moves; the branch is restacked onto it and gains a commit.
	e.git(e.ctx.Git, "checkout", "main")
	e.commit(e.ctx.Git, "trunk.txt", "moved\n", "trunk moves")
	e.git(e.ctx.Git, "checkout", "feat")
	e.git(e.ctx.Git, "rebase", "main")
	e.commit(e.ctx.Git, "f3.txt", "three\n", "c3")

	got := e.classify("feat")
	e.wantDisposition(got, DispPushForce)
	if got.RemoteSHA == "" {
		t.Error("a force push needs a lease pin")
	}
}

// The Push Record makes this free; the point of the test is that the content
// tiers agree, so a fresh clone reaches the same answer.
func TestClassify_SquashIsLosslessWithoutPushRecord(t *testing.T) {
	e := newDivEnv(t)
	e.git(e.ctx.Git, "checkout", "-b", "feat")
	e.commit(e.ctx.Git, "f1.txt", "one\n", "c1")
	e.commit(e.ctx.Git, "f2.txt", "two\n", "c2")
	e.commit(e.ctx.Git, "f3.txt", "three\n", "c3")
	e.git(e.ctx.Git, "push", "-u", "origin", "feat") // no Push Record: fresh-clone simulation

	// Collapse the three commits into one.
	e.git(e.ctx.Git, "reset", "--soft", "main")
	e.git(e.ctx.Git, "commit", "-m", "squashed")

	e.wantDisposition(e.classify("feat"), DispPushForce)
}

func TestClassify_AbsorbIsLosslessWithoutPushRecord(t *testing.T) {
	e := newDivEnv(t)
	e.git(e.ctx.Git, "checkout", "-b", "feat")
	e.commit(e.ctx.Git, "f1.txt", "one\n", "c1")
	e.commit(e.ctx.Git, "f2.txt", "two\n", "c2")
	e.git(e.ctx.Git, "push", "-u", "origin", "feat")

	// Absorb a change into an earlier commit, touching a region that commit did
	// not itself create. Patch-id equivalence breaks here; content containment
	// does not.
	e.write(e.ctx.Git, "f3.txt", "absorbed\n")
	e.git(e.ctx.Git, "add", "f3.txt")
	e.git(e.ctx.Git, "commit", "--amend", "--no-edit")

	e.wantDisposition(e.classify("feat"), DispPushForce)
}

// A known and unavoidable false positive: amending the very hunk the remote
// commit introduced looks identical to a collaborator amending it. Both sides
// hold the same file at different content with no common version in the base,
// so the three-way merge is an add/add conflict.
//
// No content-only predicate can separate these two — which is precisely why the
// Push Record is primary rather than a shortcut. The second half of this test
// shows tier 0 clearing it.
func TestClassify_SameRegionAmendIsAKnownFalsePositiveCuredByTier0(t *testing.T) {
	e := newDivEnv(t)
	e.git(e.ctx.Git, "checkout", "-b", "feat")
	e.commit(e.ctx.Git, "f1.txt", "one\n", "c1")
	e.commit(e.ctx.Git, "f2.txt", "two\n", "c2")
	e.git(e.ctx.Git, "push", "-u", "origin", "feat") // no Push Record

	e.write(e.ctx.Git, "f2.txt", "two\nabsorbed\n")
	e.git(e.ctx.Git, "add", "f2.txt")
	e.git(e.ctx.Git, "commit", "--amend", "--no-edit")

	withoutRecord := e.classify("feat")
	e.wantDisposition(withoutRecord, DispNeedsDecision)

	// Now record what we actually left on the remote, as a real submit would.
	remoteSHA, _ := e.ctx.Git.RevParse("refs/remotes/origin/feat")
	if err := e.ctx.Store.SetPushRecord("origin", "feat", remoteSHA); err != nil {
		t.Fatal(err)
	}
	e.wantDisposition(e.classify("feat"), DispPushForce)
}

func TestClassify_SplitIsLosslessWithoutPushRecord(t *testing.T) {
	e := newDivEnv(t)
	e.git(e.ctx.Git, "checkout", "-b", "feat")
	e.write(e.ctx.Git, "a.txt", "a\n")
	e.write(e.ctx.Git, "b.txt", "b\n")
	e.git(e.ctx.Git, "add", "a.txt", "b.txt")
	e.git(e.ctx.Git, "commit", "-m", "both at once")
	e.git(e.ctx.Git, "push", "-u", "origin", "feat")

	// Split that one commit into two, same end state.
	e.git(e.ctx.Git, "reset", "--hard", "main")
	e.commit(e.ctx.Git, "a.txt", "a\n", "just a")
	e.commit(e.ctx.Git, "b.txt", "b\n", "just b")

	e.wantDisposition(e.classify("feat"), DispPushForce)
}

// The case that defeats patch-id outright: trunk edits a line *adjacent* to a
// branch hunk, so the rebased commit's patch-id changes even though nothing
// about the branch's own work did.
func TestClassify_RestackOverAdjacentTrunkEditIsLossless(t *testing.T) {
	e := newDivEnv(t)
	e.commit(e.ctx.Git, "shared.txt", "line1\nline2\nline3\nline4\nline5\n", "seed")
	e.git(e.ctx.Git, "push", "origin", "main")

	e.git(e.ctx.Git, "checkout", "-b", "feat")
	e.commit(e.ctx.Git, "shared.txt", "line1\nline2\nline3\nline4\nBRANCH\n", "branch edits the tail")
	e.git(e.ctx.Git, "push", "-u", "origin", "feat")

	// Trunk edits line1 — different hunk, but it lands in the branch commit's
	// diff context.
	e.git(e.ctx.Git, "checkout", "main")
	e.commit(e.ctx.Git, "shared.txt", "TRUNK\nline2\nline3\nline4\nline5\n", "trunk edits the head")
	e.git(e.ctx.Git, "checkout", "feat")
	e.git(e.ctx.Git, "rebase", "main")

	e.wantDisposition(e.classify("feat"), DispPushForce)
}

// --- Cases that MUST stop -----------------------------------------------------

func TestClassify_CollaboratorCommitStops(t *testing.T) {
	e := newDivEnv(t)
	e.git(e.ctx.Git, "checkout", "-b", "feat")
	e.commit(e.ctx.Git, "f1.txt", "one\n", "c1")
	e.pushAndRecord("feat")

	// Collaborator adds work we have never seen.
	e.git(e.collab, "fetch", "origin")
	e.git(e.collab, "checkout", "-b", "feat", "origin/feat")
	e.commit(e.collab, "theirs.txt", "their work\n", "collaborator commit")
	e.git(e.collab, "push", "origin", "feat")

	// Meanwhile we restack locally.
	e.commit(e.ctx.Git, "f2.txt", "two\n", "c2")

	got := e.classify("feat")
	e.wantDisposition(got, DispNeedsDecision)
	if len(got.RemoteOnly) == 0 {
		t.Error("RemoteOnly should name the collaborator's commit so remediation can replay it")
	}
}

// A revert cancels against the commit it undoes, so whole-branch containment
// (tier 1) reports "safe". Tier 2 replays commits individually and catches it.
func TestClassify_CollaboratorRevertStops(t *testing.T) {
	e := newDivEnv(t)
	e.git(e.ctx.Git, "checkout", "-b", "feat")
	e.commit(e.ctx.Git, "f1.txt", "one\n", "c1")
	e.commit(e.ctx.Git, "danger.txt", "risky\n", "c2 risky")
	e.git(e.ctx.Git, "push", "-u", "origin", "feat")

	// Collaborator reverts the risky commit.
	e.git(e.collab, "fetch", "origin")
	e.git(e.collab, "checkout", "-b", "feat", "origin/feat")
	e.git(e.collab, "revert", "--no-edit", "HEAD")
	e.git(e.collab, "push", "origin", "feat")

	// We restack, still holding the reverted work.
	e.git(e.ctx.Git, "checkout", "main")
	e.commit(e.ctx.Git, "trunk.txt", "moved\n", "trunk moves")
	e.git(e.ctx.Git, "checkout", "feat")
	e.git(e.ctx.Git, "rebase", "main")

	got := e.classify("feat")
	e.wantDisposition(got, DispNeedsDecision)
}

// The sharpest case, and the reason the Push Record is primary. The
// collaborator drops a commit and force-pushes, so the remote becomes a strict
// ANCESTOR of local: every content test says "safe" and git would not even
// require a force. Only "the remote moved off what we left there" catches it.
func TestClassify_CollaboratorDroppedCommitStops(t *testing.T) {
	e := newDivEnv(t)
	e.git(e.ctx.Git, "checkout", "-b", "feat")
	e.commit(e.ctx.Git, "f1.txt", "one\n", "c1")
	e.commit(e.ctx.Git, "f2.txt", "two\n", "c2")
	e.pushAndRecord("feat")

	// Collaborator drops c2 and force-pushes.
	e.git(e.collab, "fetch", "origin")
	e.git(e.collab, "checkout", "-b", "feat", "origin/feat")
	e.git(e.collab, "reset", "--hard", "HEAD~1")
	e.git(e.collab, "push", "--force", "origin", "feat")

	got := e.classify("feat")
	e.wantDisposition(got, DispNeedsDecision)

	// Prove the trap is real: without the record, this looks like an ordinary
	// fast-forward and c2 would be silently resurrected.
	if err := e.ctx.Store.DeletePushRecordsForBranch("feat"); err != nil {
		t.Fatal(err)
	}
	withoutRecord := e.classify("feat")
	if withoutRecord.Disposition != DispPushPlain {
		t.Fatalf("expected the content tiers to be fooled (DispPushPlain), got %v — "+
			"if this now stops, the ladder changed and tier 0's primacy should be re-argued",
			withoutRecord.Disposition)
	}
}

func TestClassify_LocalBehindRemoteStops(t *testing.T) {
	e := newDivEnv(t)
	e.git(e.ctx.Git, "checkout", "-b", "feat")
	e.commit(e.ctx.Git, "f1.txt", "one\n", "c1")
	e.pushAndRecord("feat")

	e.git(e.collab, "fetch", "origin")
	e.git(e.collab, "checkout", "-b", "feat", "origin/feat")
	e.commit(e.collab, "theirs.txt", "ahead\n", "they moved ahead")
	e.git(e.collab, "push", "origin", "feat")

	got := e.classify("feat")
	e.wantDisposition(got, DispNeedsDecision)
}

func TestClassify_UnrelatedHistoriesStops(t *testing.T) {
	e := newDivEnv(t)
	e.git(e.ctx.Git, "checkout", "-b", "feat")
	e.commit(e.ctx.Git, "f1.txt", "one\n", "c1")
	e.git(e.ctx.Git, "push", "-u", "origin", "feat")

	// Replace the branch with a history that shares no ancestry at all.
	e.git(e.ctx.Git, "checkout", "--orphan", "orphan-tmp")
	if _, err := e.ctx.Git.RunGitCapture("rm", "-rf", "."); err != nil {
		t.Fatalf("rm: %v", err)
	}
	e.commit(e.ctx.Git, "z.txt", "alone\n", "orphan root")
	e.git(e.ctx.Git, "branch", "-M", "feat")

	got := e.classify("feat")
	e.wantDisposition(got, DispNeedsDecision)
}

// Tier 0 must win even when the content tiers would have to work hard: a
// restacked branch whose remote we ourselves last wrote is force-pushable with
// no content analysis at all.
func TestClassify_PushRecordShortCircuitsContentTiers(t *testing.T) {
	e := newDivEnv(t)
	e.git(e.ctx.Git, "checkout", "-b", "feat")
	e.commit(e.ctx.Git, "f1.txt", "one\n", "c1")
	e.pushAndRecord("feat")

	remoteSHA, _ := e.ctx.Git.RevParse("refs/remotes/origin/feat")

	e.git(e.ctx.Git, "checkout", "main")
	e.commit(e.ctx.Git, "trunk.txt", "moved\n", "trunk moves")
	e.git(e.ctx.Git, "checkout", "feat")
	e.git(e.ctx.Git, "rebase", "main")

	got := e.classify("feat")
	e.wantDisposition(got, DispPushForce)
	if got.RemoteSHA != remoteSHA {
		t.Errorf("lease pin = %s, want the SHA we inspected %s", got.RemoteSHA, remoteSHA)
	}
}

// A Push Record mismatch says somebody else wrote to this branch, and the
// content tiers must NOT be able to clear it.
//
// The trap: a collaborator force-pushes a branch with a commit REMOVED, and we
// have independently restacked. Their deletion now sits outside the merge base
// entirely, so merging them in is a no-op and every content test reports
// "nothing to lose" — while force-pushing would resurrect the commit they
// deliberately dropped.
func TestClassify_PushRecordMismatchIsNotClearableByContentTiers(t *testing.T) {
	e := newDivEnv(t)
	e.git(e.ctx.Git, "checkout", "-b", "feat")
	e.commit(e.ctx.Git, "f1.txt", "one\n", "c1")
	e.commit(e.ctx.Git, "risky.txt", "risky\n", "c2 risky")
	e.pushAndRecord("feat")

	// Collaborator drops the risky commit and force-pushes.
	e.git(e.collab, "fetch", "origin")
	e.git(e.collab, "checkout", "-b", "feat", "origin/feat")
	e.git(e.collab, "reset", "--hard", "HEAD~1")
	e.git(e.collab, "push", "--force", "origin", "feat")

	// We restack over a moved trunk, so the refs genuinely diverge and their
	// deletion falls outside the merge base.
	e.git(e.ctx.Git, "checkout", "main")
	e.commit(e.ctx.Git, "trunk.txt", "moved\n", "trunk moves")
	e.git(e.ctx.Git, "checkout", "feat")
	e.git(e.ctx.Git, "rebase", "main")

	got := e.classify("feat")
	e.wantDisposition(got, DispNeedsDecision)
	if !strings.Contains(got.Reason, "somebody else pushed") {
		t.Errorf("reason = %q, want it to name the push-record mismatch", got.Reason)
	}
}
