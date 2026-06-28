package git

// Commit creates a commit with the given message and options.
func (r *Runner) Commit(msg string, opts CommitOpts) error {
	args := []string{"commit"}
	if msg != "" {
		args = append(args, "-m", msg)
	}
	if opts.All {
		args = append(args, "-a")
	}
	if opts.Amend {
		args = append(args, "--amend")
	}
	if opts.AllowEmpty {
		args = append(args, "--allow-empty")
	}
	if opts.NoEdit {
		args = append(args, "--no-edit")
	}
	if opts.Edit {
		args = append(args, "--edit")
	}
	if !r.Verify {
		args = append(args, "--no-verify")
	}
	return r.RunGit(args...)
}

// CommitOpts holds options for git commit.
type CommitOpts struct {
	All        bool
	Amend      bool
	AllowEmpty bool
	NoEdit     bool
	Edit       bool
}

// Add stages files.
func (r *Runner) Add(paths ...string) error {
	args := append([]string{"add"}, paths...)
	return r.RunGit(args...)
}

// AddAll stages all changes (git add -A).
func (r *Runner) AddAll() error {
	return r.RunGit("add", "-A")
}

// AddUpdate stages all tracked file changes (git add -u).
func (r *Runner) AddUpdate() error {
	return r.RunGit("add", "-u")
}
