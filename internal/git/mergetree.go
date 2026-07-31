package git

import (
	"strings"
)

// MergeTreeResult reports what an in-memory three-way merge produced.
//
// Tree is the merged tree's OID. Clean says whether the merge succeeded without
// conflicts; git prints a tree OID on the first line even when it conflicts, so
// Tree alone cannot be used to tell the two apart.
type MergeTreeResult struct {
	Tree  string
	Clean bool
}

// MergeTreeWriteTree three-way merges two commits entirely in the object store —
// no worktree, no index, safe to run in the middle of any other operation — and
// returns the resulting tree.
//
// This answers "would merging theirs into ours change anything?", which is the
// question behind Content Divergence: if the merged tree equals ours, theirs
// contributes nothing we do not already have.
//
// Exit codes carry the meaning: 0 is a clean merge, 1 is a conflicted merge (and
// also, unhelpfully, an unmergeable argument), anything else is a real failure
// such as unrelated histories. Callers must treat every error as "cannot say",
// never as "safe" — see ClassifyBranch.
func (r *Runner) MergeTreeWriteTree(ours, theirs string) (MergeTreeResult, error) {
	return r.mergeTree(nil, ours, theirs)
}

// MergeTreeOnto is MergeTreeWriteTree with an explicit merge base, which turns a
// branch-level comparison into a single-commit one: with base X^ it asks what
// commit X alone would contribute when replayed onto ours.
func (r *Runner) MergeTreeOnto(mergeBase, ours, theirs string) (MergeTreeResult, error) {
	return r.mergeTree([]string{"--merge-base=" + mergeBase}, ours, theirs)
}

func (r *Runner) mergeTree(extra []string, ours, theirs string) (MergeTreeResult, error) {
	args := append([]string{"merge-tree", "--write-tree"}, extra...)
	args = append(args, ours, theirs)

	stdout, stderr, err := r.RunGitCaptureAll(args...)
	if err == nil {
		return MergeTreeResult{Tree: firstLine(stdout), Clean: true}, nil
	}

	// Exit 1 means "conflicted merge" — a real answer — but git also exits 1 when
	// an argument is not something it can merge, in which case there is no tree
	// on stdout. Distinguish on the output, not the status.
	if tree := firstLine(stdout); isSHA(tree) {
		return MergeTreeResult{Tree: tree, Clean: false}, nil
	}

	return MergeTreeResult{}, &gitMergeTreeError{args: args, stderr: stderr, err: err}
}

type gitMergeTreeError struct {
	args   []string
	stderr string
	err    error
}

func (e *gitMergeTreeError) Error() string {
	msg := strings.TrimSpace(e.stderr)
	if msg == "" {
		msg = e.err.Error()
	}
	return "git " + strings.Join(e.args, " ") + ": " + msg
}

func (e *gitMergeTreeError) Unwrap() error { return e.err }

// TreeOf returns the tree OID a revision points at.
func (r *Runner) TreeOf(rev string) (string, error) {
	return r.RunGitCapture("rev-parse", rev+"^{tree}")
}

// RemoteOnlyCommits returns the commits reachable from remote but not from local
// that have no patch-equivalent commit on the local side, oldest first.
//
// --cherry-pick drops commits whose patch already appears on the other side,
// which is what makes this survive a rebase: the same work replayed under a new
// SHA is matched and filtered out.
//
// Merge commits are deliberately NOT excluded. A merge can carry content of its
// own — a conflict resolution that appears in neither parent — and dropping
// merges here would let that content be force-pushed away unseen.
func (r *Runner) RemoteOnlyCommits(local, remote string) ([]string, error) {
	out, err := r.RunGitCapture("rev-list", "--right-only", "--cherry-pick",
		"--reverse", local+"..."+remote)
	if err != nil {
		return nil, err
	}
	return nonEmptyLines(out), nil
}

// IsRootCommit reports whether a commit has no parents. A root commit cannot be
// replayed onto anything, so callers must stop rather than guess.
func (r *Runner) IsRootCommit(sha string) bool {
	out, err := r.RunGitCapture("rev-list", "--parents", "-n", "1", sha)
	if err != nil {
		return false
	}
	return len(strings.Fields(out)) == 1
}

// FirstParent returns a commit's first parent. For a merge commit this is the
// branch it was merged into, so replaying against it asks what the merge itself
// brought in — the right question for a "Update branch" style merge, and the
// only well-defined one for an octopus merge.
func (r *Runner) FirstParent(sha string) (string, error) {
	return r.RunGitCapture("rev-parse", sha+"^1")
}

// SupportsMergeTreeWriteTree reports whether git understands
// `merge-tree --write-tree` (git 2.38+). Older git must fall back to asking the
// user rather than silently skipping the check.
func (r *Runner) SupportsMergeTreeWriteTree() bool {
	_, stderr, err := r.RunGitCaptureAll("merge-tree", "--write-tree", "-h")
	if err == nil {
		return true
	}
	// `-h` exits non-zero while still printing usage; an unknown option is the
	// signal we actually care about.
	return !strings.Contains(stderr, "unknown option") &&
		!strings.Contains(stderr, "usage: git merge-tree [-z] [--trivial-merge]")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

func nonEmptyLines(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func isSHA(s string) bool {
	if len(s) < 7 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
