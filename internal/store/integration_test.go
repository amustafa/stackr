package store

import (
	"testing"

	"github.com/amustafa/stackr/internal/git"
	"github.com/amustafa/stackr/internal/graph"
)

// setupTwoClones creates a bare remote and two local clones, each with an
// initialized RefStore. Simulates two developers sharing a repo.
func setupTwoClones(t *testing.T) (repoA, repoB *RefStore) {
	t.Helper()

	// Create bare remote.
	remoteDir := t.TempDir()
	remote := &git.Runner{Dir: remoteDir}
	if _, err := remote.RunGitCapture("init", "--bare"); err != nil {
		t.Fatalf("git init bare: %v", err)
	}

	// Clone A.
	dirA := t.TempDir()
	runnerA := &git.Runner{Dir: dirA}
	if _, err := runnerA.RunGitCapture("clone", remoteDir, "."); err != nil {
		t.Fatalf("clone A: %v", err)
	}
	runnerA.RunGitCapture("config", "user.email", "a@test.com")
	runnerA.RunGitCapture("config", "user.name", "Dev A")
	gitDirA, _ := runnerA.GitCommonDir()
	repoA = NewRefStore(runnerA, gitDirA)
	repoA.Init()

	// Clone B.
	dirB := t.TempDir()
	runnerB := &git.Runner{Dir: dirB}
	if _, err := runnerB.RunGitCapture("clone", remoteDir, "."); err != nil {
		t.Fatalf("clone B: %v", err)
	}
	runnerB.RunGitCapture("config", "user.email", "b@test.com")
	runnerB.RunGitCapture("config", "user.name", "Dev B")
	gitDirB, _ := runnerB.GitCommonDir()
	repoB = NewRefStore(runnerB, gitDirB)
	repoB.Init()

	return repoA, repoB
}

func TestPushPullBasic(t *testing.T) {
	repoA, repoB := setupTwoClones(t)

	// A writes config and graph, pushes.
	cfg := &Config{Trunk: "main", Remote: "origin"}
	if err := repoA.WriteConfig(cfg); err != nil {
		t.Fatalf("A.WriteConfig: %v", err)
	}
	g := graph.New()
	g.AddTrunk("main", "aaa")
	g.AddBranch("feat-a", "main", "aaa", "bbb")
	if err := repoA.WriteGraph(g); err != nil {
		t.Fatalf("A.WriteGraph: %v", err)
	}
	if err := repoA.Push("origin"); err != nil {
		t.Fatalf("A.Push: %v", err)
	}

	// B pulls and verifies.
	if err := repoB.Pull("origin"); err != nil {
		t.Fatalf("B.Pull: %v", err)
	}

	gotG, err := repoB.ReadGraph()
	if err != nil {
		t.Fatalf("B.ReadGraph: %v", err)
	}
	if !gotG.Has("main") || !gotG.Has("feat-a") {
		t.Fatalf("B missing branches after pull: %v", gotG.Branches)
	}

	gotCfg, err := repoB.ReadConfig()
	if err != nil {
		t.Fatalf("B.ReadConfig: %v", err)
	}
	if gotCfg.Trunk != "main" {
		t.Fatalf("B.trunk = %q, want main", gotCfg.Trunk)
	}
}

func TestPushPullFastForward(t *testing.T) {
	repoA, repoB := setupTwoClones(t)

	// A initializes and pushes.
	repoA.WriteConfig(&Config{Trunk: "main", Remote: "origin"})
	g := graph.New()
	g.AddTrunk("main", "aaa")
	repoA.WriteGraph(g)
	repoA.Push("origin")

	// B pulls (gets initial state).
	repoB.Pull("origin")

	// A adds a branch and pushes again.
	g, _ = repoA.ReadGraph()
	g.AddBranch("feat-a", "main", "aaa", "bbb")
	repoA.WriteGraph(g)
	repoA.Push("origin")

	// B pulls — should fast-forward.
	if err := repoB.Pull("origin"); err != nil {
		t.Fatalf("B.Pull fast-forward: %v", err)
	}

	gotG, _ := repoB.ReadGraph()
	if !gotG.Has("feat-a") {
		t.Fatal("B missing feat-a after fast-forward pull")
	}
}

func TestPushPullMergeDivergent(t *testing.T) {
	repoA, repoB := setupTwoClones(t)

	// Both start from the same state.
	cfg := &Config{Trunk: "main", Remote: "origin"}
	repoA.WriteConfig(cfg)
	g := graph.New()
	g.AddTrunk("main", "aaa")
	repoA.WriteGraph(g)
	repoA.Push("origin")
	repoB.Pull("origin")

	// A adds feat-a and pushes.
	gA, _ := repoA.ReadGraph()
	gA.AddBranch("feat-a", "main", "aaa", "bbb")
	repoA.WriteGraph(gA)
	repoA.Push("origin")

	// B (without pulling) adds feat-b and writes locally.
	gB, _ := repoB.ReadGraph()
	gB.AddBranch("feat-b", "main", "aaa", "ccc")
	repoB.WriteGraph(gB)

	// B pulls — should merge (A's feat-a + B's feat-b).
	if err := repoB.Pull("origin"); err != nil {
		t.Fatalf("B.Pull merge: %v", err)
	}

	merged, _ := repoB.ReadGraph()
	if !merged.Has("feat-a") {
		t.Fatal("merged graph missing feat-a (from A)")
	}
	if !merged.Has("feat-b") {
		t.Fatal("merged graph missing feat-b (from B)")
	}
}

func TestPullNoRemoteData(t *testing.T) {
	repoA, _ := setupTwoClones(t)

	// Pull when remote has no stackr data — should succeed silently.
	if err := repoA.Pull("origin"); err != nil {
		t.Fatalf("Pull with no remote data: %v", err)
	}
}

