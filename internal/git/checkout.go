package git

// Checkout switches to the named branch.
func (r *Runner) Checkout(branch string) error {
	return r.RunGit("checkout", branch)
}

// CheckoutNew creates and switches to a new branch from startPoint.
func (r *Runner) CheckoutNew(branch, startPoint string) error {
	args := []string{"checkout", "-b", branch}
	if startPoint != "" {
		args = append(args, startPoint)
	}
	return r.RunGit(args...)
}
