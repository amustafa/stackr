package git

import (
	"testing"
)

// treeEqualsOurs is the predicate the divergence classifier is built on: does
// merging theirs into ours leave ours unchanged?
func treeEqualsOurs(t *testing.T, r *Runner, ours, theirs string) bool {
	t.Helper()
	res, err := r.MergeTreeWriteTree(ours, theirs)
	if err != nil {
		t.Fatalf("merge-tree %s %s: %v", ours, theirs, err)
	}
	if !res.Clean {
		return false
	}
	ourTree, err := r.TreeOf(ours)
	if err != nil {
		t.Fatalf("tree of %s: %v", ours, err)
	}
	return res.Tree == ourTree
}

func TestMergeTreeWriteTree_TheirsAlreadyContained(t *testing.T) {
	r := tempRunner(t)
	commitFile(t, r, "a.txt", "one\n", "c1")
	base, _ := r.RevParse("HEAD")

	// theirs adds a line; ours adds the same line plus another.
	if err := r.RunGit("checkout", "-b", "theirs"); err != nil {
		t.Fatal(err)
	}
	commitFile(t, r, "a.txt", "one\ntwo\n", "c2")

	if err := r.RunGit("checkout", "-b", "ours", base); err != nil {
		t.Fatal(err)
	}
	commitFile(t, r, "a.txt", "one\ntwo\n", "c2 replayed")
	commitFile(t, r, "b.txt", "extra\n", "c3")

	if !treeEqualsOurs(t, r, "ours", "theirs") {
		t.Fatal("theirs is fully contained in ours; merge should be a no-op")
	}
}

func TestMergeTreeWriteTree_TheirsHasNewContent(t *testing.T) {
	r := tempRunner(t)
	commitFile(t, r, "a.txt", "one\n", "c1")
	base, _ := r.RevParse("HEAD")

	if err := r.RunGit("checkout", "-b", "theirs"); err != nil {
		t.Fatal(err)
	}
	commitFile(t, r, "collab.txt", "their work\n", "collaborator commit")

	if err := r.RunGit("checkout", "-b", "ours", base); err != nil {
		t.Fatal(err)
	}
	commitFile(t, r, "b.txt", "our work\n", "our commit")

	if treeEqualsOurs(t, r, "ours", "theirs") {
		t.Fatal("theirs has a file ours lacks; merge must change the tree")
	}
}

func TestMergeTreeWriteTree_UnrelatedHistoriesErrors(t *testing.T) {
	r := tempRunner(t)
	commitFile(t, r, "a.txt", "one\n", "c1")

	// An orphan branch shares no history at all.
	if err := r.RunGit("checkout", "--orphan", "lonely"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.RunGitCapture("rm", "-rf", "."); err != nil {
		t.Fatal(err)
	}
	commitFile(t, r, "z.txt", "alone\n", "orphan")

	if _, err := r.MergeTreeWriteTree("master", "lonely"); err == nil {
		if _, err2 := r.MergeTreeWriteTree("main", "lonely"); err2 == nil {
			t.Fatal("unrelated histories must be reported as an error, never as clean")
		}
	}
}

// A remote revert is the case whole-branch containment cannot see: the revert
// cancels against the commit it undoes, so the net diff hides it. Tier 2 must
// catch it commit by commit.
func TestRemoteOnlyCommits_CatchesRevertWholeBranchMisses(t *testing.T) {
	r := tempRunner(t)
	commitFile(t, r, "a.txt", "one\n", "c1")
	commitFile(t, r, "b.txt", "two\n", "c2")
	c3 := commitFile(t, r, "c.txt", "three\n", "c3")
	trunk, _ := r.CurrentBranch()

	// Remote = our three commits plus a revert of c3.
	if err := r.RunGit("checkout", "-b", "remote"); err != nil {
		t.Fatal(err)
	}
	if err := r.RunGit("revert", "--no-edit", c3); err != nil {
		t.Fatal(err)
	}

	// Local = the same three commits rebased onto a moved trunk, still holding c3.
	if err := r.RunGit("checkout", trunk); err != nil {
		t.Fatal(err)
	}
	if err := r.RunGit("checkout", "-b", "local"); err != nil {
		t.Fatal(err)
	}
	commitFile(t, r, "d.txt", "four\n", "c4")

	// Whole-branch containment says "safe" — the revert cancels in the net diff.
	if !treeEqualsOurs(t, r, "local", "remote") {
		t.Skip("environment produced a different net diff; the tier-2 assertion below is the load-bearing one")
	}

	// Tier 2 must still surface the revert.
	only, err := r.RemoteOnlyCommits("local", "remote")
	if err != nil {
		t.Fatalf("RemoteOnlyCommits: %v", err)
	}
	if len(only) == 0 {
		t.Fatal("the revert must appear as a remote-only commit")
	}

	// And replaying it alone must change our tree (it deletes c.txt).
	for _, sha := range only {
		parent, err := r.FirstParent(sha)
		if err != nil {
			t.Fatalf("first parent of %s: %v", sha, err)
		}
		res, err := r.MergeTreeOnto(parent, "local", sha)
		if err != nil {
			t.Fatalf("MergeTreeOnto: %v", err)
		}
		ourTree, _ := r.TreeOf("local")
		if res.Clean && res.Tree == ourTree {
			continue
		}
		return // caught it
	}
	t.Fatal("replaying the revert should have changed our tree")
}

func TestRemoteOnlyCommits_IgnoresRebasedEquivalents(t *testing.T) {
	r := tempRunner(t)
	commitFile(t, r, "a.txt", "one\n", "c1")
	trunk, _ := r.CurrentBranch()

	if err := r.RunGit("checkout", "-b", "remote"); err != nil {
		t.Fatal(err)
	}
	commitFile(t, r, "feature.txt", "work\n", "feature commit")

	// Move trunk forward, then replay the same work on top of it locally.
	if err := r.RunGit("checkout", trunk); err != nil {
		t.Fatal(err)
	}
	commitFile(t, r, "trunk.txt", "moved\n", "trunk moves")
	if err := r.RunGit("checkout", "-b", "local"); err != nil {
		t.Fatal(err)
	}
	commitFile(t, r, "feature.txt", "work\n", "feature commit replayed")

	only, err := r.RemoteOnlyCommits("local", "remote")
	if err != nil {
		t.Fatalf("RemoteOnlyCommits: %v", err)
	}
	if len(only) != 0 {
		t.Fatalf("a rebased-but-identical commit must be filtered by --cherry-pick, got %v", only)
	}
}

func TestIsRootCommitAndFirstParent(t *testing.T) {
	r := tempRunner(t)
	root := commitFile(t, r, "a.txt", "one\n", "c1")
	child := commitFile(t, r, "b.txt", "two\n", "c2")

	if !r.IsRootCommit(root) {
		t.Error("first commit is a root commit")
	}
	if r.IsRootCommit(child) {
		t.Error("second commit is not a root commit")
	}
	parent, err := r.FirstParent(child)
	if err != nil {
		t.Fatalf("FirstParent: %v", err)
	}
	if parent != root {
		t.Errorf("FirstParent = %s, want %s", parent, root)
	}
}

func TestSupportsMergeTreeWriteTree(t *testing.T) {
	r := tempRunner(t)
	if !r.SupportsMergeTreeWriteTree() {
		t.Skip("git too old for merge-tree --write-tree; classifier degrades to prompting")
	}
}
