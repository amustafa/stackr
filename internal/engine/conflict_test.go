package engine

import (
	"testing"

	"github.com/amustafa/stackr/internal/store"
)

func TestRemainingBranches(t *testing.T) {
	gs := &store.GetState{
		WalkPath:      []string{"feat-a", "feat-b", "feat-c", "feat-d"},
		CurrentBranch: "feat-b",
	}

	remaining := remainingBranches(gs)
	expected := []string{"feat-c", "feat-d"}
	if len(remaining) != len(expected) {
		t.Fatalf("remainingBranches length = %d, want %d", len(remaining), len(expected))
	}
	for i, name := range expected {
		if remaining[i] != name {
			t.Errorf("remaining[%d] = %q, want %q", i, remaining[i], name)
		}
	}
}

func TestRemainingBranches_LastBranch(t *testing.T) {
	gs := &store.GetState{
		WalkPath:      []string{"feat-a", "feat-b"},
		CurrentBranch: "feat-b",
	}

	remaining := remainingBranches(gs)
	if len(remaining) != 0 {
		t.Errorf("expected empty remaining, got %v", remaining)
	}
}

func TestRemainingBranches_NotFound(t *testing.T) {
	gs := &store.GetState{
		WalkPath:      []string{"feat-a", "feat-b"},
		CurrentBranch: "feat-x",
	}

	remaining := remainingBranches(gs)
	if remaining != nil {
		t.Errorf("expected nil for not-found branch, got %v", remaining)
	}
}

func TestContinue_NoState(t *testing.T) {
	c, _ := setupGetTestEnv(t)

	err := Continue(c)
	if err == nil {
		t.Fatal("expected error when no state exists")
	}
}

func TestAbort_GetState(t *testing.T) {
	c, _ := setupGetTestEnv(t)

	c.Store.WriteGetState(&store.GetState{
		Operation:     "get",
		OrigBranch:    "main",
		Target:        "feat-a",
		WalkPath:      []string{"feat-a"},
		CurrentBranch: "feat-a",
	})

	if !c.Store.HasGetState() {
		t.Fatal("expected get state to exist")
	}

	err := Abort(c)
	if err != nil {
		t.Fatalf("Abort: %v", err)
	}

	if c.Store.HasGetState() {
		t.Fatal("expected get state to be cleared after abort")
	}

	current, _ := c.Git.CurrentBranch()
	if current != "main" {
		t.Errorf("expected to return to main, got %q", current)
	}
}

func TestAbort_RebaseState(t *testing.T) {
	c, _ := setupGetTestEnv(t)

	c.Store.WriteRebaseState(&store.RebaseState{
		Operation:     "restack",
		OrigBranch:    "main",
		CurrentBranch: "main",
	})

	err := Abort(c)
	if err != nil {
		t.Fatalf("Abort rebase: %v", err)
	}

	if c.Store.HasRebaseState() {
		t.Fatal("expected rebase state to be cleared after abort")
	}
}

func TestAbort_NoState(t *testing.T) {
	c, _ := setupGetTestEnv(t)

	err := Abort(c)
	if err == nil {
		t.Fatal("expected error when no state exists")
	}
}
