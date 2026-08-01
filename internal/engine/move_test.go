package engine

import (
	"sort"
	"strings"
	"testing"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/graph"
)

// newMoveStack builds trunk -> a -> b -> c, each branch adding one distinctly
// named file so a rebase that replays the wrong commits is visible in the
// resulting file list rather than only in revision hashes.
func newMoveStack(t *testing.T) (*context.Context, string) {
	t.Helper()
	c, trunk := newBaseRepo(t)
	for _, n := range []string{"a", "b", "c"} {
		if err := Create(c, CreateOpts{Name: n}); err != nil {
			t.Fatalf("create %s: %v", n, err)
		}
		commitFile(t, c, n+".txt", "from "+n, n+": work")
		syncTip(t, c, n)
	}
	return c, trunk
}

// filesOnBranch lists the files introduced by branch relative to base.
func filesOnBranch(t *testing.T, c *context.Context, branch, base string) []string {
	t.Helper()
	out, err := c.Git.RunGitCapture("log", base+".."+branch, "--name-only", "--format=")
	if err != nil {
		t.Fatalf("log %s..%s: %v", base, branch, err)
	}
	seen := map[string]bool{}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		files = append(files, line)
	}
	sort.Strings(files)
	return files
}

// testGraph builds trunk -> a -> b -> c plus an unrelated stack trunk -> x.
func testGraph() *graph.Graph {
	g := graph.New()
	g.AddTrunk("main", "r0")
	g.AddBranch("a", "main", "r0", "ra")
	g.AddBranch("b", "a", "ra", "rb")
	g.AddBranch("c", "b", "rb", "rc")
	g.AddBranch("x", "main", "r0", "rx")
	return g
}

func TestMoveTargetReason(t *testing.T) {
	g := testGraph()

	tests := []struct {
		name      string
		source    string
		candidate string
		want      string
	}{
		{"self is not a cycle message", "b", "b", "can't move a branch onto itself"},
		{"direct child is a cycle", "b", "c", "would create a cycle — depends on b"},
		{"grandchild is a cycle", "a", "c", "would create a cycle — depends on a"},
		{"current parent is a no-op", "b", "a", "already the parent"},
		{"trunk is legal when not the parent", "b", "main", ""},
		{"trunk is the parent for a bottom branch", "a", "main", "already the parent"},
		{"another stack is legal", "b", "x", ""},
		{"an ancestor that is not the parent is legal", "c", "a", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := moveTargetReason(g, tc.source, tc.candidate); got != tc.want {
				t.Errorf("moveTargetReason(%q, %q) = %q, want %q",
					tc.source, tc.candidate, got, tc.want)
			}
		})
	}
}

func TestSelectableMoveTargets(t *testing.T) {
	g := testGraph()
	got := SelectableMoveTargets(g, "b")
	sort.Strings(got)
	if strings.Join(got, ",") != "main,x" {
		t.Errorf("selectable for b = %v, want [main x]", got)
	}
}

// The bottom branch of a repo's only stack has nowhere legal to go: trunk is
// already its parent and everything else descends from it. The picker relies on
// this being detectable up front so it can print a sentence instead of opening
// a menu that cannot be answered.
func TestSelectableMoveTargetsEmptyForBottomOfOnlyStack(t *testing.T) {
	g := graph.New()
	g.AddTrunk("main", "r0")
	g.AddBranch("a", "main", "r0", "ra")
	g.AddBranch("b", "a", "ra", "rb")

	if got := SelectableMoveTargets(g, "a"); len(got) != 0 {
		t.Errorf("selectable for bottom branch = %v, want none", got)
	}
}

func TestMoveRejectsCycleTarget(t *testing.T) {
	c, _ := newMoveStack(t)
	err := Move(c, MoveOpts{Source: "a", Onto: "c"})
	if err == nil {
		t.Fatal("moving a onto its own descendant succeeded; this makes the graph cyclic and Downstack then never terminates")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error = %q, want it to mention a cycle", err)
	}
}

func TestMoveRejectsFrozenSource(t *testing.T) {
	c, _ := newMoveStack(t)
	g, _ := c.Store.ReadGraph()
	g.Branches["b"].Frozen = true
	if err := c.Store.WriteGraph(g); err != nil {
		t.Fatalf("write graph: %v", err)
	}

	err := Move(c, MoveOpts{Source: "b", Onto: "main"})
	if err == nil {
		t.Fatal("moved a frozen branch; freezing pins a branch's position (ADR-0015)")
	}
	if !strings.Contains(err.Error(), "frozen") {
		t.Errorf("error = %q, want it to mention frozen", err)
	}
}

func TestMoveRejectsTrunkSource(t *testing.T) {
	c, trunk := newMoveStack(t)
	if err := Move(c, MoveOpts{Source: trunk, Onto: "a"}); err == nil {
		t.Fatal("moved trunk")
	}
}

// The core behaviour: moving c from b to a must replay only c's own commit.
// The previous implementation rebased c but left its recorded parent revision
// and its upstack inconsistent; this asserts the resulting branch content.
func TestMoveReplaysOnlyTheBranchesOwnCommits(t *testing.T) {
	c, trunk := newMoveStack(t)

	if err := Move(c, MoveOpts{Source: "c", Onto: "a"}); err != nil {
		t.Fatalf("Move: %v", err)
	}

	g, _ := c.Store.ReadGraph()
	if got := g.Branches["c"].ParentBranchName; got != "a" {
		t.Errorf("c's parent = %q, want a", got)
	}
	if childrenContain(g, "b", "c") {
		t.Error("c is still listed as a child of b")
	}
	if !childrenContain(g, "a", "c") {
		t.Error("c is not listed as a child of a")
	}

	files := filesOnBranch(t, c, "c", trunk)
	want := []string{"a.txt", "c.txt"}
	if strings.Join(files, ",") != strings.Join(want, ",") {
		t.Errorf("c contains %v, want %v\nb.txt appearing here means the move replayed b's commits onto c", files, want)
	}
}

// A move takes the source's upstack with it. Moving b onto trunk must carry c
// along and leave neither of them holding a's commit.
func TestMoveCarriesUpstack(t *testing.T) {
	c, trunk := newMoveStack(t)

	if err := Move(c, MoveOpts{Source: "b", Onto: trunk}); err != nil {
		t.Fatalf("Move: %v", err)
	}

	g, _ := c.Store.ReadGraph()
	if got := g.Branches["c"].ParentBranchName; got != "b" {
		t.Errorf("c's parent = %q, want b — the upstack should travel with the source", got)
	}

	if files := filesOnBranch(t, c, "b", trunk); strings.Join(files, ",") != "b.txt" {
		t.Errorf("b contains %v, want [b.txt]", files)
	}
	files := filesOnBranch(t, c, "c", trunk)
	want := []string{"b.txt", "c.txt"}
	if strings.Join(files, ",") != strings.Join(want, ",") {
		t.Errorf("c contains %v, want %v\na.txt appearing here means c was not replayed onto b's new position", files, want)
	}
}

// The recorded parent revision is the cut point a later restack uses to work
// out which commits belong to the branch. The repoint must not overwrite it.
func TestMoveNoRestackKeepsCutPoint(t *testing.T) {
	c, _ := newMoveStack(t)
	before, _ := c.Store.ReadGraph()
	cutPoint := before.Branches["c"].ParentBranchRevision

	if err := Move(c, MoveOpts{Source: "c", Onto: "a", NoRestack: true}); err != nil {
		t.Fatalf("Move: %v", err)
	}

	g, _ := c.Store.ReadGraph()
	if got := g.Branches["c"].ParentBranchName; got != "a" {
		t.Errorf("c's parent = %q, want a", got)
	}
	if got := g.Branches["c"].ParentBranchRevision; got != cutPoint {
		t.Errorf("ParentBranchRevision = %q, want the old parent's revision %q\n"+
			"overwriting the cut point leaves a later restack unable to tell which commits are c's own", got, cutPoint)
	}
}

func childrenContain(g *graph.Graph, parent, child string) bool {
	for _, ch := range g.ChildrenOf(parent) {
		if ch == child {
			return true
		}
	}
	return false
}
