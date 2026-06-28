package graph

import (
	"testing"
)

func TestAddTrunkAndBranch(t *testing.T) {
	g := New()
	g.AddTrunk("main", "abc123")

	if !g.IsTrunk("main") {
		t.Fatal("expected main to be trunk")
	}
	if g.TrunkName() != "main" {
		t.Fatalf("expected trunk name 'main', got %q", g.TrunkName())
	}

	err := g.AddBranch("feat-a", "main", "abc123", "def456")
	if err != nil {
		t.Fatalf("AddBranch failed: %v", err)
	}

	if !g.Has("feat-a") {
		t.Fatal("expected feat-a to exist")
	}
	if g.Parent("feat-a") != "main" {
		t.Fatalf("expected parent 'main', got %q", g.Parent("feat-a"))
	}

	children := g.ChildrenOf("main")
	if len(children) != 1 || children[0] != "feat-a" {
		t.Fatalf("expected main children [feat-a], got %v", children)
	}
}

func TestRemoveBranchReparents(t *testing.T) {
	g := New()
	g.AddTrunk("main", "aaa")
	g.AddBranch("feat-a", "main", "aaa", "bbb")
	g.AddBranch("feat-b", "feat-a", "bbb", "ccc")
	g.AddBranch("feat-c", "feat-b", "ccc", "ddd")

	// Remove feat-b — feat-c should be reparented to feat-a.
	err := g.RemoveBranch("feat-b")
	if err != nil {
		t.Fatalf("RemoveBranch failed: %v", err)
	}

	if g.Has("feat-b") {
		t.Fatal("feat-b should be removed")
	}
	if g.Parent("feat-c") != "feat-a" {
		t.Fatalf("expected feat-c parent 'feat-a', got %q", g.Parent("feat-c"))
	}
	children := g.ChildrenOf("feat-a")
	if len(children) != 1 || children[0] != "feat-c" {
		t.Fatalf("expected feat-a children [feat-c], got %v", children)
	}
}

func TestTraversal(t *testing.T) {
	g := New()
	g.AddTrunk("main", "aaa")
	g.AddBranch("feat-a", "main", "aaa", "bbb")
	g.AddBranch("feat-b", "feat-a", "bbb", "ccc")
	g.AddBranch("feat-c", "feat-b", "ccc", "ddd")

	// Upstack from feat-a.
	up := g.Upstack("feat-a")
	if len(up) != 3 {
		t.Fatalf("expected 3 upstack branches, got %d: %v", len(up), up)
	}
	if up[0] != "feat-a" || up[1] != "feat-b" || up[2] != "feat-c" {
		t.Fatalf("unexpected upstack order: %v", up)
	}

	// Downstack from feat-c.
	down := g.Downstack("feat-c")
	if len(down) != 4 {
		t.Fatalf("expected 4 downstack branches, got %d: %v", len(down), down)
	}
	if down[0] != "feat-c" || down[3] != "main" {
		t.Fatalf("unexpected downstack order: %v", down)
	}

	// Top from feat-a.
	if g.Top("feat-a") != "feat-c" {
		t.Fatalf("expected top 'feat-c', got %q", g.Top("feat-a"))
	}

	// Bottom from feat-c.
	if g.Bottom("feat-c") != "feat-a" {
		t.Fatalf("expected bottom 'feat-a', got %q", g.Bottom("feat-c"))
	}

	// Up/Down steps.
	if g.Up("feat-a", 2) != "feat-c" {
		t.Fatalf("expected Up(2) = feat-c, got %q", g.Up("feat-a", 2))
	}
	if g.Down("feat-c", 1) != "feat-b" {
		t.Fatalf("expected Down(1) = feat-b, got %q", g.Down("feat-c", 1))
	}
}

func TestTopoOrder(t *testing.T) {
	g := New()
	g.AddTrunk("main", "aaa")
	g.AddBranch("feat-a", "main", "aaa", "bbb")
	g.AddBranch("feat-b", "feat-a", "bbb", "ccc")

	topo := g.TopoOrder("main")
	if len(topo) != 3 {
		t.Fatalf("expected 3 branches, got %d", len(topo))
	}
	if topo[0] != "main" || topo[1] != "feat-a" || topo[2] != "feat-b" {
		t.Fatalf("unexpected topo order: %v", topo)
	}
}

func TestRenderLinearStack(t *testing.T) {
	g := New()
	g.AddTrunk("main", "aaa")
	g.AddBranch("feat-a", "main", "aaa", "bbb")
	g.AddBranch("feat-b", "feat-a", "bbb", "ccc")

	got := g.RenderTree(RenderOpts{CurrentBranch: "feat-b", ShowAll: true})
	expect := "◉ feat-b ←\n│\n◯ feat-a\n│\n◯ main (trunk)\n"
	if got != expect {
		t.Fatalf("linear stack render mismatch:\ngot:\n%s\nexpect:\n%s", got, expect)
	}
}

func TestRenderBranchingStack(t *testing.T) {
	// Tree:
	//   main (trunk)
	//     └─ feat-a
	//          ├─ feat-b
	//          └─ feat-c  (current)
	g := New()
	g.AddTrunk("main", "aaa")
	g.AddBranch("feat-a", "main", "aaa", "bbb")
	g.AddBranch("feat-b", "feat-a", "bbb", "ccc")
	g.AddBranch("feat-c", "feat-a", "bbb", "ddd")

	got := g.RenderTree(RenderOpts{CurrentBranch: "feat-c", ShowAll: true})
	// Primary child = feat-c (contains current), side branch = feat-b
	expect := "◉ feat-c ←\n│\n│ ◯ feat-b\n├─┘\n◯ feat-a\n│\n◯ main (trunk)\n"
	if got != expect {
		t.Fatalf("branching stack render mismatch:\ngot:\n%s\nexpect:\n%s", got, expect)
	}
}

func TestRenderDeepBranchingStack(t *testing.T) {
	// Tree:
	//   master (trunk)
	//     └─ main
	//          └─ pr
	//               ├─ pr-2
	//               └─ pr-2b  (current)
	g := New()
	g.AddTrunk("master", "aaa")
	g.AddBranch("main", "master", "aaa", "bbb")
	g.AddBranch("pr", "main", "bbb", "ccc")
	g.AddBranch("pr-2", "pr", "ccc", "ddd")
	g.AddBranch("pr-2b", "pr", "ccc", "eee")

	got := g.RenderTree(RenderOpts{CurrentBranch: "pr-2b", ShowAll: true})
	expect := "◉ pr-2b ←\n│\n│ ◯ pr-2\n├─┘\n◯ pr\n│\n◯ main\n│\n◯ master (trunk)\n"
	if got != expect {
		t.Fatalf("deep branching render mismatch:\ngot:\n%s\nexpect:\n%s", got, expect)
	}
}

func TestValidate(t *testing.T) {
	g := New()
	g.AddTrunk("main", "aaa")
	g.AddBranch("feat-a", "main", "aaa", "bbb")

	errs := g.Validate()
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	// Break it: reference nonexistent parent.
	g.Branches["feat-a"].ParentBranchName = "nonexistent"
	errs = g.Validate()
	if len(errs) == 0 {
		t.Fatal("expected validation errors for broken parent ref")
	}
}
