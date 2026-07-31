package engine

import (
	"strings"
	"testing"

	"github.com/amustafa/stackr/internal/graph"
)

// buildGraph wires a trunk plus a parent->children map into a Graph.
// Revisions are irrelevant to segmentation, so they are left empty.
func buildGraph(t *testing.T, trunk string, edges map[string][]string) *graph.Graph {
	t.Helper()
	g := graph.New()
	g.AddTrunk(trunk, "")
	// Add in topological order: a branch can only be added once its parent is.
	added := map[string]bool{trunk: true}
	for progress := true; progress; {
		progress = false
		for parent, children := range edges {
			if !added[parent] {
				continue
			}
			for _, child := range children {
				if added[child] {
					continue
				}
				if err := g.AddBranch(child, parent, "", ""); err != nil {
					t.Fatalf("AddBranch(%s -> %s): %v", child, parent, err)
				}
				added[child] = true
				progress = true
			}
		}
	}
	return g
}

func formatSegments(segments [][]string) string {
	var parts []string
	for _, s := range segments {
		parts = append(parts, "["+strings.Join(s, " ")+"]")
	}
	return strings.Join(parts, " ")
}

func TestLinearSegments_LinearStackIsOneSegment(t *testing.T) {
	g := buildGraph(t, "main", map[string][]string{
		"main": {"a"},
		"a":    {"b"},
		"b":    {"c"},
	})

	got := formatSegments(linearSegments(g, []string{"a", "b", "c"}))
	if want := "[a b c]"; got != want {
		t.Errorf("linearSegments = %s, want %s", got, want)
	}
}

// The case from the design discussion: main <- a <- b, with b forking into
// c and d. The fork must cut the run, and each child starts its own segment
// based on b — never one stack containing both c and d.
func TestLinearSegments_ForkCutsSegmentAtForkPoint(t *testing.T) {
	g := buildGraph(t, "main", map[string][]string{
		"main": {"a"},
		"a":    {"b"},
		"b":    {"c", "d"},
	})

	got := formatSegments(linearSegments(g, []string{"a", "b", "c", "d"}))
	if want := "[a b] [c] [d]"; got != want {
		t.Errorf("linearSegments = %s, want %s", got, want)
	}
}

// A fork whose children have children of their own: each side becomes a real
// multi-PR stack rooted at the fork point.
func TestLinearSegments_ForkedBranchesExtendUpward(t *testing.T) {
	g := buildGraph(t, "main", map[string][]string{
		"main": {"a"},
		"a":    {"b"},
		"b":    {"c", "d"},
		"c":    {"c2"},
		"d":    {"d2"},
	})

	got := formatSegments(linearSegments(g, []string{"a", "b", "c", "c2", "d", "d2"}))
	if want := "[a b] [c c2] [d d2]"; got != want {
		t.Errorf("linearSegments = %s, want %s", got, want)
	}
}

// Only part of the stack was submitted, so the run starts where the submitted
// set starts — its base is simply whatever the parent branch already is.
func TestLinearSegments_PartialSubmitStartsAtSubmittedRoot(t *testing.T) {
	g := buildGraph(t, "main", map[string][]string{
		"main": {"a"},
		"a":    {"b"},
		"b":    {"c"},
	})

	got := formatSegments(linearSegments(g, []string{"b", "c"}))
	if want := "[b c]"; got != want {
		t.Errorf("linearSegments = %s, want %s", got, want)
	}
}

// A fork is only a fork if both children are actually being submitted.
// Submitting one side leaves a single unambiguous run.
func TestLinearSegments_UnsubmittedSiblingIsNotAFork(t *testing.T) {
	g := buildGraph(t, "main", map[string][]string{
		"main": {"a"},
		"a":    {"b"},
		"b":    {"c", "d"},
	})

	got := formatSegments(linearSegments(g, []string{"a", "b", "c"}))
	if want := "[a b c]"; got != want {
		t.Errorf("linearSegments = %s, want %s", got, want)
	}
}

func TestLinearSegments_TrunkIsExcluded(t *testing.T) {
	g := buildGraph(t, "main", map[string][]string{
		"main": {"a"},
		"a":    {"b"},
	})

	got := formatSegments(linearSegments(g, []string{"main", "a", "b"}))
	if want := "[a b]"; got != want {
		t.Errorf("linearSegments = %s, want %s", got, want)
	}
}

func TestLinearSegments_NoBranchesYieldsNoSegments(t *testing.T) {
	g := buildGraph(t, "main", map[string][]string{"main": {"a"}})

	if segments := linearSegments(g, nil); len(segments) != 0 {
		t.Errorf("linearSegments = %s, want no segments", formatSegments(segments))
	}
}

func openStack(prs ...int) *GHStack {
	s := &GHStack{Number: 7, Open: true}
	for _, n := range prs {
		s.PullRequests = append(s.PullRequests, GHStackPR{Number: n, State: "open"})
	}
	return s
}

// A stack whose PRs have all merged stays queryable with open:false rather than
// 404ing, so it must be treated as finished — otherwise the next submit adds to
// a closed stack instead of starting a new one.
func TestStackNeedsRebuild(t *testing.T) {
	closed := openStack(42, 43)
	closed.Open = false

	emptied := openStack()
	emptied.Open = true

	tests := map[string]struct {
		stack *GHStack
		want  bool
	}{
		"missing stack":     {nil, true},
		"closed stack":      {closed, true},
		"open but emptied":  {emptied, true},
		"open with members": {openStack(42, 43), false},
	}
	for name, tc := range tests {
		if got := stackNeedsRebuild(tc.stack); got != tc.want {
			t.Errorf("%s: stackNeedsRebuild = %v, want %v", name, got, tc.want)
		}
	}
}

func TestClassifyAgainstRemote(t *testing.T) {
	tests := map[string]struct {
		local, remote        []int
		wantBelow, wantAbove []int
		wantFound            bool
	}{
		"nothing new":              {[]int{42, 43}, []int{42, 43}, []int{}, []int{}, true},
		"one to append":            {[]int{42, 43, 44}, []int{42, 43}, []int{}, []int{44}, true},
		"remote lost merged":       {[]int{42, 43, 44}, []int{43}, []int{42}, []int{44}, true},
		"empty remote":             {[]int{42, 43}, nil, []int{}, []int{42, 43}, true},
		"diverged":                 {[]int{42, 43}, []int{99}, nil, nil, false},
		"new PR below (#26)":       {[]int{26, 22, 23}, []int{22, 23}, []int{26}, []int{}, true},
		"below and above":          {[]int{26, 22, 23, 44}, []int{22, 23}, []int{26}, []int{44}, true},
		"remote longer than local": {[]int{42}, []int{41, 42}, nil, nil, false},
	}
	for name, tc := range tests {
		gotBelow, gotAbove, gotFound := classifyAgainstRemote(tc.local, tc.remote)
		if gotFound != tc.wantFound {
			t.Errorf("%s: found = %v, want %v", name, gotFound, tc.wantFound)
			continue
		}
		checkInts := func(label string, got, want []int) {
			if len(got) != len(want) {
				t.Errorf("%s: %s = %v, want %v", name, label, got, want)
				return
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("%s: %s = %v, want %v", name, label, got, want)
					return
				}
			}
		}
		checkInts("newBelow", gotBelow, tc.wantBelow)
		checkInts("newAbove", gotAbove, tc.wantAbove)
	}
}
