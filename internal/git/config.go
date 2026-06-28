package git

import "strings"

// GetConfig reads a git config value. Returns empty string if not set.
func (r *Runner) GetConfig(key string) (string, error) {
	val, _, err := r.RunGitCaptureAll("config", "--get", key)
	if err != nil {
		// Exit code 1 means key not found — not an error.
		return "", nil
	}
	return val, nil
}

// SetConfig writes a git config value (local scope).
func (r *Runner) SetConfig(key, value string) error {
	return r.RunGit("config", "--local", key, value)
}

// DefaultBranch tries to detect the default branch from remote HEAD or common names.
func (r *Runner) DefaultBranch() (string, error) {
	// Try the remote HEAD reference first.
	ref, err := r.RunGitCapture("symbolic-ref", "refs/remotes/origin/HEAD")
	if err == nil && ref != "" {
		// "refs/remotes/origin/main" → "main"
		parts := strings.Split(ref, "/")
		if len(parts) > 0 {
			return parts[len(parts)-1], nil
		}
	}

	// Fall back to checking common branch names.
	for _, name := range []string{"main", "master"} {
		exists, err := r.BranchExists(name)
		if err != nil {
			return "", err
		}
		if exists {
			return name, nil
		}
	}

	return "", nil
}
