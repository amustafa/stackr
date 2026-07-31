package git

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// anchorEpoch is a fixed timestamp used for anchor commits so that an unchanged
// set of anchored commits always hashes to the same commit SHA. The "@" prefix
// is required: git rejects a bare "0" as an invalid date.
const anchorEpoch = "@0 +0000"

// AnchorCommits keeps the given commits reachable by pointing ref at a synthetic
// commit that lists them all as parents. Commits that no longer exist are
// dropped; if none remain, ref is deleted.
//
// Why this is needed: stackr records a base commit per branch, and after the
// parent is amended or rebased that base is reachable from nothing — only the
// reflog keeps it alive. Once the reflog expires or `git gc --prune` runs, the
// base is collected and every later restack that needs it fails with
// "fatal: bad revision". Anchoring makes the bases genuinely durable.
//
// A single commit with N parents is used rather than one ref per branch because
// per-branch refs hit git's directory/file conflict for branch pairs like `foo`
// and `foo/bar`, which are perfectly legal branch names.
//
// The anchor is deterministic: fixed identity and timestamps mean an unchanged
// set of commits re-hashes to the same SHA, so repeated writes do not churn the
// ref or accumulate garbage.
func (r *Runner) AnchorCommits(ref, message string, shas []string) error {
	seen := make(map[string]bool, len(shas))
	parents := make([]string, 0, len(shas))
	for _, sha := range shas {
		if sha == "" || seen[sha] {
			continue
		}
		seen[sha] = true
		if !r.ObjectExists(sha) {
			continue
		}
		parents = append(parents, sha)
	}

	if len(parents) == 0 {
		if existing, _ := r.ReadRef(ref); existing != "" {
			return r.DeleteRef(ref)
		}
		return nil
	}

	// Sort so the anchor commit does not depend on map iteration order.
	sort.Strings(parents)

	tree, err := r.MakeTree(nil)
	if err != nil {
		return fmt.Errorf("anchor: empty tree: %w", err)
	}

	args := []string{"commit-tree", tree}
	for _, p := range parents {
		args = append(args, "-p", p)
	}
	args = append(args, "-m", message)

	var stdout, stderr bytes.Buffer
	cmd := r.command(args...)
	cmd.Env = append(cmd.Environ(),
		"GIT_AUTHOR_NAME=stackr",
		"GIT_AUTHOR_EMAIL=stackr@localhost",
		"GIT_AUTHOR_DATE="+anchorEpoch,
		"GIT_COMMITTER_NAME=stackr",
		"GIT_COMMITTER_EMAIL=stackr@localhost",
		"GIT_COMMITTER_DATE="+anchorEpoch,
	)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("anchor: commit-tree: %s: %w", stderr.String(), err)
	}
	anchor := strings.TrimSpace(stdout.String())

	if existing, _ := r.ReadRef(ref); existing == anchor {
		return nil
	}
	return r.UpdateRef(ref, anchor, "")
}
