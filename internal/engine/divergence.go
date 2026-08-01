package engine

import (
	"fmt"

	"github.com/amustafa/stackr/internal/context"
)

// Disposition is what Submit should do with one branch.
type Disposition int

const (
	// DispNoPush means local and remote already agree.
	DispNoPush Disposition = iota
	// DispPushPlain means an ordinary push suffices — no force, no lease drama.
	DispPushPlain
	// DispPushForce means force-pushing is lossless; RemoteSHA pins the lease.
	DispPushForce
	// DispNeedsDecision means we cannot prove the push is lossless. The user
	// decides. Never reached by accident: every unexpected condition lands here.
	DispNeedsDecision
)

func (d Disposition) String() string {
	switch d {
	case DispNoPush:
		return "up to date"
	case DispPushPlain:
		return "push"
	case DispPushForce:
		return "force push (lossless)"
	default:
		return "needs a decision"
	}
}

// Classification is the verdict for one branch.
type Classification struct {
	Branch      string
	Disposition Disposition

	// RemoteSHA is the commit the remote held when we looked. It pins the push
	// lease, so it must be the value we actually reasoned about — not whatever
	// the remote-tracking ref says at push time. Empty when the remote ref is
	// absent, which is the correct "expect nothing" lease for a first push.
	RemoteSHA string

	// Reason explains a DispNeedsDecision to the user.
	Reason string

	// RemoteOnly lists the remote commits with no local equivalent. Populated
	// for DispNeedsDecision so remediation can cherry-pick exactly these.
	RemoteOnly []string
}

// maxUnmatchedCommits caps tier 2. Beyond this the branches have diverged so
// far that a per-commit replay is both slow and pointless — a human should look.
const maxUnmatchedCommits = 50

// ClassifyBranch decides whether pushing a branch over its remote would lose
// anything. It has no side effects.
//
// The ladder is ordered cheapest and most authoritative first. Only tier 0 (the
// Push Record) is sound; the content tiers below it are heuristics that exist to
// avoid prompting for the cases GitHub itself creates. Every unexpected
// condition resolves to DispNeedsDecision — being wrong in the direction of
// asking is free, being wrong in the direction of forcing is not. See ADR-0014.
func ClassifyBranch(c *context.Context, remote, branch string) (Classification, error) {
	res := Classification{Branch: branch}

	localSHA, err := c.Git.RevParse(branch)
	if err != nil {
		return res, fmt.Errorf("cannot resolve %s: %w", branch, err)
	}

	remoteRef := remote + "/" + branch
	exists, _ := c.Git.RemoteBranchExists(remote, branch)
	if !exists {
		// Nothing to overwrite. The empty-expect lease still asserts the ref is
		// absent, which closes the race where someone creates it first.
		res.Disposition = DispPushPlain
		return res, nil
	}

	remoteSHA, err := c.Git.RevParse(remoteRef)
	if err != nil {
		return res, fmt.Errorf("cannot resolve %s: %w", remoteRef, err)
	}
	res.RemoteSHA = remoteSHA

	if localSHA == remoteSHA {
		res.Disposition = DispNoPush
		return res, nil
	}

	// Remote is behind us and contained: a plain fast-forward.
	//
	// Note this is exactly the shape a collaborator's "drop a commit and force
	// push" also takes, and no content test can tell the two apart. Tier 0 can,
	// so consult the Push Record before trusting the fast-forward.
	if anc, err := c.Git.IsAncestor(remoteSHA, localSHA); err == nil && anc {
		if rec := c.Store.PushRecordFor(remote, branch); rec != "" && rec != remoteSHA {
			res.Disposition = DispNeedsDecision
			res.Reason = fmt.Sprintf("the remote was rewound: we last left %s there, it now holds %s",
				abbrev(rec), abbrev(remoteSHA))
			return res, nil
		}
		res.Disposition = DispPushPlain
		return res, nil
	}

	// We are strictly behind. Pushing would be a no-op at best; the user should
	// pull instead.
	if anc, err := c.Git.IsAncestor(localSHA, remoteSHA); err == nil && anc {
		res.Disposition = DispNeedsDecision
		res.Reason = "the remote is ahead of your local branch"
		return res, nil
	}

	// ---- Ref Divergence from here on. Force would be required. ----

	// Tier 0: the only sound test, with three outcomes.
	//
	//   match    — the remote still holds what we left there, so everything on
	//              it is ours and overwriting it loses nothing.
	//   mismatch — somebody else wrote. Stop. The content tiers below CANNOT
	//              clear this: if the collaborator's write dropped a commit and
	//              our branch was independently restacked, their deletion sits
	//              outside the merge base entirely, so the merge is a no-op and
	//              every content test reports "safe" while force-pushing
	//              resurrects the commit they removed. Absence of evidence is
	//              not evidence of absence here.
	//   absent   — we have never pushed this branch from this clone, so we have
	//              no claim either way; fall through to the heuristics.
	switch rec := c.Store.PushRecordFor(remote, branch); {
	case rec == remoteSHA && rec != "":
		res.Disposition = DispPushForce
		return res, nil
	case rec != "":
		res.Disposition = DispNeedsDecision
		res.Reason = fmt.Sprintf("somebody else pushed to this branch: we last left %s there, it now holds %s",
			abbrev(rec), abbrev(remoteSHA))
		res.RemoteOnly, _ = c.Git.RemoteOnlyCommits(branch, remoteRef)
		return res, nil
	}

	// Tiers 1 and 2 are content heuristics, reached only when tier 0 has nothing
	// to say. They can promote "we don't know" to "probably fine"; they are
	// never consulted to overturn a tier-0 mismatch.
	if !c.Git.SupportsMergeTreeWriteTree() {
		res.Disposition = DispNeedsDecision
		res.Reason = "git is too old for `merge-tree --write-tree` (needs 2.38+) to check this safely"
		return res, nil
	}

	if _, err := c.Git.MergeBase(branch, remoteRef); err != nil {
		res.Disposition = DispNeedsDecision
		res.Reason = "local and remote share no history"
		return res, nil
	}

	localTree, err := c.Git.TreeOf(branch)
	if err != nil {
		return res, err
	}

	// Tier 1: would merging the remote in change our tree at all?
	merged, err := c.Git.MergeTreeWriteTree(branch, remoteRef)
	if err != nil {
		res.Disposition = DispNeedsDecision
		res.Reason = "could not compare against the remote: " + err.Error()
		return res, nil
	}
	if !merged.Clean {
		res.Disposition = DispNeedsDecision
		res.Reason = "the remote conflicts with your local branch"
		res.RemoteOnly, _ = c.Git.RemoteOnlyCommits(branch, remoteRef)
		return res, nil
	}
	if merged.Tree != localTree {
		res.Disposition = DispNeedsDecision
		res.Reason = "the remote has changes your branch does not contain"
		res.RemoteOnly, _ = c.Git.RemoteOnlyCommits(branch, remoteRef)
		return res, nil
	}

	// Tier 2: tier 1 compares net diffs, so a pair of remote commits that cancel
	// out — most commonly a commit and its revert — is invisible to it. Replay
	// each unmatched remote commit on its own; a revert shows up as a deletion.
	remoteOnly, err := c.Git.RemoteOnlyCommits(branch, remoteRef)
	if err != nil {
		res.Disposition = DispNeedsDecision
		res.Reason = "could not enumerate remote commits: " + err.Error()
		return res, nil
	}
	if len(remoteOnly) > maxUnmatchedCommits {
		res.Disposition = DispNeedsDecision
		res.Reason = fmt.Sprintf("%d remote commits have no local equivalent — too many to check", len(remoteOnly))
		res.RemoteOnly = remoteOnly
		return res, nil
	}

	for _, sha := range remoteOnly {
		contained, reason := commitAlreadyContained(c, branch, localTree, sha)
		if contained {
			continue
		}
		res.Disposition = DispNeedsDecision
		res.Reason = reason
		res.RemoteOnly = remoteOnly
		return res, nil
	}

	res.Disposition = DispPushForce
	return res, nil
}

// commitAlreadyContained reports whether replaying one remote commit onto the
// branch would be a no-op — i.e. its effect is already present locally.
//
// Merge commits are compared against their first parent, which asks what the
// merge itself brought in. That is the right question for a "Update branch"
// merge and the only well-defined one for an octopus merge.
func commitAlreadyContained(c *context.Context, branch, localTree, sha string) (bool, string) {
	if c.Git.IsRootCommit(sha) {
		return false, fmt.Sprintf("remote commit %s is a root commit and cannot be checked", abbrev(sha))
	}

	parent, err := c.Git.FirstParent(sha)
	if err != nil {
		return false, fmt.Sprintf("could not resolve the parent of remote commit %s", abbrev(sha))
	}

	replayed, err := c.Git.MergeTreeOnto(parent, branch, sha)
	if err != nil {
		return false, fmt.Sprintf("could not replay remote commit %s: %v", abbrev(sha), err)
	}
	if !replayed.Clean {
		return false, fmt.Sprintf("remote commit %s conflicts with your branch", abbrev(sha))
	}
	if replayed.Tree != localTree {
		return false, fmt.Sprintf("remote commit %s is not contained in your branch", abbrev(sha))
	}
	return true, ""
}
