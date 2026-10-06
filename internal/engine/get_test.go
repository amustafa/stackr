package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/git"
	"github.com/amustafa/stackr/internal/graph"
	"github.com/amustafa/stackr/internal/store"
)

func setupGetTestEnv(t *testing.T) (local *context.Context, remoteDir string) {
	t.Helper()

	remoteDir = t.TempDir()
	remote := &git.Runner{Dir: remoteDir}
	if _, err := remote.RunGitCapture("init", "--bare"); err != nil {
		t.Fatalf("bare init: %v", err)
	}

	localDir := t.TempDir()
	localRunner := &git.Runner{Dir: localDir}
	if _, err := localRunner.RunGitCapture("clone", remoteDir, "."); err != nil {
		t.Fatalf("clone: %v", err)
	}
	localRunner.RunGitCapture("config", "user.email", "test@test.com")
	localRunner.RunGitCapture("config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(localDir, "init.txt"), []byte("init"), 0o644); err != nil {
		t.Fatalf("write init.txt: %v", err)
	}
	localRunner.RunGitCapture("add", "init.txt")
	if err := localRunner.RunGit("commit", "-m", "initial"); err != nil {
		t.Fatalf("initial commit: %v", err)
	}
	localRunner.RunGitCapture("branch", "-M", "main")
	if err := localRunner.RunGit("push", "origin", "main"); err != nil {
		t.Fatalf("push main: %v", err)
	}

	gitDir, _ := localRunner.GitCommonDir()
	s := store.NewRefStore(localRunner, gitDir)
	s.Init()

	g := graph.New()
	mainRev, _ := localRunner.RevParse("main")
	g.AddTrunk("main", mainRev)
	s.WriteGraph(g)
	s.WriteConfig(&store.Config{Remote: "origin"})

	ctx := &context.Context{
		Git:         localRunner,
		Store:       s,
		Interactive: false,
		Quiet:       true,
	}

	return ctx, remoteDir
}

func addRemoteBranch(t *testing.T, remoteDir, branch, file, content string) {
	t.Helper()
	tmpDir := t.TempDir()
	r := &git.Runner{Dir: tmpDir}
	r.RunGitCapture("clone", remoteDir, ".")
	r.RunGitCapture("config", "user.email", "test@test.com")
	r.RunGitCapture("config", "user.name", "Test")
	r.RunGitCapture("checkout", "-b", branch, "origin/main")
	os.WriteFile(filepath.Join(tmpDir, file), []byte(content), 0o644)
	r.RunGitCapture("add", file)
	r.RunGit("commit", "-m", "add "+file)
	r.RunGit("push", "origin", branch)
}

func TestGet_SimpleFFBranch(t *testing.T) {
	c, remoteDir := setupGetTestEnv(t)

	addRemoteBranch(t, remoteDir, "feat-a", "a.txt", "feature a")

	g, _ := c.Store.ReadGraph()
	mainRev, _ := c.Git.RevParse("main")
	g.AddBranch("feat-a", "main", mainRev, mainRev)
	c.Store.WriteGraph(g)

	c.Git.RunGit("branch", "feat-a", "main")

	result, err := Get(c, GetOpts{Branch: "feat-a", Force: true, Stay: true})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if len(result.Synced) != 1 || result.Synced[0] != "feat-a" {
		t.Errorf("expected feat-a synced, got %v", result.Synced)
	}

	if _, err := os.Stat(filepath.Join(c.Git.Dir, "a.txt")); err == nil {
		t.Error("a.txt should not be in working dir (--stay, not checked out)")
	}
}

func TestGet_NewBranchFromRemote(t *testing.T) {
	c, remoteDir := setupGetTestEnv(t)

	addRemoteBranch(t, remoteDir, "feat-new", "new.txt", "new feature")

	result, err := Get(c, GetOpts{Branch: "feat-new", Force: true, Stay: true})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	exists, _ := c.Git.BranchExists("feat-new")
	if !exists {
		t.Error("feat-new should exist locally after get")
	}

	g, _ := c.Store.ReadGraph()
	if !g.Has("feat-new") {
		t.Error("feat-new should be tracked in the graph")
	}

	_ = result
}

func TestGet_UpToDate(t *testing.T) {
	c, remoteDir := setupGetTestEnv(t)

	addRemoteBranch(t, remoteDir, "feat-a", "a.txt", "feature a")

	c.Git.Fetch("origin")
	c.Git.RunGit("branch", "feat-a", "origin/feat-a")

	g, _ := c.Store.ReadGraph()
	rev, _ := c.Git.RevParse("feat-a")
	mainRev, _ := c.Git.RevParse("main")
	g.AddBranch("feat-a", "main", mainRev, rev)
	c.Store.WriteGraph(g)

	result, err := Get(c, GetOpts{Branch: "feat-a", Stay: true})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if len(result.Skipped) != 1 || result.Skipped[0] != "feat-a" {
		t.Errorf("expected feat-a skipped (up-to-date), got synced=%v skipped=%v", result.Synced, result.Skipped)
	}
}

func TestGet_GuardsRebaseState(t *testing.T) {
	c, _ := setupGetTestEnv(t)

	c.Store.WriteRebaseState(&store.RebaseState{
		Operation:     "restack",
		OrigBranch:    "main",
		CurrentBranch: "feat-a",
	})

	_, err := Get(c, GetOpts{Branch: "main"})
	if err == nil {
		t.Fatal("expected error when rebase state exists")
	}

	c.Store.ClearRebaseState()
}

func TestGet_GuardsGetState(t *testing.T) {
	c, _ := setupGetTestEnv(t)

	c.Store.WriteGetState(&store.GetState{
		Operation: "get",
		Target:    "feat-a",
	})

	_, err := Get(c, GetOpts{Branch: "main"})
	if err == nil {
		t.Fatal("expected error when get state exists")
	}

	c.Store.ClearGetState()
}

func TestGet_DownstackOnly(t *testing.T) {
	c, remoteDir := setupGetTestEnv(t)

	addRemoteBranch(t, remoteDir, "feat-a", "a.txt", "feature a")
	addRemoteBranch(t, remoteDir, "feat-b", "b.txt", "feature b")

	c.Git.Fetch("origin")
	c.Git.RunGit("branch", "feat-a", "origin/feat-a")
	c.Git.RunGit("branch", "feat-b", "origin/feat-b")

	g, _ := c.Store.ReadGraph()
	mainRev, _ := c.Git.RevParse("main")
	aRev, _ := c.Git.RevParse("feat-a")
	bRev, _ := c.Git.RevParse("feat-b")
	g.AddBranch("feat-a", "main", mainRev, aRev)
	g.AddBranch("feat-b", "feat-a", aRev, bRev)
	c.Store.WriteGraph(g)

	result, err := Get(c, GetOpts{Branch: "feat-a", Downstack: true, Stay: true})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	for _, s := range result.Synced {
		if s == "feat-b" {
			t.Error("feat-b should not be synced with --downstack")
		}
	}
	for _, s := range result.Created {
		if s == "feat-b" {
			t.Error("feat-b should not be created with --downstack")
		}
	}
}

func TestComputeWalkPath(t *testing.T) {
	g := graph.New()
	g.AddTrunk("main", "abc")
	g.AddBranch("feat-a", "main", "abc", "def")
	g.AddBranch("feat-b", "feat-a", "def", "ghi")
	g.AddBranch("feat-c", "feat-b", "ghi", "jkl")

	path := computeWalkPath(g, "feat-c")
	expected := []string{"feat-a", "feat-b", "feat-c"}
	if len(path) != len(expected) {
		t.Fatalf("walk path length = %d, want %d", len(path), len(expected))
	}
	for i, name := range expected {
		if path[i] != name {
			t.Errorf("walk path[%d] = %q, want %q", i, path[i], name)
		}
	}
}

func TestComputeWalkPath_SingleBranch(t *testing.T) {
	g := graph.New()
	g.AddTrunk("main", "abc")
	g.AddBranch("feat-a", "main", "abc", "def")

	path := computeWalkPath(g, "feat-a")
	if len(path) != 1 || path[0] != "feat-a" {
		t.Errorf("walk path = %v, want [feat-a]", path)
	}
}

// rebaseRemoteFeatureOntoNewTrunk plays the forge: from a throwaway clone it
// lands commit M1 on main, rebases feature onto it (identical patch), and
// force-pushes feature. With aheadAgain it then lands M2 on main as well, so
// the remote branch sits on a trunk commit older than trunk's tip. Returns
// the SHAs of M1, the rebuilt feature tip, and main's final tip.
func rebaseRemoteFeatureOntoNewTrunk(t *testing.T, remoteDir string, aheadAgain bool) (m1, f1Rebuilt, mainTip string) {
	t.Helper()
	dir := t.TempDir()
	r := &git.Runner{Dir: dir}
	r.RunGitCapture("clone", remoteDir, ".")
	r.RunGitCapture("config", "user.email", "test@test.com")
	r.RunGitCapture("config", "user.name", "Test")

	if err := r.RunGit("checkout", "main"); err != nil {
		t.Fatalf("checkout main: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "m1.txt"), []byte("m1"), 0o644)
	r.RunGitCapture("add", "m1.txt")
	if err := r.RunGit("commit", "-m", "trunk: M1"); err != nil {
		t.Fatalf("commit M1: %v", err)
	}
	m1, _ = r.RevParse("main")
	if err := r.RunGit("push", "origin", "main"); err != nil {
		t.Fatalf("push M1: %v", err)
	}

	if err := r.RunGit("checkout", "-b", "feature", "origin/feature"); err != nil {
		t.Fatalf("checkout feature: %v", err)
	}
	if err := r.RunGit("rebase", "main"); err != nil {
		t.Fatalf("rebase feature: %v", err)
	}
	f1Rebuilt, _ = r.RevParse("feature")
	if err := r.RunGit("push", "--force", "origin", "feature"); err != nil {
		t.Fatalf("force-push feature: %v", err)
	}

	mainTip = m1
	if aheadAgain {
		r.RunGit("checkout", "main")
		os.WriteFile(filepath.Join(dir, "m2.txt"), []byte("m2"), 0o644)
		r.RunGitCapture("add", "m2.txt")
		if err := r.RunGit("commit", "-m", "trunk: M2"); err != nil {
			t.Fatalf("commit M2: %v", err)
		}
		mainTip, _ = r.RevParse("main")
		if err := r.RunGit("push", "origin", "main"); err != nil {
			t.Fatalf("push M2: %v", err)
		}
	}
	return m1, f1Rebuilt, mainTip
}

// localFeatureOffMain creates feature locally with one commit on main, pushes
// it, and tracks it in the graph with main's tip as its base.
func localFeatureOffMain(t *testing.T, c *context.Context) (m0, f1 string) {
	t.Helper()
	m0, _ = c.Git.RevParse("main")
	if err := c.Git.RunGit("checkout", "-b", "feature", "main"); err != nil {
		t.Fatalf("checkout feature: %v", err)
	}
	commitFile(t, c, "f.txt", "feature work", "feature: F1")
	f1, _ = c.Git.RevParse("feature")
	if err := c.Git.RunGit("push", "-u", "origin", "feature"); err != nil {
		t.Fatalf("push feature: %v", err)
	}
	if err := c.Git.Checkout("main"); err != nil {
		t.Fatalf("checkout main: %v", err)
	}
	g, _ := c.Store.ReadGraph()
	if err := g.AddBranch("feature", "main", m0, f1); err != nil {
		t.Fatalf("add branch: %v", err)
	}
	if err := c.Store.WriteGraph(g); err != nil {
		t.Fatalf("write graph: %v", err)
	}
	return m0, f1
}

// Replacing a branch's tip with the remote's changes what the branch is built
// on whenever the remote was rebased — the ordinary forge auto-rebase onto a
// parent that moved. The recorded base must follow: with the parent's tip now
// in the branch's history, that tip is the base, not the commit the branch
// was first created from.
func TestReplaceWithRemote_RemoteRebasedOntoParent_RederivesBase(t *testing.T) {
	c, remoteDir := setupGetTestEnv(t)
	m0, _ := localFeatureOffMain(t, c)
	m1, f1Rebuilt, _ := rebaseRemoteFeatureOntoNewTrunk(t, remoteDir, false)

	// Local trunk catches up, as get does before walking the stack.
	if err := c.Git.RunGit("fetch", "origin"); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if err := c.Git.RunGit("merge", "--ff-only", "origin/main"); err != nil {
		t.Fatalf("fast-forward main: %v", err)
	}

	g, _ := c.Store.ReadGraph()
	if _, err := replaceWithRemote(c, g, "feature", "origin/feature"); err != nil {
		t.Fatalf("replaceWithRemote: %v", err)
	}

	b := g.Branches["feature"]
	if b.BranchRevision != f1Rebuilt {
		t.Errorf("tip = %s, want the remote's %s", abbrev(b.BranchRevision), abbrev(f1Rebuilt))
	}
	if b.ParentBranchRevision != m1 {
		t.Errorf("base = %s, want the parent's tip %s now that the branch is built on it (was %s)",
			abbrev(b.ParentBranchRevision), abbrev(m1), abbrev(m0))
	}
}

// When the parent's tip is NOT in the replaced branch's history — local trunk
// has moved past the commit the remote branch was rebuilt on — and the old
// pointer no longer holds either, a trunk parent still yields a base: the
// merge-base with trunk. That is the record sync's cleanup used to leave
// behind (a deleted parent's tip), repaired here by the next get.
func TestReplaceWithRemote_StaleBaseWithTrunkParent_UsesMergeBase(t *testing.T) {
	c, remoteDir := setupGetTestEnv(t)
	localFeatureOffMain(t, c)
	m1, f1Rebuilt, m2 := rebaseRemoteFeatureOntoNewTrunk(t, remoteDir, true)

	if err := c.Git.RunGit("fetch", "origin"); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if err := c.Git.RunGit("merge", "--ff-only", "origin/main"); err != nil {
		t.Fatalf("fast-forward main: %v", err)
	}
	if tip, _ := c.Git.RevParse("main"); tip != m2 {
		t.Fatalf("local main should be at M2 %s, got %s", abbrev(m2), abbrev(tip))
	}

	// A base that exists as an object but is in nobody's history — the shape
	// a deleted parent's tip takes once its branch is gone.
	c.Git.RunGitCapture("checkout", "-b", "dangling", "main")
	c.Git.RunGitCapture("commit", "--allow-empty", "-m", "dangling")
	dangling, _ := c.Git.RevParse("dangling")
	c.Git.Checkout("main")
	c.Git.RunGitCapture("branch", "-D", "dangling")

	g, _ := c.Store.ReadGraph()
	g.Branches["feature"].ParentBranchRevision = dangling

	c.Quiet = false
	out := captureStdout(t, func() {
		if _, err := replaceWithRemote(c, g, "feature", "origin/feature"); err != nil {
			t.Fatalf("replaceWithRemote: %v", err)
		}
	})
	c.Quiet = true

	b := g.Branches["feature"]
	if b.BranchRevision != f1Rebuilt {
		t.Errorf("tip = %s, want the remote's %s", abbrev(b.BranchRevision), abbrev(f1Rebuilt))
	}
	if b.ParentBranchRevision != m1 {
		t.Errorf("base = %s, want the merge-base with trunk %s", abbrev(b.ParentBranchRevision), abbrev(m1))
	}
	if !strings.Contains(out, "merge-base") {
		t.Errorf("recovery should be reported, got:\n%s", out)
	}
}
