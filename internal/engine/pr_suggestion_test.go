package engine

import (
	"testing"

	"github.com/amustafa/stackr/internal/graph"
)

func TestParsePRSuggestion(t *testing.T) {
	title, body := parsePRSuggestion("Add JWT auth\n\nUses stateless tokens.\nNo DB sessions.")
	if title != "Add JWT auth" {
		t.Errorf("title = %q", title)
	}
	if body != "Uses stateless tokens.\nNo DB sessions." {
		t.Errorf("body = %q", body)
	}
	// Title-only.
	title, body = parsePRSuggestion("  Just a title  ")
	if title != "Just a title" || body != "" {
		t.Errorf("title-only: %q / %q", title, body)
	}
	// Empty.
	if tl, bd := parsePRSuggestion("   "); tl != "" || bd != "" {
		t.Errorf("empty should yield empty: %q / %q", tl, bd)
	}
}

func TestLookupPRSuggestion(t *testing.T) {
	b := &graph.BranchState{Context: []graph.BranchContext{
		{Key: "approach", Text: "stateless"},
		{Key: "pr", Text: "My PR\n\nbody here"},
	}}
	title, body, ok := lookupPRSuggestion(b)
	if !ok || title != "My PR" || body != "body here" {
		t.Fatalf("lookup: ok=%v title=%q body=%q", ok, title, body)
	}
	// No pr entry.
	if _, _, ok := lookupPRSuggestion(&graph.BranchState{}); ok {
		t.Error("should not find pr entry when absent")
	}
	// Nil branch.
	if _, _, ok := lookupPRSuggestion(nil); ok {
		t.Error("nil branch should be ok=false")
	}
}
