package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// This file holds the auto-derived values — things the CLI computes rather than
// stores as config (spec 5): worktree paths, the process user, HOME, and the
// Claude project slug. Kept as pure functions so they are trivially testable.

// WorktreePath returns the canonical absolute path of a branch's worktree under
// the main repo root. It is canonicalized (symlinks resolved) so it matches,
// byte-for-byte, the path Claude inside the container will slugify — session
// continuity depends on that exact match (ADR-0008). If the path does not exist
// yet, the un-resolved join is returned.
func WorktreePath(mainRoot, branch string) string {
	p := filepath.Join(mainRoot, ".worktrees", branch)
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return p
}

// ProjectSlug mirrors how Claude Code names ~/.claude/projects/<slug> entries:
// the absolute path with "/" and "." replaced by "-". Best-effort — used for
// host-side discovery/verification only; continuity itself relies on the
// container computing the same slug from its own cwd, plus the manifest's
// session id as a fallback (ADR-0008).
func ProjectSlug(absPath string) string {
	r := strings.NewReplacer("/", "-", ".", "-")
	return r.Replace(absPath)
}

// ProcessUser returns the "uid:gid" the container should run as (the host user),
// so files it writes to bind mounts are owned by the developer.
func ProcessUser() string {
	return fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
}

// Home returns the host home directory, mounted into the container at the same
// path so ~/.claude resolves identically.
func Home() (string, error) {
	if h := os.Getenv("HOME"); h != "" {
		return h, nil
	}
	return os.UserHomeDir()
}

// MainRoot derives the main repo root (the directory containing the shared .git)
// from the git-common-dir. Works whether invoked from the repo root or a
// worktree, since the common dir always points at the main .git.
func MainRoot(gitCommonDir string) string {
	// gitCommonDir is typically "<mainRoot>/.git"; its parent is the main root.
	// Guard against a bare-repo layout by only stripping a trailing ".git".
	if filepath.Base(gitCommonDir) == ".git" {
		return filepath.Dir(gitCommonDir)
	}
	return gitCommonDir
}
