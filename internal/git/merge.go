package git

import (
	"errors"
	"fmt"
)

// MergeConflictError indicates a merge that stopped due to conflicts.
type MergeConflictError struct {
	Theirs string
}

func (e *MergeConflictError) Error() string {
	return fmt.Sprintf("merge conflict with %s — resolve and run `sr continue`", e.Theirs)
}

// IsMergeConflict returns true if the error is a MergeConflictError.
func IsMergeConflict(err error) bool {
	var mce *MergeConflictError
	return errors.As(err, &mce)
}

// MergeFF fast-forwards branch to target without requiring checkout.
// If branch is currently checked out, uses git merge --ff-only.
// Otherwise updates the ref directly via git update-ref.
func (r *Runner) MergeFF(branch, target string) error {
	targetSHA, err := r.RevParse(target)
	if err != nil {
		return fmt.Errorf("resolving target %q: %w", target, err)
	}

	isAncestor, err := r.IsAncestor(branch, target)
	if err != nil {
		return err
	}
	if !isAncestor {
		return fmt.Errorf("cannot fast-forward %s to %s: not a descendant", branch, target)
	}

	current, _ := r.CurrentBranch()
	if current == branch {
		return r.RunGit("merge", "--ff-only", target)
	}

	return r.RunGit("update-ref", "refs/heads/"+branch, targetSHA)
}

// Merge merges theirs into the current branch with a merge commit.
// Returns MergeConflictError if conflicts occur.
func (r *Runner) Merge(theirs string) error {
	err := r.RunGit("merge", "--no-edit", theirs)
	if err != nil {
		if r.IsMergeInProgress() {
			return &MergeConflictError{Theirs: theirs}
		}
		return err
	}
	return nil
}

// IsMergeInProgress checks for MERGE_HEAD which exists during an unfinished merge.
func (r *Runner) IsMergeInProgress() bool {
	_, err := r.RunGitCapture("rev-parse", "--verify", "MERGE_HEAD")
	return err == nil
}

// HasDiverged returns true if local and remote have diverged —
// neither is an ancestor of the other.
func (r *Runner) HasDiverged(local, remote string) (bool, error) {
	localIsAnc, err := r.IsAncestor(local, remote)
	if err != nil {
		return false, err
	}
	if localIsAnc {
		return false, nil
	}

	remoteIsAnc, err := r.IsAncestor(remote, local)
	if err != nil {
		return false, err
	}
	if remoteIsAnc {
		return false, nil
	}

	return true, nil
}
