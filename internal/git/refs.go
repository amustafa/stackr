package git

import "strings"

// RevParse resolves a revision to a full SHA.
func (r *Runner) RevParse(rev string) (string, error) {
	return r.RunGitCapture("rev-parse", rev)
}

// RevParseShort resolves a revision to a short SHA.
func (r *Runner) RevParseShort(rev string) (string, error) {
	return r.RunGitCapture("rev-parse", "--short", rev)
}

// MergeBase returns the best common ancestor of two commits.
func (r *Runner) MergeBase(a, b string) (string, error) {
	return r.RunGitCapture("merge-base", a, b)
}

// IsAncestor returns true if ancestor is an ancestor of descendant.
func (r *Runner) IsAncestor(ancestor, descendant string) (bool, error) {
	_, _, err := r.RunGitCaptureAll("merge-base", "--is-ancestor", ancestor, descendant)
	if err != nil {
		return false, nil
	}
	return true, nil
}

// ForkPoint returns the commit where branch diverged from ref, or "" when git
// cannot determine one.
//
// Unlike a plain merge-base, this consults ref's reflog, so it can still find
// the divergence point after ref's history has been rewritten (amend, rebase,
// squash). That makes it the one recovery mechanism that stays correct when a
// recorded base pointer has been lost. It is best-effort: reflogs are local and
// expire, so a "" result is normal and must be handled, not treated as an error.
func (r *Runner) ForkPoint(ref, branch string) string {
	sha, err := r.RunGitCapture("merge-base", "--fork-point", ref, branch)
	if err != nil {
		return ""
	}
	return sha
}

// HasReflog reports whether ref has at least one reflog entry.
//
// This must gate every use of ForkPoint. When the reflog is missing or expired,
// `merge-base --fork-point` does not fail — it silently falls back to a plain
// merge-base and returns a confident-looking SHA with none of the information
// that made fork-point trustworthy. A plain merge-base is precisely the wrong
// answer after a parent has been rewritten: it walks back past the rewritten
// commit, so the child's range swallows the parent's superseded work.
func (r *Runner) HasReflog(ref string) bool {
	out, _, err := r.RunGitCaptureAll("reflog", "show", "--format=%H", "-n", "1", ref)
	return err == nil && strings.TrimSpace(out) != ""
}

// HasCommitsSince reports whether branch has any commits after base.
func (r *Runner) HasCommitsSince(base, branch string) (bool, error) {
	out, err := r.RunGitCapture("rev-list", "--count", base+".."+branch)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "0", nil
}

// ObjectExists reports whether sha resolves to an existing commit.
func (r *Runner) ObjectExists(sha string) bool {
	if sha == "" {
		return false
	}
	_, _, err := r.RunGitCaptureAll("cat-file", "-e", sha+"^{commit}")
	return err == nil
}

// AllCommitsUpstream reports whether every commit in (base, branch] already has
// a patch-equivalent commit in upstream.
//
// This is how a squash-merged or rebase-merged branch is detected. Those merge
// strategies rewrite the commits when landing them, so the branch never becomes
// an ancestor of trunk and plain ancestry checks report "not merged" forever.
// git cherry compares patch IDs instead of SHAs and sees through the rewrite.
//
// An empty range returns false: a branch with no commits of its own has not
// landed, it simply has nothing in it, and must not be cleaned up as merged.
func (r *Runner) AllCommitsUpstream(upstream, branch, base string) (bool, error) {
	out, err := r.RunGitCapture("cherry", upstream, branch, base)
	if err != nil {
		return false, err
	}
	if out == "" {
		return false, nil
	}
	for _, line := range strings.Split(out, "\n") {
		// git cherry prefixes "+" for commits with no upstream equivalent
		// and "-" for those already applied upstream.
		if strings.HasPrefix(strings.TrimSpace(line), "+") {
			return false, nil
		}
	}
	return true, nil
}

// RepoRoot returns the absolute path of the repository root.
func (r *Runner) RepoRoot() (string, error) {
	return r.RunGitCapture("rev-parse", "--show-toplevel")
}

// GitDir returns the path to the .git directory.
// In a worktree this returns the worktree-specific dir (e.g. .git/worktrees/name).
func (r *Runner) GitDir() (string, error) {
	return r.RunGitCapture("rev-parse", "--git-dir")
}

// GitCommonDir returns the shared .git directory.
// Unlike GitDir, this always returns the main .git dir even from a worktree.
func (r *Runner) GitCommonDir() (string, error) {
	return r.RunGitCapture("rev-parse", "--git-common-dir")
}
