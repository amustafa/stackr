package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newRepoWithRemote returns a working repo wired to a bare remote, plus a second
// clone standing in for a collaborator.
func newRepoWithRemote(t *testing.T) (local, collab *Runner, remoteDir string) {
	t.Helper()

	remoteDir = filepath.Join(t.TempDir(), "remote.git")
	bare := &Runner{Dir: t.TempDir()}
	if _, err := bare.RunGitCapture("init", "--bare", remoteDir); err != nil {
		t.Fatalf("init bare: %v", err)
	}

	local = tempRunner(t)
	if err := local.AddRemote("origin", remoteDir); err != nil {
		t.Fatalf("add remote: %v", err)
	}
	commitFile(t, local, "a.txt", "one\n", "c1")
	branch, _ := local.CurrentBranch()
	if err := local.RunGit("push", "-u", "origin", branch); err != nil {
		t.Fatalf("seed push: %v", err)
	}

	collabDir := t.TempDir()
	c := &Runner{Dir: collabDir}
	if _, err := c.RunGitCapture("clone", remoteDir, collabDir); err != nil {
		t.Fatalf("clone: %v", err)
	}
	c.RunGitCapture("config", "user.email", "collab@test.com")
	c.RunGitCapture("config", "user.name", "Collab")

	return local, c, remoteDir
}

func TestPushPinned_SucceedsWhenRemoteMatchesInspectedSHA(t *testing.T) {
	local, _, _ := newRepoWithRemote(t)
	branch, _ := local.CurrentBranch()

	inspected, err := local.RevParse("refs/remotes/origin/" + branch)
	if err != nil {
		t.Fatalf("rev-parse remote: %v", err)
	}

	commitFile(t, local, "b.txt", "two\n", "c2")

	if err := local.PushPinned("origin", branch, inspected, true); err != nil {
		t.Fatalf("PushPinned should succeed when the remote still holds the inspected SHA: %v", err)
	}
}

// This is the case an unqualified --force-with-lease gets wrong. A collaborator
// pushes, we fetch (which updates the remote-tracking ref to their commit), and
// the bare lease then "expects" their commit and passes — destroying it. Pinning
// to the SHA we actually inspected must reject instead.
func TestPushPinned_RejectsWhenRemoteMovedAfterInspection(t *testing.T) {
	local, collab, _ := newRepoWithRemote(t)
	branch, _ := local.CurrentBranch()

	inspected, err := local.RevParse("refs/remotes/origin/" + branch)
	if err != nil {
		t.Fatalf("rev-parse remote: %v", err)
	}

	// Collaborator publishes work we have never seen.
	commitFile(t, collab, "theirs.txt", "their work\n", "collaborator commit")
	if err := collab.RunGit("push", "origin", branch); err != nil {
		t.Fatalf("collaborator push: %v", err)
	}

	// We fetch — this is what makes the unqualified lease vacuous.
	if err := local.Fetch("origin"); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	commitFile(t, local, "ours.txt", "our work\n", "our commit")

	err = local.PushPinned("origin", branch, inspected, true)
	if err == nil {
		t.Fatal("PushPinned must reject: the remote moved off the inspected SHA")
	}
	if !IsStaleLease(err) {
		t.Fatalf("expected a StaleLeaseError, got %T: %v", err, err)
	}

	// And the collaborator's commit must still be on the remote.
	remoteSHA, _ := local.RevParse("refs/remotes/origin/" + branch)
	if remoteSHA == "" {
		t.Fatal("remote ref vanished")
	}
	out, err := local.RunGitCapture("log", "--format=%s", "-n", "5", "refs/remotes/origin/"+branch)
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if !strings.Contains(out, "collaborator commit") {
		t.Fatalf("collaborator's commit was destroyed; remote log:\n%s", out)
	}
}

func TestPushPinned_EmptyExpectCreatesBranchAndRejectsOnceItExists(t *testing.T) {
	local, _, _ := newRepoWithRemote(t)

	if err := local.RunGit("checkout", "-b", "brand-new"); err != nil {
		t.Fatal(err)
	}
	commitFile(t, local, "new.txt", "new\n", "new work")

	// Empty expect asserts the ref does not exist yet.
	if err := local.PushPinned("origin", "brand-new", "", true); err != nil {
		t.Fatalf("first push with empty-expect lease should succeed: %v", err)
	}

	commitFile(t, local, "new2.txt", "more\n", "more work")
	err := local.PushPinned("origin", "brand-new", "", true)
	if err == nil {
		t.Fatal("empty-expect lease must reject once the ref exists — this is the create race")
	}
	if !IsStaleLease(err) {
		t.Fatalf("expected StaleLeaseError, got %T: %v", err, err)
	}
}

func TestPushPinned_VerifyControlsPrePushHook(t *testing.T) {
	local, _, _ := newRepoWithRemote(t)
	branch, _ := local.CurrentBranch()

	hookDir, err := local.RunGitCapture("rev-parse", "--git-path", "hooks")
	if err != nil {
		t.Fatalf("locate hooks dir: %v", err)
	}
	if !filepath.IsAbs(hookDir) {
		hookDir = filepath.Join(local.Dir, hookDir)
	}
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(hookDir, "pre-push")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	inspected, _ := local.RevParse("refs/remotes/origin/" + branch)
	commitFile(t, local, "b.txt", "two\n", "c2")

	local.Verify = true
	if err := local.PushPinned("origin", branch, inspected, true); err == nil {
		t.Fatal("push must fail while the pre-push hook rejects and Verify is on")
	}

	local.Verify = false
	if err := local.PushPinned("origin", branch, inspected, true); err != nil {
		t.Fatalf("Verify=false must pass --no-verify and skip the hook: %v", err)
	}
}

func TestPushPinned_ForcePushOverOurOwnRewriteSucceeds(t *testing.T) {
	local, _, _ := newRepoWithRemote(t)
	branch, _ := local.CurrentBranch()

	commitFile(t, local, "b.txt", "two\n", "c2")
	if err := local.RunGit("push", "origin", branch); err != nil {
		t.Fatalf("push: %v", err)
	}
	inspected, _ := local.RevParse("refs/remotes/origin/" + branch)

	// Rewrite history the way a restack would: same content, new SHA.
	if err := local.RunGit("commit", "--amend", "-m", "c2 amended"); err != nil {
		t.Fatalf("amend: %v", err)
	}

	if err := local.PushPinned("origin", branch, inspected, false); err != nil {
		t.Fatalf("force push over our own rewrite should succeed: %v", err)
	}
}
