package engine

import (
	"fmt"

	"github.com/amustafa/stackr/internal/context"
	srerr "github.com/amustafa/stackr/internal/errors"
	"github.com/amustafa/stackr/internal/graph"
)

// MoveOpts holds options for moving a branch to a new parent.
type MoveOpts struct {
	Onto      string // Move Target: the branch to become the new parent
	Source    string // Move Source: branch to move (default: current)
	NoRestack bool   // Repoint the graph but skip the restack half
}

// MoveTarget is one candidate parent, annotated with the reason it cannot be
// chosen. Reason is empty when the branch is a legal target.
type MoveTarget struct {
	Branch string
	Reason string
}

// Move relocates a branch to a new parent.
//
// A move is a graph repoint followed by a restack — it performs no rebase of
// its own. Repointing the parent *is* the move; replaying commits onto the new
// parent is what a restack already does, for the branch and everything stacked
// above it. Delegating rather than rebasing here is what gives move conflict
// handling, `sr continue` resumption, and the frozen wall for free. A
// self-contained rebase would have to reimplement all three, and the earlier
// implementation reimplemented none of them: it rebased only the source's own
// commits and left the entire upstack sitting on a base that no longer existed,
// with nothing in the output to say so.
//
// The graph is written before the restack runs, so a conflict leaves a
// repointed graph mid-operation. That is deliberate — it is the state
// `sr continue` resumes into — and it is why the undo point is taken first.
func Move(c *context.Context, opts MoveOpts) error {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}

	source := opts.Source
	if source == "" {
		source, err = c.Git.CurrentBranch()
		if err != nil {
			return err
		}
	}
	if err := ValidateMoveSource(g, source); err != nil {
		return err
	}

	onto := opts.Onto
	if onto == "" {
		return fmt.Errorf("--onto is required")
	}
	if !g.Has(onto) {
		return fmt.Errorf("target branch %q not tracked", onto)
	}
	if reason := moveTargetReason(g, source, onto); reason != "" {
		return fmt.Errorf("cannot move %s onto %s: %s", source, onto, reason)
	}

	SaveUndoPoint(c, "move", source)

	// The repoint. ParentBranchRevision is deliberately NOT updated: it records
	// the commit this branch was cut from, and that is exactly the cut point
	// the restack below needs to work out which commits are the branch's own.
	// Overwriting it here with the new parent's tip would strand those commits.
	b := g.Branches[source]
	if oldParent := g.Branches[b.ParentBranchName]; oldParent != nil {
		oldParent.Children = removeFromSlice(oldParent.Children, source)
	}
	g.Branches[onto].Children = append(g.Branches[onto].Children, source)
	b.ParentBranchName = onto

	if err := c.Store.WriteGraph(g); err != nil {
		return err
	}
	if !c.Quiet {
		fmt.Printf("Moved %s onto %s\n", source, onto)
	}

	if opts.NoRestack {
		if !c.Quiet {
			fmt.Printf("Skipped restack — run `sr restack` to replay %s and its upstack\n", source)
		}
		return nil
	}

	// Normal restack mechanics take hold from here: the frozen wall, conflict
	// handling, and reporting all come from Restack rather than being
	// duplicated. Naming source as the subject exempts it from the frozen wall,
	// which is harmless because ValidateMoveSource already rejected a frozen
	// source outright.
	return Restack(c, RestackOpts{Branch: source, Upstack: true})
}

// ValidateMoveSource reports whether a branch may be the subject of a move.
//
// Freezing pins a branch's position, and a move's first act is to repoint it.
// Unlike `sr restack --branch <frozen>`, naming the branch explicitly is not an
// exemption here: the exemption covers a branch's commits, not its place in the
// graph (ADR-0015).
func ValidateMoveSource(g *graph.Graph, source string) error {
	b := g.Branches[source]
	if b == nil {
		return fmt.Errorf("branch %q not tracked", source)
	}
	if b.IsTrunk {
		return fmt.Errorf("cannot move trunk")
	}
	if b.Frozen {
		return fmt.Errorf("cannot move %s: %w", source, srerr.ErrFrozen)
	}
	return nil
}

// MoveTargets returns every tracked branch as a candidate parent for source,
// each annotated with the reason it cannot be chosen.
//
// Ineligible branches are returned rather than filtered out: the picker renders
// them greyed with their reason, so a branch a user expected to see is never
// simply absent. Callers that want only the legal targets should filter on an
// empty Reason.
func MoveTargets(g *graph.Graph, source string) []MoveTarget {
	targets := make([]MoveTarget, 0, len(g.Branches))
	for name := range g.Branches {
		targets = append(targets, MoveTarget{
			Branch: name,
			Reason: moveTargetReason(g, source, name),
		})
	}
	return targets
}

// SelectableMoveTargets returns only the branches source may legally move onto.
func SelectableMoveTargets(g *graph.Graph, source string) []string {
	var out []string
	for _, t := range MoveTargets(g, source) {
		if t.Reason == "" {
			out = append(out, t.Branch)
		}
	}
	return out
}

// moveTargetReason reports why source may not be moved onto candidate,
// returning the empty string when the move is legal.
//
// This is the single predicate behind both the --onto validation and the
// picker's greyed-out rows, so the menu can never offer something Move would
// then reject.
//
// The reason text is shown verbatim to the user, next to the branch in the
// picker, so it should read as a phrase completing "can't pick this because…".
//
// TODO(you): implement. See the discussion below for the four cases.
func moveTargetReason(g *graph.Graph, source, candidate string) string {
	return ""
}

func removeFromSlice(s []string, val string) []string {
	result := make([]string, 0, len(s))
	for _, v := range s {
		if v != val {
			result = append(result, v)
		}
	}
	return result
}
