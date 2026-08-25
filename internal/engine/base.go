package engine

import (
	"fmt"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/graph"
)

// The base pointer invariant
//
// For every non-trunk branch, ParentBranchRevision is an ancestor of the branch,
// and the branch's own commits are exactly the range (ParentBranchRevision, branch].
//
// Everything that rewrites history — restack, squash, fold, split, absorb —
// must derive its commit range from that base, never from the parent's *name*.
// The two agree only while the branch is up to date; the moment the parent moves
// they diverge, and using the parent's current tip silently pulls the parent's
// commits into the child's range.

// baseSource records how a branch's base commit was determined.
type baseSource int

const (
	// baseRecorded means the stored pointer was valid and used as-is.
	baseRecorded baseSource = iota
	// baseForkPoint means the stored pointer was unusable and the base was
	// reconstructed from the parent's reflog.
	baseForkPoint
)

// resolvedBase is the outcome of resolving a branch's base commit.
type resolvedBase struct {
	SHA    string
	Source baseSource
}

// Recovered reports whether the base had to be reconstructed because the
// recorded pointer was unusable. Callers should surface this: a silent recovery
// hides the fact that the graph had drifted from reality.
func (rb resolvedBase) Recovered() bool { return rb.Source != baseRecorded }

// BaseUnresolvedError reports that a branch's base is neither recorded usably
// nor recoverable, so no commit range can be derived safely.
type BaseUnresolvedError struct {
	Branch   string
	Parent   string
	Recorded string
}

func (e *BaseUnresolvedError) Error() string {
	detail := "no base commit is recorded"
	if e.Recorded != "" {
		detail = fmt.Sprintf("recorded base %s is missing or is not an ancestor of the branch", abbrev(e.Recorded))
	}
	return fmt.Sprintf(
		"cannot determine which commits belong to %s: %s.\n"+
			"  Rebasing on a guess would duplicate or drop commits, so stackr stopped.\n"+
			"  Re-point it with:  sr restack --branch %s --base <sha>\n"+
			"  where <sha> is the commit %s was branched from (try `git log --oneline %s..%s`).",
		e.Branch, detail, e.Branch, e.Branch, e.Parent, e.Branch)
}

// resolveBase returns the commit that begins branch `name`'s own history: the
// base of the range (base, name] that a restack replays onto a new parent.
//
// The recorded pointer is authoritative only when it is usable, which requires
// both that it still resolves to a real commit and that it is an ancestor of the
// branch. The ancestry check is the one that catches genuine corruption: a base
// that is not an ancestor means (base, name] is not the branch's own work, and
// rebasing with it replays commits belonging to somebody else — which is exactly
// how a stale base re-applies a parent's superseded commits on top of the
// rewritten ones.
//
// When the recorded pointer fails those checks we fall back to
// `merge-base --fork-point`, which reads the parent's reflog and can therefore
// see through history the parent has rewritten.
//
// We deliberately do NOT fall back to a plain `git merge-base`. Once a parent
// has been amended, the plain merge base walks back PAST the rewritten commit to
// the grandparent, so the range would swallow the parent's own pre-amend work
// and duplicate it into the child. Reporting an unrecoverable base and letting
// the user name one explicitly is strictly better than silently duplicating or
// dropping commits.
func resolveBase(c *context.Context, name string, b *graph.BranchState) (resolvedBase, error) {
	if rec := b.ParentBranchRevision; rec != "" && c.Git.ObjectExists(rec) {
		if ok, _ := c.Git.IsAncestor(rec, name); ok {
			return resolvedBase{SHA: rec, Source: baseRecorded}, nil
		}
	}

	// Fork-point recovery is only trustworthy while the parent still has a
	// reflog. Without one, git answers with a plain merge-base instead of
	// failing — the very answer this function must never return — and nothing
	// in the result distinguishes the two. Check first, then ask.
	if c.Git.HasReflog(b.ParentBranchName) {
		if fp := c.Git.ForkPoint(b.ParentBranchName, name); fp != "" {
			if ok, _ := c.Git.IsAncestor(fp, name); ok {
				return resolvedBase{SHA: fp, Source: baseForkPoint}, nil
			}
		}
	}

	return resolvedBase{}, &BaseUnresolvedError{
		Branch:   name,
		Parent:   b.ParentBranchName,
		Recorded: b.ParentBranchRevision,
	}
}

// isStackedOn reports whether branch is already built on top of parentTip.
//
// The test is ancestry, not a comparison of recorded revisions. "The stored base
// equals the parent's tip" and "the branch is actually built on the parent's
// tip" are different claims, and trusting the former lets a branch that was
// rewritten with raw git — or by a stackr path that recorded its base too early
// — silently skip its restack and drift further out of date.
func isStackedOn(c *context.Context, branch, parentTip string) bool {
	ok, _ := c.Git.IsAncestor(parentTip, branch)
	return ok
}

// NeedsRestack reports whether a branch is no longer built on its parent's
// current tip. Trunk never needs a restack, and a branch whose parent can't
// be resolved is reported as fine — display callers (`sr log`) must not turn
// a lookup hiccup into a false alarm.
func NeedsRestack(c *context.Context, g *graph.Graph, branch string) bool {
	b := g.Branches[branch]
	if b == nil || b.IsTrunk {
		return false
	}
	parentRev, err := c.Git.RevParse(b.ParentBranchName)
	if err != nil {
		return false
	}
	return !isStackedOn(c, branch, parentRev)
}

// setBase re-points a branch's recorded base, validating that the result still
// satisfies the invariant. A base that is not an ancestor of the branch makes
// the recorded commit range meaningless, so it is rejected rather than stored.
func setBase(c *context.Context, g *graph.Graph, name, rev string) error {
	b := g.Branches[name]
	if b == nil {
		return fmt.Errorf("branch %q not tracked", name)
	}
	if b.IsTrunk {
		return fmt.Errorf("trunk has no base commit")
	}
	sha, err := c.Git.RevParse(rev)
	if err != nil {
		return fmt.Errorf("could not resolve base %q: %w", rev, err)
	}
	if ok, _ := c.Git.IsAncestor(sha, name); !ok {
		return fmt.Errorf("%s is not an ancestor of %s, so it cannot be that branch's base", abbrev(sha), name)
	}
	b.ParentBranchRevision = sha
	return nil
}

func abbrev(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
