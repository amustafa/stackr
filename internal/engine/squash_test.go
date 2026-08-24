package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/git"
)

// setupSquashStack builds trunk -> a -> b -> c, two commits per branch, all
// correctly stacked and recorded in the graph.
func setupSquashStack(t *testing.T) *context.Context {
	t.Helper()
	c, _ := setupGetTestEnv(t)

	for _, name := range []string{"a", "b", "c"} {
		if err := Create(c, CreateOpts{Name: name}); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		squashCommit(t, c.Git, name+"1.txt", name+": first")
		squashCommit(t, c.Git, name+"2.txt", name+": second")

		g, _ := c.Store.ReadGraph()
		rev, _ := c.Git.RevParse(name)
		g.Branches[name].BranchRevision = rev
		if err := c.Store.WriteGraph(g); err != nil {
			t.Fatalf("write graph: %v", err)
		}
	}
	return c
}

func squashCommit(t *testing.T, r *git.Runner, file, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.Dir, file), []byte(msg+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.RunGitCapture("add", file); err != nil {
		t.Fatal(err)
	}
	if err := r.RunGit("commit", "-m", msg); err != nil {
		t.Fatal(err)
	}
}

// commitCount returns the number of commits on branch that are not on parent.
func commitCount(t *testing.T, r *git.Runner, parent, branch string) string {
	t.Helper()
	n, err := r.RunGitCapture("rev-list", "--count", parent+".."+branch)
	if err != nil {
		t.Fatalf("rev-list %s..%s: %v", parent, branch, err)
	}
	return n
}

func sitsOnTip(r *git.Runner, parent, branch string) bool {
	_, err := r.RunGitCapture("merge-base", "--is-ancestor", parent, branch)
	return err == nil
}

func TestSquash_DefaultLeavesDescendantsAlone(t *testing.T) {
	c := setupSquashStack(t)
	if err := c.Git.Checkout("a"); err != nil {
		t.Fatal(err)
	}

	if err := Squash(c, SquashOpts{}); err != nil {
		t.Fatalf("Squash: %v", err)
	}

	if n := commitCount(t, c.Git, "main", "a"); n != "1" {
		t.Fatalf("a has %s commits after squash, want 1", n)
	}
	// Descendants must be untouched: b still builds on the pre-squash commits,
	// so it does NOT sit on the squashed a's tip.
	if sitsOnTip(c.Git, "a", "b") {
		t.Fatal("b was restacked without --restack")
	}
}

func TestSquash_RestackFlagRestacksDescendants(t *testing.T) {
	c := setupSquashStack(t)
	if err := c.Git.Checkout("a"); err != nil {
		t.Fatal(err)
	}

	if err := Squash(c, SquashOpts{Restack: true}); err != nil {
		t.Fatalf("Squash --restack: %v", err)
	}

	if !sitsOnTip(c.Git, "a", "b") || !sitsOnTip(c.Git, "b", "c") {
		t.Fatal("descendants not restacked onto the squashed branch")
	}
	// Restack replays, not squashes: b and c keep their two commits.
	if n := commitCount(t, c.Git, "a", "b"); n != "2" {
		t.Fatalf("b has %s commits, want 2 (restack must not squash)", n)
	}
}

func TestSquashStack_SquashesWholeStackBottomUp(t *testing.T) {
	c := setupSquashStack(t)
	// Start mid-stack: the sweep must still cover the whole stack.
	if err := c.Git.Checkout("b"); err != nil {
		t.Fatal(err)
	}

	if err := Squash(c, SquashOpts{Stack: true}); err != nil {
		t.Fatalf("Squash --stack: %v", err)
	}

	for _, pair := range [][2]string{{"main", "a"}, {"a", "b"}, {"b", "c"}} {
		parent, branch := pair[0], pair[1]
		if !sitsOnTip(c.Git, parent, branch) {
			t.Fatalf("%s does not sit on %s's tip after sweep", branch, parent)
		}
		if n := commitCount(t, c.Git, parent, branch); n != "1" {
			t.Fatalf("%s has %s commits after sweep, want 1", branch, n)
		}
	}

	// Nothing was lost or reverted: c's tree holds every file from every branch.
	out, err := c.Git.RunGitCapture("ls-tree", "--name-only", "c")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"a1.txt", "a2.txt", "b1.txt", "b2.txt", "c1.txt", "c2.txt"} {
		if !strings.Contains(out, f) {
			t.Fatalf("%s missing from c's tree after sweep:\n%s", f, out)
		}
	}

	// The sweep returns to where the user started.
	if cur, _ := c.Git.CurrentBranch(); cur != "b" {
		t.Fatalf("ended on %s, want b", cur)
	}
}

func TestSquashStack_RerunLeavesEverythingUntouched(t *testing.T) {
	c := setupSquashStack(t)
	if err := c.Git.Checkout("b"); err != nil {
		t.Fatal(err)
	}
	if err := Squash(c, SquashOpts{Stack: true}); err != nil {
		t.Fatalf("first sweep: %v", err)
	}

	before := map[string]string{}
	for _, name := range []string{"a", "b", "c"} {
		before[name], _ = c.Git.RevParse(name)
	}

	// Single-commit branches are skipped, so a rerun must not rewrite anything
	// (a second squash would clobber messages and churn SHAs for nothing).
	if err := Squash(c, SquashOpts{Stack: true}); err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	for _, name := range []string{"a", "b", "c"} {
		if now, _ := c.Git.RevParse(name); now != before[name] {
			t.Fatalf("%s rewritten by an idempotent rerun: %s -> %s", name, before[name], now)
		}
	}
}

func TestSquashStack_SkipsFrozenBranch(t *testing.T) {
	c := setupSquashStack(t)
	g, _ := c.Store.ReadGraph()
	g.Branches["b"].Frozen = true
	if err := c.Store.WriteGraph(g); err != nil {
		t.Fatal(err)
	}
	if err := c.Git.Checkout("a"); err != nil {
		t.Fatal(err)
	}

	if err := Squash(c, SquashOpts{Stack: true}); err != nil {
		t.Fatalf("Squash --stack: %v", err)
	}

	if n := commitCount(t, c.Git, "a", "b"); n == "1" {
		t.Fatal("frozen b was squashed — the sweep must leave frozen branches alone")
	}
	// a still squashes; c restacks onto the (unmoved) frozen b and squashes.
	if n := commitCount(t, c.Git, "main", "a"); n != "1" {
		t.Fatalf("a has %s commits, want 1", n)
	}
	if n := commitCount(t, c.Git, "b", "c"); n != "1" {
		t.Fatalf("c has %s commits, want 1", n)
	}
	if !sitsOnTip(c.Git, "b", "c") {
		t.Fatal("c does not sit on frozen b's tip")
	}
}
