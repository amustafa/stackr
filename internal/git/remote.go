package git

import (
	"fmt"
	"os"
	"strings"
)

// StaleLeaseError reports a push rejected because the remote no longer held the
// commit we inspected — someone else wrote to the branch between our check and
// our push. It is the safety net working, not a failure to route around.
type StaleLeaseError struct {
	Branch string
	Expect string
	Stderr string
}

func (e *StaleLeaseError) Error() string {
	return fmt.Sprintf("push of %s rejected: the remote moved off %s while we were working",
		e.Branch, abbrevSHA(e.Expect))
}

// IsStaleLease reports whether an error is a lease rejection.
func IsStaleLease(err error) bool {
	var sle *StaleLeaseError
	return asStaleLease(err, &sle)
}

func asStaleLease(err error, target **StaleLeaseError) bool {
	for err != nil {
		if e, ok := err.(*StaleLeaseError); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// PushPinned force-pushes a branch with a lease pinned to an exact commit.
//
// The unqualified `--force-with-lease` compares the remote against the local
// remote-tracking ref, which any fetch has just updated — so after stackr
// fetches, the lease expects whatever the remote currently holds and passes by
// construction, destroying a collaborator's commit. Pinning to expectSHA, the
// commit we actually inspected and made a decision about, is what closes the
// window between the check and the push.
//
// An empty expectSHA asserts the remote ref does not exist yet, which is the
// correct lease for a first push and also closes the create race.
func (r *Runner) PushPinned(remote, branch, expectSHA string, setUpstream bool) error {
	lease := "--force-with-lease=refs/heads/" + branch + ":" + expectSHA

	args := []string{"push"}
	if setUpstream {
		args = append(args, "-u")
	}
	// An explicit refspec keeps the destination independent of push.default and
	// of whether an upstream is configured; the lease is a pure server-side
	// compare-and-swap and needs no remote-tracking ref to work.
	args = append(args, lease, remote, branch+":refs/heads/"+branch)

	stdout, stderr, err := r.RunGitCaptureAll(args...)
	if stderr != "" {
		fmt.Fprintln(os.Stderr, stderr)
	}
	if stdout != "" {
		fmt.Println(stdout)
	}
	if err == nil {
		return nil
	}
	if isStaleLeaseStderr(stderr) {
		return &StaleLeaseError{Branch: branch, Expect: expectSHA, Stderr: stderr}
	}
	return fmt.Errorf("push of %s failed: %s", branch, firstNonEmptyLine(stderr, err.Error()))
}

// isStaleLeaseStderr distinguishes a lease rejection from a transport failure.
// git reports the former as a rejected ref update mentioning stale info; a
// network or auth failure produces neither.
func isStaleLeaseStderr(stderr string) bool {
	s := strings.ToLower(stderr)
	if !strings.Contains(s, "rejected") && !strings.Contains(s, "failed to push") {
		return false
	}
	return strings.Contains(s, "stale info") || strings.Contains(s, "fetch first") ||
		strings.Contains(s, "non-fast-forward")
}

func firstNonEmptyLine(s, fallback string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return fallback
}

func abbrevSHA(sha string) string {
	if sha == "" {
		return "(absent)"
	}
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// Push pushes a branch to the remote.
func (r *Runner) Push(remote, branch string, force bool) error {
	args := []string{"push", remote, branch}
	if force {
		args = []string{"push", "--force-with-lease", remote, branch}
	}
	return r.RunGit(args...)
}

// PushWithUpstream pushes and sets upstream tracking.
func (r *Runner) PushWithUpstream(remote, branch string, force bool) error {
	args := []string{"push", "-u", remote, branch}
	if force {
		args = []string{"push", "--force-with-lease", "-u", remote, branch}
	}
	return r.RunGit(args...)
}

// Fetch fetches from the remote.
func (r *Runner) Fetch(remote string) error {
	return r.RunGit("fetch", remote)
}

// FetchPrune fetches and prunes deleted remote branches.
func (r *Runner) FetchPrune(remote string) error {
	return r.RunGit("fetch", "--prune", remote)
}

// RemoteBranchExists checks if a branch exists on the remote.
func (r *Runner) RemoteBranchExists(remote, branch string) (bool, error) {
	_, err := r.RunGitCapture("rev-parse", "--verify", "refs/remotes/"+remote+"/"+branch)
	return err == nil, nil
}

// AddRemote adds a named remote with the given URL.
func (r *Runner) AddRemote(name, url string) error {
	return r.RunGit("remote", "add", name, url)
}

// ListRemotes returns the list of configured remotes.
func (r *Runner) ListRemotes() ([]string, error) {
	out, err := r.RunGitCapture("remote")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// IsMergedInto checks if branch is merged into target.
func (r *Runner) IsMergedInto(branch, target string) (bool, error) {
	return r.IsAncestor(branch, target)
}
