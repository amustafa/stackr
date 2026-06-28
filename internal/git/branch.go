package git

import "strings"

// CurrentBranch returns the name of the current branch (HEAD).
func (r *Runner) CurrentBranch() (string, error) {
	return r.RunGitCapture("symbolic-ref", "--short", "HEAD")
}

// BranchExists checks if a branch exists locally.
func (r *Runner) BranchExists(name string) (bool, error) {
	_, err := r.RunGitCapture("rev-parse", "--verify", "refs/heads/"+name)
	if err != nil {
		return false, nil
	}
	return true, nil
}

// CreateBranch creates a new branch at the given start point.
// If startPoint is empty, branches from HEAD.
func (r *Runner) CreateBranch(name, startPoint string) error {
	args := []string{"branch", name}
	if startPoint != "" {
		args = append(args, startPoint)
	}
	return r.RunGit(args...)
}

// DeleteBranch deletes a local branch. If force is true, uses -D instead of -d.
func (r *Runner) DeleteBranch(name string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	return r.RunGit("branch", flag, name)
}

// RenameBranch renames a branch.
func (r *Runner) RenameBranch(oldName, newName string) error {
	return r.RunGit("branch", "-m", oldName, newName)
}

// ListBranches returns all local branch names.
func (r *Runner) ListBranches() ([]string, error) {
	out, err := r.RunGitCapture("branch", "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}
