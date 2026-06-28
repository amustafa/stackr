package git

import "strings"

// WorktreeEntry represents a single git worktree.
type WorktreeEntry struct {
	Path   string
	Branch string
}

// WorktreeAdd creates a new worktree at path for the given branch.
func (r *Runner) WorktreeAdd(path, branch string) error {
	return r.RunGit("worktree", "add", path, branch)
}

// WorktreeRemove removes the worktree at path.
func (r *Runner) WorktreeRemove(path string) error {
	return r.RunGit("worktree", "remove", path)
}

// WorktreeList returns all worktrees by parsing `git worktree list --porcelain`.
func (r *Runner) WorktreeList() ([]WorktreeEntry, error) {
	out, err := r.RunGitCapture("worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	var entries []WorktreeEntry
	var current WorktreeEntry
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			current = WorktreeEntry{Path: strings.TrimPrefix(line, "worktree ")}
		case strings.HasPrefix(line, "branch refs/heads/"):
			current.Branch = strings.TrimPrefix(line, "branch refs/heads/")
		case line == "":
			if current.Path != "" {
				entries = append(entries, current)
			}
			current = WorktreeEntry{}
		}
	}
	// Last entry (no trailing blank line).
	if current.Path != "" {
		entries = append(entries, current)
	}
	return entries, nil
}

// WorktreeForBranch returns the worktree path for a branch, or "" if the
// branch is not checked out in any worktree.
func (r *Runner) WorktreeForBranch(branch string) (string, error) {
	entries, err := r.WorktreeList()
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.Branch == branch {
			return e.Path, nil
		}
	}
	return "", nil
}

// StashPush stashes all changes (staged + unstaged + untracked).
func (r *Runner) StashPush(message string) error {
	args := []string{"stash", "push", "--include-untracked"}
	if message != "" {
		args = append(args, "-m", message)
	}
	return r.RunGit(args...)
}

// StashPop pops the most recent stash entry.
func (r *Runner) StashPop() error {
	return r.RunGit("stash", "pop")
}
