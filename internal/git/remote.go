package git

import "strings"

// Push pushes a branch to the remote.
func (r *Runner) Push(remote, branch string, force bool) error {
	args := []string{"push", remote, branch}
	if force {
		args = []string{"push", "--force-with-lease", remote, branch}
	}
	return r.RunGit(args...)
}

// PushWithUpstream pushes and sets upstream tracking.
func (r *Runner) PushWithUpstream(remote, branch string, force bool) error {
	args := []string{"push", "-u", remote, branch}
	if force {
		args = []string{"push", "--force-with-lease", "-u", remote, branch}
	}
	return r.RunGit(args...)
}

// Fetch fetches from the remote.
func (r *Runner) Fetch(remote string) error {
	return r.RunGit("fetch", remote)
}

// FetchPrune fetches and prunes deleted remote branches.
func (r *Runner) FetchPrune(remote string) error {
	return r.RunGit("fetch", "--prune", remote)
}

// RemoteBranchExists checks if a branch exists on the remote.
func (r *Runner) RemoteBranchExists(remote, branch string) (bool, error) {
	_, err := r.RunGitCapture("rev-parse", "--verify", "refs/remotes/"+remote+"/"+branch)
	return err == nil, nil
}

// ListRemotes returns the list of configured remotes.
func (r *Runner) ListRemotes() ([]string, error) {
	out, err := r.RunGitCapture("remote")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// IsMergedInto checks if branch is merged into target.
func (r *Runner) IsMergedInto(branch, target string) (bool, error) {
	return r.IsAncestor(branch, target)
}
