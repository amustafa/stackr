package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/amustafa/stackr/internal/graph"
	"github.com/amustafa/stackr/internal/store"
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

// mapSegment reads PR numbers and bases out of the local record, and nothing
// else. The stack a PR belongs to comes from GitHub, so a disagreeing (here,
// entirely absent) StackNumber must not change what this produces.
func TestMapSegment_ReadsNumbersAndBasesOnly(t *testing.T) {
	prInfo := &store.PRInfo{Branches: map[string]*store.BranchPR{
		"a": {Number: 42, BaseBranch: "main", StackNumber: 0},
		"b": {Number: 43, BaseBranch: "a", StackNumber: 7},
		"c": {Number: 44, BaseBranch: "b", StackNumber: 9},
	}}

	seg := mapSegment(prInfo, []string{"a", "b", "c"})

	// Order matters: ghCreateStack validates the chain bottom-to-top, so a
	// reordered or mismapped PR list fails at GitHub rather than here.
	wantInts(t, "prNumbers", seg.prNumbers, []int{42, 43, 44})

	for pr, want := range map[int]string{42: "main", 43: "a", 44: "b"} {
		if got := seg.baseByPR[pr]; got != want {
			t.Errorf("baseByPR[%d] = %q, want %q", pr, got, want)
		}
	}
}

// wantInts compares an int slice element by element, so a test cannot pass on
// length alone while mapping the wrong values.
func wantInts(t *testing.T, label string, got, want []int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s = %v, want %v", label, got, want)
		}
	}
}

func TestMapSegment_SkipsBranchesWithoutAPR(t *testing.T) {
	prInfo := &store.PRInfo{Branches: map[string]*store.BranchPR{
		"a": {Number: 42, StackNumber: 7},
		"b": {Number: 0}, // pushed, no PR yet
		"c": {Number: 44, StackNumber: 7},
	}}

	seg := mapSegment(prInfo, []string{"a", "b", "c"})

	if len(seg.branches) != 2 || seg.branches[0] != "a" || seg.branches[1] != "c" {
		t.Fatalf("branches = %v, want only those with a PR", seg.branches)
	}
	// The PR list must stay aligned with the branch list, or the stack is
	// registered with the wrong members.
	wantInts(t, "prNumbers", seg.prNumbers, []int{42, 44})
}

// A stack that has already gone away is the state unstacking was trying to
// reach, so rebuildStack must not treat it as a failure.
func TestIsMissingStack(t *testing.T) {
	cases := map[string]bool{
		"gh api POST repos/x/y/stacks/7/unstack failed: HTTP 404: Not Found": true,
		"gh api ... failed: 404":                            true,
		"gh api ... failed: HTTP 422: Unprocessable Entity": false,
		"gh api ... timed out after 30s":                    false,
	}
	for msg, want := range cases {
		if got := isMissingStack(errors.New(msg)); got != want {
			t.Errorf("isMissingStack(%q) = %v, want %v", msg, got, want)
		}
	}
}

// numberedStack builds an open stack with a specific stack number, so a test can
// tell one group from another.
func numberedStack(number int, prs ...int) GHStack {
	s := GHStack{Number: number, Open: true}
	for _, n := range prs {
		s.PullRequests = append(s.PullRequests, GHStackPR{Number: n, State: "open"})
	}
	return s
}

// The listing is repository-wide, so most of what it returns has nothing to do
// with the segment being submitted. Reconciling against an unrelated stack would
// dissolve someone else's grouping.
func TestStacksContaining_IgnoresUnrelatedStacksAndSortsByNumber(t *testing.T) {
	all := []GHStack{
		numberedStack(9, 44, 45),
		numberedStack(3, 90, 91), // unrelated
		numberedStack(7, 42, 43),
	}

	got := stacksContaining(all, []int{42, 43, 44, 45})

	wantInts(t, "stacksContaining", stackNumbersOf(got), []int{7, 9})
}

// chooseAnchor is where the three shapes the remote can be in are decided. Each
// case names the situation it stands for, because getting the anchor wrong is
// not a visible failure — it silently dissolves a grouping that could have been
// extended, churning the stack number on PRs already under review.
func TestChooseAnchor(t *testing.T) {
	merged := numberedStack(7, 43)
	merged.Open = false

	tests := map[string]struct {
		prNumbers    []int
		groups       []GHStack
		wantAnchor   int // 0 means "no anchor, rebuild"
		wantStart    int
		wantDissolve []int
	}{
		// Nothing to change on GitHub: the recorded number was simply lost.
		"remote already holds the whole segment": {
			prNumbers:    []int{42, 43, 44},
			groups:       []GHStack{numberedStack(7, 42, 43, 44)},
			wantAnchor:   7,
			wantStart:    0,
			wantDissolve: nil,
		},
		// a-b registered, c-d-e new: extend upward.
		"remote holds a lower run": {
			prNumbers:    []int{42, 43, 44, 45, 46},
			groups:       []GHStack{numberedStack(7, 42, 43)},
			wantAnchor:   7,
			wantStart:    0,
			wantDissolve: nil,
		},
		// a-b and d-e registered separately: the lower run anchors, the upper one
		// is dissolved so c, d and e can be appended to it.
		"two rival groups: the lower one anchors": {
			prNumbers:    []int{42, 43, 44, 45, 46},
			groups:       []GHStack{numberedStack(7, 42, 43), numberedStack(9, 45, 46)},
			wantAnchor:   7,
			wantStart:    0,
			wantDissolve: []int{9},
		},
		// GitHub drops merged PRs, so the live group can start above our bottom.
		"anchor starts above the segment bottom": {
			prNumbers:    []int{42, 43, 44},
			groups:       []GHStack{numberedStack(9, 43, 44)},
			wantAnchor:   9,
			wantStart:    1,
			wantDissolve: nil,
		},
		// A closed stack keeps its members but cannot be added to.
		"closed stack cannot anchor": {
			prNumbers:    []int{42, 43, 44},
			groups:       []GHStack{merged},
			wantAnchor:   0,
			wantDissolve: []int{7},
		},
		// The group's members are not a run of ours at all — genuinely diverged.
		"non-contiguous group cannot anchor": {
			prNumbers:    []int{42, 43, 44},
			groups:       []GHStack{numberedStack(7, 42, 44)},
			wantAnchor:   0,
			wantDissolve: []int{7},
		},
	}

	for name, tc := range tests {
		anchor, start, dissolve := chooseAnchor(tc.prNumbers, tc.groups)

		gotAnchor := 0
		if anchor != nil {
			gotAnchor = anchor.Number
		}
		if gotAnchor != tc.wantAnchor {
			t.Errorf("%s: anchor = #%d, want #%d", name, gotAnchor, tc.wantAnchor)
			continue
		}
		if anchor != nil && start != tc.wantStart {
			t.Errorf("%s: start = %d, want %d", name, start, tc.wantStart)
		}
		wantInts(t, name+": dissolve", dissolve, tc.wantDissolve)
	}
}

// The listing is read once per submit but a fork produces several segments, so a
// stack dissolved for one must not still look live to the next.
func TestWithoutStacks_DropsDissolvedGroups(t *testing.T) {
	all := []GHStack{numberedStack(7, 42), numberedStack(9, 44), numberedStack(11, 46)}

	got := withoutStacks(all, []int{9})

	wantInts(t, "withoutStacks", stackNumbersOf(got), []int{7, 11})
}
