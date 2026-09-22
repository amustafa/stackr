package git

// Checkout switches to the named branch.
func (r *Runner) Checkout(branch string) error {
	return r.RunGit("checkout", branch)
}

// CheckoutQuiet switches to branch without git's "Switched to branch" /
// "Already on" chatter. For operations that return to where the user started
// after working elsewhere — the move is bookkeeping, not news.
func (r *Runner) CheckoutQuiet(branch string) error {
	return r.RunGit("checkout", "--quiet", branch)
}

// CheckoutNew creates and switches to a new branch from startPoint.
func (r *Runner) CheckoutNew(branch, startPoint string) error {
	args := []string{"checkout", "-b", branch}
	if startPoint != "" {
		args = append(args, startPoint)
	}
	return r.RunGit(args...)
}
