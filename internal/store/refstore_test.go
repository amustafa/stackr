package store

import (
	"testing"

	"github.com/amustafa/stackr/internal/git"
	"github.com/amustafa/stackr/internal/graph"
)

func tempRefStore(t *testing.T) *RefStore {
	t.Helper()
	dir := t.TempDir()
	r := &git.Runner{Dir: dir}
	if _, err := r.RunGitCapture("init"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	r.RunGitCapture("config", "user.email", "test@test.com")
	r.RunGitCapture("config", "user.name", "Test")

	gitDir, err := r.GitCommonDir()
	if err != nil {
		t.Fatalf("GitCommonDir: %v", err)
	}

	rs := NewRefStore(r, gitDir)
	if err := rs.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return rs
}

func TestRefStoreConfigRoundTrip(t *testing.T) {
	rs := tempRefStore(t)
	cfg := &Config{Trunk: "main", Remote: "origin"}
	if err := rs.WriteConfig(cfg); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}

	got, err := rs.ReadConfig()
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if got.Trunk != "main" || got.Remote != "origin" {
		t.Fatalf("unexpected config: %+v", got)
	}
}

func TestRefStoreGraphRoundTrip(t *testing.T) {
	rs := tempRefStore(t)

	g := graph.New()
	g.AddTrunk("main", "abc123")
	g.AddBranch("feat-a", "main", "abc123", "def456")

	if err := rs.WriteGraph(g); err != nil {
		t.Fatalf("WriteGraph: %v", err)
	}

	got, err := rs.ReadGraph()
	if err != nil {
		t.Fatalf("ReadGraph: %v", err)
	}
	if !got.Has("main") || !got.Has("feat-a") {
		t.Fatal("expected main and feat-a in graph")
	}
	if got.Parent("feat-a") != "main" {
		t.Fatalf("expected feat-a parent 'main', got %q", got.Parent("feat-a"))
	}
}

func TestRefStorePRInfoRoundTrip(t *testing.T) {
	rs := tempRefStore(t)

	info := &PRInfo{Branches: map[string]*BranchPR{
		"feat-a": {Number: 42, URL: "https://github.com/test/pr/42", State: "open"},
	}}
	if err := rs.WritePRInfo(info); err != nil {
		t.Fatalf("WritePRInfo: %v", err)
	}

	got, err := rs.ReadPRInfo()
	if err != nil {
		t.Fatalf("ReadPRInfo: %v", err)
	}
	if got.Branches["feat-a"] == nil || got.Branches["feat-a"].Number != 42 {
		t.Fatalf("unexpected PR info: %+v", got)
	}
}

func TestRefStoreExistsAfterWrite(t *testing.T) {
	rs := tempRefStore(t)

	// Before any write, the ref doesn't exist (only local dir does).
	// Exists() should still return true because Init() created .stackr/.
	if !rs.Exists() {
		t.Fatal("expected Exists() true after Init")
	}

	// Write something to create the ref.
	cfg := &Config{Trunk: "main", Remote: "origin"}
	rs.WriteConfig(cfg)

	if !rs.Exists() {
		t.Fatal("expected Exists() true after write")
	}
}

func TestRefStoreMultipleWritesCreateCommitChain(t *testing.T) {
	rs := tempRefStore(t)

	// First write.
	cfg := &Config{Trunk: "main", Remote: "origin"}
	rs.WriteConfig(cfg)
	sha1, _ := rs.git.ReadRef(rs.ref)

	// Second write.
	g := graph.New()
	g.AddTrunk("main", "abc123")
	rs.WriteGraph(g)
	sha2, _ := rs.git.ReadRef(rs.ref)

	if sha1 == sha2 {
		t.Fatal("expected different SHAs for different commits")
	}

	// sha1 should be ancestor of sha2.
	isAnc, _ := rs.git.IsAncestor(sha1, sha2)
	if !isAnc {
		t.Fatal("expected first commit to be ancestor of second")
	}
}

func TestRefStoreEmptyReadReturnsDefaults(t *testing.T) {
	rs := tempRefStore(t)

	// Reading before any write should return empty/default values.
	g, err := rs.ReadGraph()
	if err != nil {
		t.Fatalf("ReadGraph: %v", err)
	}
	if len(g.Branches) != 0 {
		t.Fatalf("expected empty graph, got %d branches", len(g.Branches))
	}

	info, err := rs.ReadPRInfo()
	if err != nil {
		t.Fatalf("ReadPRInfo: %v", err)
	}
	if len(info.Branches) != 0 {
		t.Fatalf("expected empty PR info, got %d", len(info.Branches))
	}
}

func TestRefStoreRebaseStateStaysLocal(t *testing.T) {
	rs := tempRefStore(t)

	if rs.HasRebaseState() {
		t.Fatal("expected no rebase state initially")
	}

	state := &RebaseState{
		Operation:     "restack",
		OrigBranch:    "feat-a",
		Pending:       []string{"feat-b"},
		Completed:     []string{},
		CurrentBranch: "feat-a",
	}
	if err := rs.WriteRebaseState(state); err != nil {
		t.Fatalf("WriteRebaseState: %v", err)
	}
	if !rs.HasRebaseState() {
		t.Fatal("expected rebase state to exist")
	}

	got, err := rs.ReadRebaseState()
	if err != nil {
		t.Fatalf("ReadRebaseState: %v", err)
	}
	if got.Operation != "restack" {
		t.Fatalf("unexpected operation: %q", got.Operation)
	}

	if err := rs.ClearRebaseState(); err != nil {
		t.Fatalf("ClearRebaseState: %v", err)
	}
	if rs.HasRebaseState() {
		t.Fatal("expected no rebase state after clear")
	}
}

func TestRefStoreUndoSnapshot(t *testing.T) {
	rs := tempRefStore(t)

	g := graph.New()
	g.AddTrunk("main", "abc123")
	rs.WriteGraph(g)

	if err := rs.SaveSnapshot("create", "feat-a"); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	event, data, err := rs.PopSnapshot()
	if err != nil {
		t.Fatalf("PopSnapshot: %v", err)
	}
	if event.Operation != "create" || event.Branch != "feat-a" {
		t.Fatalf("unexpected event: %+v", event)
	}
	if len(data) == 0 {
		t.Fatal("expected snapshot data")
	}

	_, _, err = rs.PopSnapshot()
	if err == nil {
		t.Fatal("expected error on empty undo stack")
	}
}
