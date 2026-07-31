package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/amustafa/stackr/internal/graph"
	"github.com/amustafa/stackr/internal/store"
)

// StackMembers returns the branches belonging to GitHub stack `stackNumber`,
// ordered bottom to top (nearest trunk first) — the same order GitHub itself
// uses for a stack's pull requests.
//
// Membership is read from local PR metadata rather than the API so that `sr
// info` and `sr log` stay fast and work offline. It can therefore lag GitHub
// if a stack was regrouped remotely; `sr submit` is what reconciles the two.
func StackMembers(g *graph.Graph, prInfo *store.PRInfo, stackNumber int) []string {
	if stackNumber == 0 || prInfo == nil {
		return nil
	}

	var members []string
	for name, pr := range prInfo.Branches {
		if pr != nil && pr.StackNumber == stackNumber && g.Has(name) {
			members = append(members, name)
		}
	}

	// Distance from trunk is the stack position: a branch's downstack walk is
	// longer the further up the stack it sits.
	sort.Slice(members, func(i, j int) bool {
		di, dj := len(g.Downstack(members[i])), len(g.Downstack(members[j]))
		if di != dj {
			return di < dj
		}
		return members[i] < members[j]
	})
	return members
}

// StackPosition returns the 1-based position of a branch within its GitHub
// stack and the stack's size. Returns 0, 0 when the branch is not in a stack.
func StackPosition(g *graph.Graph, prInfo *store.PRInfo, branch string) (pos, size int) {
	pr := prInfo.Branches[branch]
	if pr == nil || pr.StackNumber == 0 {
		return 0, 0
	}
	members := StackMembers(g, prInfo, pr.StackNumber)
	for i, name := range members {
		if name == branch {
			return i + 1, len(members)
		}
	}
	return 0, len(members)
}

// BranchAnnotation formats the PR suffix for one branch in `sr log`:
//
//	◯ feat-a #42 [stack #7]
//	◯ feat-b #43
//	◉ feat-c #44 (draft) ←
//
// The stack marker is printed only where a stack begins — that is, where the
// branch's parent belongs to a different stack or none at all. Repeating it on
// every member would trade a lot of noise for no extra information, while
// showing it at each start makes a fork legible: a stack that ends at the fork
// and fresh stacks above it each get their own marker.
func BranchAnnotation(g *graph.Graph, prInfo *store.PRInfo, branch string) string {
	if prInfo == nil {
		return ""
	}
	pr := prInfo.Branches[branch]
	if pr == nil || pr.Number == 0 {
		return ""
	}

	parts := []string{fmt.Sprintf("#%d", pr.Number)}

	switch {
	case pr.State == "merged" || pr.State == "MERGED":
		parts = append(parts, "(merged)")
	case pr.State == "closed" || pr.State == "CLOSED":
		parts = append(parts, "(closed)")
	case pr.Draft:
		parts = append(parts, "(draft)")
	}

	if pr.StackNumber != 0 && startsStack(g, prInfo, branch) {
		_, size := StackPosition(g, prInfo, branch)
		parts = append(parts, fmt.Sprintf("[stack #%d, %d PRs]", pr.StackNumber, size))
	}

	return strings.Join(parts, " ")
}

// startsStack reports whether branch is the bottom of its GitHub stack.
func startsStack(g *graph.Graph, prInfo *store.PRInfo, branch string) bool {
	b := g.Branches[branch]
	if b == nil {
		return false
	}
	parent := prInfo.Branches[b.ParentBranchName]
	if parent == nil {
		return true
	}
	return parent.StackNumber != prInfo.Branches[branch].StackNumber
}
