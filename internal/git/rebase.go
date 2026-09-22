package git

import (
	"strings"

	srerr "github.com/amustafa/stackr/internal/errors"
)

// Rebase rebases the current branch onto target.
func (r *Runner) Rebase(onto string) error {
	args := []string{"rebase", onto}
	if r.NoVerify {
		args = append(args, "--no-verify")
	}
	return r.RunGit(args...)
}

// RebaseOntoCaptured performs `git rebase --onto newBase oldBase branch` with
// git's output captured instead of forwarded, returned alongside the error.
// The caller announces the move in its own words, so on success git's
// "Rebasing (1/1)" and "Successfully rebased" would only say it again. On a
// conflict the output holds the CONFLICT lines the user needs, but also a
// block of hints about `git rebase --continue` that are wrong for a rebase
// stackr is about to abort or resume itself — the caller decides what to show.
func (r *Runner) RebaseOntoCaptured(newBase, oldBase, branch string) (string, error) {
	args := []string{"rebase", "--quiet", "--onto", newBase, oldBase, branch}
	if r.NoVerify {
		args = append(args, "--no-verify")
	}
	stdout, stderr, err := r.RunGitCaptureAll(args...)
	out := strings.TrimSpace(stdout + "\n" + stderr)
	if err != nil {
		return out, &srerr.GitError{Args: args, Stderr: stderr, Err: err}
	}
	return out, nil
}

// RebaseContinue continues a rebase after conflict resolution.
func (r *Runner) RebaseContinue() error {
	return r.RunGit("rebase", "--continue")
}

// RebaseAbort aborts an in-progress rebase.
func (r *Runner) RebaseAbort() error {
	return r.RunGit("rebase", "--abort")
}

// IsRebaseInProgress checks for REBASE_HEAD ref which exists during rebase.
func (r *Runner) IsRebaseInProgress() bool {
	_, err := r.RunGitCapture("rev-parse", "--verify", "REBASE_HEAD")
	return err == nil
}
