package git

// Init initializes a new git repository in the runner's directory.
func (r *Runner) Init() error {
	return r.RunGit("init")
}

// IsHeadUnborn returns true if HEAD points to a branch with no commits.
func (r *Runner) IsHeadUnborn() bool {
	_, err := r.RunGitCapture("rev-parse", "HEAD")
	return err != nil
}
