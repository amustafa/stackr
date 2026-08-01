package git

import (
	"errors"
	"fmt"
	"strings"
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

// MergeSquash stages the changes theirs brings in without recording a merge
// commit, leaving the caller to commit them as one ordinary commit.
//
// A real merge commit is wrong inside a stack: `git rebase` linearises, so the
// next Restack would drop it and replay both parents flat. Squashing keeps the
// branch a flat sequence on top of its parent — the invariant resolveBase and
// RebaseOnto depend on — and resolves conflicts once rather than once per commit.
func (r *Runner) MergeSquash(theirs string) error {
	_, stderr, err := r.RunGitCaptureAll("merge", "--squash", theirs)
	if err == nil {
		return nil
	}
	if r.HasConflicts() || strings.Contains(stderr, "CONFLICT") {
		return &MergeConflictError{Theirs: theirs}
	}
	return fmt.Errorf("merge --squash %s failed: %s", theirs, strings.TrimSpace(stderr))
}

// MergeSquashAbort discards a squash merge that stopped on conflicts. A squash
// merge records no MERGE_HEAD, so `git merge --abort` does not apply; resetting
// the index and worktree to HEAD is the equivalent.
func (r *Runner) MergeSquashAbort() error {
	return r.RunGit("reset", "--merge")
}

// CherryPick replays commits onto the current branch, oldest first.
func (r *Runner) CherryPick(shas ...string) error {
	if len(shas) == 0 {
		return nil
	}
	args := append([]string{"cherry-pick", "--allow-empty", "--keep-redundant-commits"}, shas...)
	_, stderr, err := r.RunGitCaptureAll(args...)
	if err == nil {
		return nil
	}
	if r.IsCherryPickInProgress() {
		return &MergeConflictError{Theirs: shas[0]}
	}
	return fmt.Errorf("cherry-pick failed: %s", strings.TrimSpace(stderr))
}

// CherryPickAbort restores the branch to its pre-cherry-pick state.
func (r *Runner) CherryPickAbort() error {
	return r.RunGit("cherry-pick", "--abort")
}

// IsCherryPickInProgress checks for CHERRY_PICK_HEAD.
func (r *Runner) IsCherryPickInProgress() bool {
	_, err := r.RunGitCapture("rev-parse", "--verify", "CHERRY_PICK_HEAD")
	return err == nil
}

// HasConflicts reports whether the index holds unmerged paths.
func (r *Runner) HasConflicts() bool {
	out, err := r.RunGitCapture("diff", "--name-only", "--diff-filter=U")
	return err == nil && strings.TrimSpace(out) != ""
}

// ResetHard moves a branch to a revision, discarding local commits and changes.
func (r *Runner) ResetHard(rev string) error {
	return r.RunGit("reset", "--hard", rev)
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
