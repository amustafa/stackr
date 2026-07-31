package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newRefsRepo(t *testing.T) (r *Runner, trunk string) {
	t.Helper()
	dir := t.TempDir()
	r = &Runner{Dir: dir}
	r.RunGitCapture("init")
	r.RunGitCapture("config", "user.email", "test@test.com")
	r.RunGitCapture("config", "user.name", "Test")
	r.RunGitCapture("commit", "--allow-empty", "-m", "initial")
	trunk, err := r.CurrentBranch()
	if err != nil {
		t.Fatalf("current branch: %v", err)
	}
	return r, trunk
}

func writeFile(t *testing.T, r *Runner, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.Dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestSquashMergedUpstream_MultiCommitSquash(t *testing.T) {
	r, trunk := newRefsRepo(t)
	base, err := r.RevParse("HEAD")
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}

	r.RunGitCapture("checkout", "-b", "feature")
	for i, line := range []string{"a1", "a2", "a3"} {
		writeFile(t, r, "x", line+"\n")
		r.RunGitCapture("add", "x")
		r.RunGitCapture("commit", "-m", "commit "+string(rune('1'+i)))
	}
	branch, err := r.RevParse("feature")
	if err != nil {
		t.Fatalf("rev-parse feature: %v", err)
	}

	// Simulate a GitHub squash merge: apply the branch's combined diff as one
	// commit on trunk, never touching the branch itself.
	r.RunGitCapture("checkout", trunk)
	diff, _, err := r.RunGitCaptureAll("diff", base, branch)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	applyCmd := r.command("apply")
	// RunGitCaptureAll trims the trailing newline git apply requires at EOF.
	applyCmd.Stdin = strings.NewReader(diff + "\n")
	if out, err := applyCmd.CombinedOutput(); err != nil {
		t.Fatalf("apply: %v: %s", err, out)
	}
	r.RunGitCapture("add", "x")
	r.RunGitCapture("commit", "-m", "squash of feature")

	// AllCommitsUpstream compares patch IDs one commit at a time, so it must
	// fail to see through the squash merge — this pins the gap the new check
	// covers, not just the fix itself.
	if landed, err := r.AllCommitsUpstream(trunk, "feature", base); err != nil {
		t.Fatalf("AllCommitsUpstream: %v", err)
	} else if landed {
		t.Fatal("AllCommitsUpstream unexpectedly detected the squash merge; test no longer exercises the gap it's meant to")
	}

	landed, err := r.SquashMergedUpstream(trunk, "feature", base)
	if err != nil {
		t.Fatalf("SquashMergedUpstream: %v", err)
	}
	if !landed {
		t.Fatal("SquashMergedUpstream did not detect a squash merge of an equivalent multi-commit branch")
	}
}

func TestSquashMergedUpstream_NotLanded(t *testing.T) {
	r, trunk := newRefsRepo(t)
	base, err := r.RevParse("HEAD")
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}

	r.RunGitCapture("checkout", "-b", "feature")
	writeFile(t, r, "x", "unmerged\n")
	r.RunGitCapture("add", "x")
	r.RunGitCapture("commit", "-m", "still open")

	// trunk moves on its own, unrelated work — nothing matches the branch.
	r.RunGitCapture("checkout", trunk)
	writeFile(t, r, "y", "trunk work\n")
	r.RunGitCapture("add", "y")
	r.RunGitCapture("commit", "-m", "unrelated trunk commit")

	landed, err := r.SquashMergedUpstream(trunk, "feature", base)
	if err != nil {
		t.Fatalf("SquashMergedUpstream: %v", err)
	}
	if landed {
		t.Fatal("SquashMergedUpstream reported landed for a branch that never merged")
	}
}
