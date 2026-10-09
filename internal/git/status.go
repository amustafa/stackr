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

// HasUncommittedChanges reports whether tracked files carry staged or unstaged
// changes — the condition under which git refuses to start a rebase. Untracked
// files don't count: git rebases over them. IsDirty is the broader test.
func (r *Runner) HasUncommittedChanges() (bool, error) {
	out, err := r.RunGitCapture("status", "--porcelain", "--untracked-files=no")
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
