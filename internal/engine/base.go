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
//
// The invariant is also what every path that changes a branch's parent or tip
// behind a restack's back — a parent deleted out from under it, a tip replaced
// by the remote's — must restore before it writes the graph. reconcileBase is
// the one way to do that.

// baseSource records how a branch's base commit was determined.
type baseSource int

const (
	// baseRecorded means the stored pointer was valid and used as-is.
	baseRecorded baseSource = iota
	// baseForkPoint means the stored pointer was unusable and the base was
	// reconstructed from the parent's reflog.
	baseForkPoint
	// baseMergeBase means the stored pointer was unusable, the reflog could not
	// help, and the parent is trunk, so the plain merge-base with trunk was
	// taken. Only trunk earns this: see resolveBase for why it is safe there
	// and nowhere else.
	baseMergeBase
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

// recoveryNote says how a recovered base was found, for the user's terminal.
// Empty when nothing was recovered.
func (rb resolvedBase) recoveryNote(branch, parent string) string {
	switch rb.Source {
	case baseForkPoint:
		return fmt.Sprintf("Note: recorded base for %s was unusable; recovered %s from %s's reflog",
			branch, abbrev(rb.SHA), parent)
	case baseMergeBase:
		return fmt.Sprintf("Note: recorded base for %s was unusable; recovered %s as its merge-base with %s",
			branch, abbrev(rb.SHA), parent)
	}
	return ""
}

// BaseUnresolvedError reports that a branch's base is neither recorded usably
// nor recoverable, so no commit range can be derived safely.
type BaseUnresolvedError struct {
	Branch   string
	Parent   string
	Recorded string

	// ParentIsTrunk says whether Parent is the trunk. It decides how much the
	// suggestion below can be trusted.
	ParentIsTrunk bool
	// Suggested is `git merge-base Parent Branch`, when git could compute one.
	// For a trunk parent it is the answer; for any other parent it is only a
	// candidate, because an amended parent pushes the merge-base back past
	// the rewrite — the very reason resolveBase refuses to take it on its own.
	Suggested string
}

func (e *BaseUnresolvedError) Error() string {
	detail := "no base commit is recorded"
	if e.Recorded != "" {
		detail = fmt.Sprintf("recorded base %s is missing or is not an ancestor of the branch", abbrev(e.Recorded))
	}
	head := fmt.Sprintf(
		"cannot determine which commits belong to %s: %s.\n"+
			"  Rebasing on a guess would duplicate or drop commits, so stackr stopped.\n",
		e.Branch, detail)

	switch {
	case e.Suggested != "" && e.ParentIsTrunk:
		return head + fmt.Sprintf(
			"  Re-point it with:  sr restack --branch %s --base %s\n"+
				"  (%s is `git merge-base %s %s`; %s only grows, so that is where %s's own commits begin.)",
			e.Branch, e.Suggested, abbrev(e.Suggested), e.Parent, e.Branch, e.Parent, e.Branch)
	case e.Suggested != "":
		return head + fmt.Sprintf(
			"  Re-point it with:  sr restack --branch %s --base <sha>\n"+
				"  where <sha> is the commit %s was branched from. `git merge-base %s %s` says %s, which is right\n"+
				"  unless %s's history was rewritten since; check that `git log --oneline %s..%s` lists only %s's own commits.",
			e.Branch, e.Branch, e.Parent, e.Branch, abbrev(e.Suggested), e.Parent, abbrev(e.Suggested), e.Branch, e.Branch)
	default:
		return head + fmt.Sprintf(
			"  Re-point it with:  sr restack --branch %s --base <sha>\n"+
				"  where <sha> is the commit %s was branched from (try `git merge-base %s %s`).",
			e.Branch, e.Branch, e.Parent, e.Branch)
	}
}

// resolveBase returns the commit that begins branch `name`'s own history: the
// base of the range (base, name] that a restack replays onto a new parent.
// parentIsTrunk says whether the branch's parent is the trunk, which unlocks
// the last recovery tier.
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
// We deliberately do NOT fall back to a plain `git merge-base` for an ordinary
// parent. Once a parent has been amended, the plain merge base walks back PAST
// the rewritten commit to the grandparent, so the range would swallow the
// parent's own pre-amend work and duplicate it into the child. Reporting an
// unrecoverable base and letting the user name one explicitly is strictly
// better than silently duplicating or dropping commits.
//
// Trunk is the exception, because trunk is never amended: it only ever grows.
// The merge-base with trunk therefore cannot land before the commit the branch
// was built on, and it IS the definition of where a trunk-parented branch's
// own commits begin. This tier is what a stack needs after its parent
// squash-merges and the forge rebuilds the child onto the squash commit: the
// recorded base is the deleted parent's tip, and trunk's reflog never visited
// the squash commit — a local trunk advances in fast-forward jumps, so
// fork-point has nothing to see through.
func resolveBase(c *context.Context, name string, b *graph.BranchState, parentIsTrunk bool) (resolvedBase, error) {
	if rec := b.ParentBranchRevision; rec != "" && c.Git.ObjectExists(rec) {
		if ok, _ := c.Git.IsAncestor(rec, name); ok {
			return resolvedBase{SHA: rec, Source: baseRecorded}, nil
		}
	}

	// Fork-point recovery is only trustworthy while the parent still has a
	// reflog. Without one, git answers with a plain merge-base instead of
	// failing — the very answer this function must never return for an
	// ordinary parent — and nothing in the result distinguishes the two.
	// Check first, then ask.
	if c.Git.HasReflog(b.ParentBranchName) {
		if fp := c.Git.ForkPoint(b.ParentBranchName, name); fp != "" {
			if ok, _ := c.Git.IsAncestor(fp, name); ok {
				return resolvedBase{SHA: fp, Source: baseForkPoint}, nil
			}
		}
	}

	mb := trunkMergeBase(c, b.ParentBranchName, name)
	if parentIsTrunk && mb != "" {
		return resolvedBase{SHA: mb, Source: baseMergeBase}, nil
	}

	return resolvedBase{}, &BaseUnresolvedError{
		Branch:        name,
		Parent:        b.ParentBranchName,
		Recorded:      b.ParentBranchRevision,
		ParentIsTrunk: parentIsTrunk,
		Suggested:     mb,
	}
}

// trunkMergeBase is `git merge-base parent name`, checked to be an ancestor of
// name (which a merge-base always is, unless the two share no history at all,
// where git errors). Empty when there is no answer.
func trunkMergeBase(c *context.Context, parent, name string) string {
	mb, err := c.Git.MergeBase(parent, name)
	if err != nil || mb == "" {
		return ""
	}
	if ok, _ := c.Git.IsAncestor(mb, name); !ok {
		return ""
	}
	return mb
}

// reconcileBase restores the base pointer invariant for a branch whose parent
// or tip was just changed outside a restack — the two ways a branch's history
// can change without stackr having rebased it:
//
//   - its parent was deleted and it was reparented onto the grandparent
//     (sync cleaning up a merged branch), or
//   - its tip was replaced by the remote's (get accepting a diverged remote).
//
// The candidates are the bases that would be correct if nothing surprising
// happened, best first: the deleted parent's tip, the parent's current tip,
// the pointer recorded before the change. The first that is an ancestor of
// the branch is taken. Taking the first candidate is the ordinary case and
// says nothing; every other outcome is reported in the returned note, since
// a graph that quietly re-derived its own record is a graph the user can no
// longer reason about.
//
// When no candidate fits, the parent being trunk makes the merge-base with it
// the right answer (resolveBase explains why trunk alone earns that). For any
// other parent the pointer is left as it is: a later restack stops loudly on
// it with a concrete suggestion, which beats guessing here. The note names
// the situation either way. why says what just happened, for the note.
func reconcileBase(c *context.Context, g *graph.Graph, name, why string, candidates ...string) string {
	b := g.Branches[name]
	if b == nil || b.IsTrunk {
		return ""
	}
	// Ancestry is checked against the branch's real tip, so record it too:
	// the graph's copy is exactly the thing that may be stale here.
	if tip, err := c.Git.RevParse(name); err == nil {
		b.BranchRevision = tip
	}

	for i, cand := range candidates {
		if cand == "" || !c.Git.ObjectExists(cand) {
			continue
		}
		if ok, _ := c.Git.IsAncestor(cand, name); !ok {
			continue
		}
		b.ParentBranchRevision = cand
		if i == 0 {
			return ""
		}
		return fmt.Sprintf("Note: %s is not built on %s's tip; kept its recorded base %s %s",
			name, b.ParentBranchName, abbrev(cand), why)
	}

	if g.IsTrunk(b.ParentBranchName) {
		if mb := trunkMergeBase(c, b.ParentBranchName, name); mb != "" {
			b.ParentBranchRevision = mb
			return fmt.Sprintf("Note: recorded base for %s was unusable %s; re-derived %s as its merge-base with %s",
				name, why, abbrev(mb), b.ParentBranchName)
		}
	}

	return fmt.Sprintf("Note: could not determine %s's base %s; `sr restack` will stop on it and say what to do",
		name, why)
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
