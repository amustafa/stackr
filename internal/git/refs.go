package git

// RevParse resolves a revision to a full SHA.
func (r *Runner) RevParse(rev string) (string, error) {
	return r.RunGitCapture("rev-parse", rev)
}

// RevParseShort resolves a revision to a short SHA.
func (r *Runner) RevParseShort(rev string) (string, error) {
	return r.RunGitCapture("rev-parse", "--short", rev)
}

// MergeBase returns the best common ancestor of two commits.
func (r *Runner) MergeBase(a, b string) (string, error) {
	return r.RunGitCapture("merge-base", a, b)
}

// IsAncestor returns true if ancestor is an ancestor of descendant.
func (r *Runner) IsAncestor(ancestor, descendant string) (bool, error) {
	_, _, err := r.RunGitCaptureAll("merge-base", "--is-ancestor", ancestor, descendant)
	if err != nil {
		return false, nil
	}
	return true, nil
}

// RepoRoot returns the absolute path of the repository root.
func (r *Runner) RepoRoot() (string, error) {
	return r.RunGitCapture("rev-parse", "--show-toplevel")
}

// GitDir returns the path to the .git directory.
// In a worktree this returns the worktree-specific dir (e.g. .git/worktrees/name).
func (r *Runner) GitDir() (string, error) {
	return r.RunGitCapture("rev-parse", "--git-dir")
}

// GitCommonDir returns the shared .git directory.
// Unlike GitDir, this always returns the main .git dir even from a worktree.
func (r *Runner) GitCommonDir() (string, error) {
	return r.RunGitCapture("rev-parse", "--git-common-dir")
}
