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
