package git

import "strings"

// IsDirty returns true if the working tree has uncommitted changes.
func (r *Runner) IsDirty() (bool, error) {
	out, err := r.RunGitCapture("status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// HasStagedChanges returns true if there are staged changes.
func (r *Runner) HasStagedChanges() (bool, error) {
	_, _, err := r.RunGitCaptureAll("diff", "--cached", "--quiet")
	if err != nil {
		return true, nil // non-zero exit = there are diffs
	}
	return false, nil
}

// HasUntrackedFiles returns true if there are untracked files.
func (r *Runner) HasUntrackedFiles() (bool, error) {
	out, err := r.RunGitCapture("ls-files", "--others", "--exclude-standard")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}
