package git

// DiffStat returns the --stat output between two refs.
func (r *Runner) DiffStat(base, head string) (string, error) {
	return r.RunGitCapture("diff", "--stat", base+".."+head)
}

// DiffPatch returns the full diff between two refs.
func (r *Runner) DiffPatch(base, head string) (string, error) {
	return r.RunGitCapture("diff", base+".."+head)
}

// DiffNameOnly returns changed file names between two refs.
func (r *Runner) DiffNameOnly(base, head string) (string, error) {
	return r.RunGitCapture("diff", "--name-only", base+".."+head)
}
