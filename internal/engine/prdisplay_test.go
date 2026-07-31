package engine

import (
	"testing"

	"github.com/amustafa/stackr/internal/graph"
	"github.com/amustafa/stackr/internal/store"
)

// prInfoFor builds PR metadata from branch -> [prNumber, stackNumber] pairs.
func prInfoFor(pairs map[string][2]int) *store.PRInfo {
	info := &store.PRInfo{Branches: map[string]*store.BranchPR{}}
	for name, v := range pairs {
		info.Branches[name] = &store.BranchPR{
			Number:      v[0],
			StackNumber: v[1],
			State:       "open",
		}
	}
	return info
}

func TestBranchAnnotation_StackMarkerOnlyAtStackBottom(t *testing.T) {
	g := buildGraph(t, "main", map[string][]string{
		"main": {"a"},
		"a":    {"b"},
		"b":    {"c"},
	})
	info := prInfoFor(map[string][2]int{
		"a": {42, 7},
		"b": {43, 7},
		"c": {44, 7},
	})

	tests := map[string]string{
		"a": "#42 [stack #7, 3 PRs]",
		"b": "#43",
		"c": "#44",
	}
	for branch, want := range tests {
		if got := BranchAnnotation(g, info, branch); got != want {
			t.Errorf("BranchAnnotation(%s) = %q, want %q", branch, got, want)
		}
	}
}

// At a fork, the lower stack ends and each child begins a new one — so each
// child must carry its own stack marker.
func TestBranchAnnotation_ForkedStacksEachGetAMarker(t *testing.T) {
	g := buildGraph(t, "main", map[string][]string{
		"main": {"a"},
		"a":    {"b"},
		"b":    {"c", "d"},
		"c":    {"c2"},
		"d":    {"d2"},
	})
	info := prInfoFor(map[string][2]int{
		"a": {42, 7}, "b": {43, 7},
		"c": {44, 8}, "c2": {45, 8},
		"d": {46, 9}, "d2": {47, 9},
	})

	tests := map[string]string{
		"a":  "#42 [stack #7, 2 PRs]",
		"b":  "#43",
		"c":  "#44 [stack #8, 2 PRs]",
		"c2": "#45",
		"d":  "#46 [stack #9, 2 PRs]",
		"d2": "#47",
	}
	for branch, want := range tests {
		if got := BranchAnnotation(g, info, branch); got != want {
			t.Errorf("BranchAnnotation(%s) = %q, want %q", branch, got, want)
		}
	}
}

func TestBranchAnnotation_UnstackedPRShowsNumberOnly(t *testing.T) {
	g := buildGraph(t, "main", map[string][]string{"main": {"a"}})
	info := prInfoFor(map[string][2]int{"a": {42, 0}})

	if got, want := BranchAnnotation(g, info, "a"), "#42"; got != want {
		t.Errorf("BranchAnnotation(a) = %q, want %q", got, want)
	}
}

func TestBranchAnnotation_UnsubmittedBranchIsBlank(t *testing.T) {
	g := buildGraph(t, "main", map[string][]string{"main": {"a"}})
	info := &store.PRInfo{Branches: map[string]*store.BranchPR{}}

	if got := BranchAnnotation(g, info, "a"); got != "" {
		t.Errorf("BranchAnnotation(a) = %q, want empty", got)
	}
}

func TestBranchAnnotation_DraftAndMergedStates(t *testing.T) {
	g := buildGraph(t, "main", map[string][]string{"main": {"a"}, "a": {"b"}})
	info := &store.PRInfo{Branches: map[string]*store.BranchPR{
		"a": {Number: 42, State: "merged"},
		"b": {Number: 43, State: "open", Draft: true},
	}}

	if got, want := BranchAnnotation(g, info, "a"), "#42 (merged)"; got != want {
		t.Errorf("BranchAnnotation(a) = %q, want %q", got, want)
	}
	if got, want := BranchAnnotation(g, info, "b"), "#43 (draft)"; got != want {
		t.Errorf("BranchAnnotation(b) = %q, want %q", got, want)
	}
}

func TestStackMembers_OrderedBottomToTop(t *testing.T) {
	g := buildGraph(t, "main", map[string][]string{
		"main": {"a"}, "a": {"b"}, "b": {"c"},
	})
	info := prInfoFor(map[string][2]int{
		"a": {42, 7}, "b": {43, 7}, "c": {44, 7},
	})

	members := StackMembers(g, info, 7)
	want := []string{"a", "b", "c"}
	if len(members) != len(want) {
		t.Fatalf("StackMembers = %v, want %v", members, want)
	}
	for i := range want {
		if members[i] != want[i] {
			t.Fatalf("StackMembers = %v, want %v", members, want)
		}
	}
}

func TestStackPosition(t *testing.T) {
	g := buildGraph(t, "main", map[string][]string{
		"main": {"a"}, "a": {"b"}, "b": {"c"},
	})
	info := prInfoFor(map[string][2]int{
		"a": {42, 7}, "b": {43, 7}, "c": {44, 7},
	})

	if pos, size := StackPosition(g, info, "b"); pos != 2 || size != 3 {
		t.Errorf("StackPosition(b) = %d of %d, want 2 of 3", pos, size)
	}
	if pos, size := StackPosition(g, info, "main"); pos != 0 || size != 0 {
		t.Errorf("StackPosition(main) = %d of %d, want 0 of 0", pos, size)
	}
}

// Renders the real tree the user sees, so the annotation wiring is covered
// end to end rather than only the string helper.
func TestRenderTree_ShowsPRsAndStacks(t *testing.T) {
	g := buildGraph(t, "main", map[string][]string{
		"main": {"a"}, "a": {"b"}, "b": {"c"},
	})
	info := prInfoFor(map[string][2]int{
		"a": {42, 7}, "b": {43, 7}, "c": {44, 7},
	})

	out := g.RenderTree(graph.RenderOpts{
		CurrentBranch: "c",
		AnnotateFn:    func(branch string) string { return BranchAnnotation(g, info, branch) },
	})

	want := "◉ c #44 ←\n│\n◯ b #43\n│\n◯ a #42 [stack #7, 3 PRs]\n│\n◯ main (trunk)\n"
	if out != want {
		t.Errorf("RenderTree =\n%s\nwant\n%s", out, want)
	}
}
