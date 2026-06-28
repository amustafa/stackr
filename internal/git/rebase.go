package git

// Rebase rebases the current branch onto target.
func (r *Runner) Rebase(onto string) error {
	args := []string{"rebase", onto}
	if !r.Verify {
		args = append(args, "--no-verify")
	}
	return r.RunGit(args...)
}

// RebaseOnto performs `git rebase --onto newBase oldBase branch`.
func (r *Runner) RebaseOnto(newBase, oldBase, branch string) error {
	args := []string{"rebase", "--onto", newBase, oldBase, branch}
	if !r.Verify {
		args = append(args, "--no-verify")
	}
	return r.RunGit(args...)
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
