package store

import (
	"encoding/json"
	"testing"

	"github.com/amustafa/stackr/internal/graph"
)

func marshalJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

func TestMergeGraphsNoConflict(t *testing.T) {
	// Base has main + feat-a.
	base := graph.New()
	base.AddTrunk("main", "aaa")
	base.AddBranch("feat-a", "main", "aaa", "bbb")

	// Local adds feat-b.
	local := graph.New()
	local.AddTrunk("main", "aaa")
	local.AddBranch("feat-a", "main", "aaa", "bbb")
	local.AddBranch("feat-b", "main", "aaa", "ccc")

	// Remote adds feat-c.
	remote := graph.New()
	remote.AddTrunk("main", "aaa")
	remote.AddBranch("feat-a", "main", "aaa", "bbb")
	remote.AddBranch("feat-c", "main", "aaa", "ddd")

	result, err := mergeGraphs(marshalJSON(t, base), marshalJSON(t, local), marshalJSON(t, remote))
	if err != nil {
		t.Fatalf("mergeGraphs: %v", err)
	}

	if !result.Has("main") {
		t.Fatal("missing main")
	}
	if !result.Has("feat-a") {
		t.Fatal("missing feat-a")
	}
	if !result.Has("feat-b") {
		t.Fatal("missing feat-b (added by local)")
	}
	if !result.Has("feat-c") {
		t.Fatal("missing feat-c (added by remote)")
	}
}

func TestMergeGraphsDeletedByRemote(t *testing.T) {
	base := graph.New()
	base.AddTrunk("main", "aaa")
	base.AddBranch("feat-a", "main", "aaa", "bbb")

	// Local unchanged.
	local := graph.New()
	local.AddTrunk("main", "aaa")
	local.AddBranch("feat-a", "main", "aaa", "bbb")

	// Remote deleted feat-a.
	remote := graph.New()
	remote.AddTrunk("main", "aaa")

	result, err := mergeGraphs(marshalJSON(t, base), marshalJSON(t, local), marshalJSON(t, remote))
	if err != nil {
		t.Fatalf("mergeGraphs: %v", err)
	}

	if result.Has("feat-a") {
		t.Fatal("feat-a should be deleted (remote deleted)")
	}
}

func TestMergeGraphsDescriptionConflict(t *testing.T) {
	base := graph.New()
	base.AddTrunk("main", "aaa")
	base.AddBranch("feat-a", "main", "aaa", "bbb")
	base.SetDescription("feat-a", "original desc")

	// Local changes description.
	local := graph.New()
	local.AddTrunk("main", "aaa")
	local.AddBranch("feat-a", "main", "aaa", "bbb")
	local.SetDescription("feat-a", "local desc")

	// Remote changes description.
	remote := graph.New()
	remote.AddTrunk("main", "aaa")
	remote.AddBranch("feat-a", "main", "aaa", "bbb")
	remote.SetDescription("feat-a", "remote desc")

	result, err := mergeGraphs(marshalJSON(t, base), marshalJSON(t, local), marshalJSON(t, remote))
	if err != nil {
		t.Fatalf("mergeGraphs: %v", err)
	}

	desc := result.Description("feat-a")
	if desc != "remote desc" {
		t.Fatalf("expected remote desc to win, got %q", desc)
	}
}

func TestMergeGraphsContextByKey(t *testing.T) {
	base := graph.New()
	base.AddTrunk("main", "aaa")
	base.AddBranch("feat-a", "main", "aaa", "bbb")
	base.SetContext("feat-a", graph.BranchContext{Key: "shared", Text: "original"})

	// Local adds a context entry.
	local := graph.New()
	local.AddTrunk("main", "aaa")
	local.AddBranch("feat-a", "main", "aaa", "bbb")
	local.SetContext("feat-a", graph.BranchContext{Key: "shared", Text: "original"})
	local.SetContext("feat-a", graph.BranchContext{Key: "local-only", Text: "from local"})

	// Remote modifies the shared context.
	remote := graph.New()
	remote.AddTrunk("main", "aaa")
	remote.AddBranch("feat-a", "main", "aaa", "bbb")
	remote.SetContext("feat-a", graph.BranchContext{Key: "shared", Text: "updated by remote"})
	remote.SetContext("feat-a", graph.BranchContext{Key: "remote-only", Text: "from remote"})

	result, err := mergeGraphs(marshalJSON(t, base), marshalJSON(t, local), marshalJSON(t, remote))
	if err != nil {
		t.Fatalf("mergeGraphs: %v", err)
	}

	ctx := result.GetContext("feat-a")
	ctxMap := make(map[string]string)
	for _, c := range ctx {
		ctxMap[c.Key] = c.Text
	}

	if ctxMap["shared"] != "updated by remote" {
		t.Fatalf("expected shared to be updated by remote, got %q", ctxMap["shared"])
	}
	if ctxMap["local-only"] != "from local" {
		t.Fatalf("expected local-only context, got %q", ctxMap["local-only"])
	}
	if ctxMap["remote-only"] != "from remote" {
		t.Fatalf("expected remote-only context, got %q", ctxMap["remote-only"])
	}
}

func TestMergeConfigTrunkRemoteWins(t *testing.T) {
	base := &Config{Trunk: "main", Remote: "origin"}
	local := &Config{Trunk: "main", Remote: "origin"}
	remote := &Config{Trunk: "develop", Remote: "origin"}

	result, err := mergeConfigs(marshalJSON(t, base), marshalJSON(t, local), marshalJSON(t, remote))
	if err != nil {
		t.Fatalf("mergeConfigs: %v", err)
	}
	if result.Trunk != "develop" {
		t.Fatalf("expected trunk 'develop', got %q", result.Trunk)
	}
}

func TestMergeConfigRemoteLocalWins(t *testing.T) {
	base := &Config{Trunk: "main", Remote: "origin"}
	local := &Config{Trunk: "main", Remote: "upstream"}
	remote := &Config{Trunk: "main", Remote: "fork"}

	result, err := mergeConfigs(marshalJSON(t, base), marshalJSON(t, local), marshalJSON(t, remote))
	if err != nil {
		t.Fatalf("mergeConfigs: %v", err)
	}
	// Remote name is clone-local, so local wins.
	if result.Remote != "upstream" {
		t.Fatalf("expected remote 'upstream' (local wins), got %q", result.Remote)
	}
}

func TestMergePRInfoPerBranch(t *testing.T) {
	base := &PRInfo{Branches: map[string]*BranchPR{
		"feat-a": {Number: 1, State: "open"},
	}}
	local := &PRInfo{Branches: map[string]*BranchPR{
		"feat-a": {Number: 1, State: "open"},
		"feat-b": {Number: 2, State: "open"},
	}}
	remote := &PRInfo{Branches: map[string]*BranchPR{
		"feat-a": {Number: 1, State: "merged"},
		"feat-c": {Number: 3, State: "open"},
	}}

	result, err := mergePRInfos(marshalJSON(t, base), marshalJSON(t, local), marshalJSON(t, remote))
	if err != nil {
		t.Fatalf("mergePRInfos: %v", err)
	}

	// feat-a: remote updated state to "merged", which ranks higher.
	if result.Branches["feat-a"].State != "merged" {
		t.Fatalf("expected feat-a state 'merged', got %q", result.Branches["feat-a"].State)
	}
	// feat-b: added by local.
	if result.Branches["feat-b"] == nil {
		t.Fatal("expected feat-b from local")
	}
	// feat-c: added by remote.
	if result.Branches["feat-c"] == nil {
		t.Fatal("expected feat-c from remote")
	}
}

func TestMergeGraphsChildrenUnion(t *testing.T) {
	// Base: main has child feat-a.
	base := graph.New()
	base.AddTrunk("main", "aaa")
	base.AddBranch("feat-a", "main", "aaa", "bbb")

	// Local adds feat-b as child of main.
	local := graph.New()
	local.AddTrunk("main", "aaa")
	local.AddBranch("feat-a", "main", "aaa", "bbb")
	local.AddBranch("feat-b", "main", "aaa", "ccc")

	// Remote adds feat-c as child of main.
	remote := graph.New()
	remote.AddTrunk("main", "aaa")
	remote.AddBranch("feat-a", "main", "aaa", "bbb")
	remote.AddBranch("feat-c", "main", "aaa", "ddd")

	result, err := mergeGraphs(marshalJSON(t, base), marshalJSON(t, local), marshalJSON(t, remote))
	if err != nil {
		t.Fatalf("mergeGraphs: %v", err)
	}

	children := result.ChildrenOf("main")
	childSet := make(map[string]bool)
	for _, c := range children {
		childSet[c] = true
	}
	if !childSet["feat-a"] {
		t.Fatal("missing feat-a in main's children")
	}
	if !childSet["feat-b"] {
		t.Fatal("missing feat-b in main's children")
	}
	if !childSet["feat-c"] {
		t.Fatal("missing feat-c in main's children")
	}
}
