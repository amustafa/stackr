package store

import (
	"os"
	"path/filepath"
	"testing"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	s := New(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	return s
}

func TestPushRecords_MissingFileIsEmptyNotAnError(t *testing.T) {
	s := tempStore(t)

	pr, err := s.ReadPushRecords()
	if err != nil {
		t.Fatalf("reading absent records should not error: %v", err)
	}
	if got := pr.Get("origin", "anything"); got != "" {
		t.Fatalf("empty records should yield no SHA, got %q", got)
	}
	// A clone that has never pushed must fall through to the heuristic tiers,
	// not claim the remote as its own.
	if got := s.PushRecordFor("origin", "anything"); got != "" {
		t.Fatalf("PushRecordFor on empty store = %q, want empty", got)
	}
}

func TestPushRecords_RoundTrip(t *testing.T) {
	s := tempStore(t)

	if err := s.SetPushRecord("origin", "feature-a", "abc123"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if got := s.PushRecordFor("origin", "feature-a"); got != "abc123" {
		t.Fatalf("PushRecordFor = %q, want abc123", got)
	}

	pr, err := s.ReadPushRecords()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if pr.Version != pushRecordsVersion {
		t.Errorf("version = %d, want %d", pr.Version, pushRecordsVersion)
	}
	if pr.Remotes["origin"]["feature-a"].UpdatedAt == "" {
		t.Error("UpdatedAt should be stamped")
	}
}

// Branch names contain slashes constantly. A flat "remote/branch" key could not
// be split back apart, which is why the file nests by remote.
func TestPushRecords_BranchNamesWithSlashes(t *testing.T) {
	s := tempStore(t)

	if err := s.SetPushRecord("origin", "feat/auth/login", "sha-nested"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := s.SetPushRecord("origin", "feat", "sha-short"); err != nil {
		t.Fatalf("set: %v", err)
	}

	if got := s.PushRecordFor("origin", "feat/auth/login"); got != "sha-nested" {
		t.Errorf("nested branch = %q, want sha-nested", got)
	}
	if got := s.PushRecordFor("origin", "feat"); got != "sha-short" {
		t.Errorf("short branch = %q, want sha-short", got)
	}
}

func TestPushRecords_MultipleRemotesAreIndependent(t *testing.T) {
	s := tempStore(t)

	if err := s.SetPushRecord("origin", "main-work", "origin-sha"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPushRecord("upstream", "main-work", "upstream-sha"); err != nil {
		t.Fatal(err)
	}

	if got := s.PushRecordFor("origin", "main-work"); got != "origin-sha" {
		t.Errorf("origin = %q", got)
	}
	if got := s.PushRecordFor("upstream", "main-work"); got != "upstream-sha" {
		t.Errorf("upstream = %q", got)
	}
}

// A recreated branch name must not inherit the old branch's claim — otherwise
// tier 0 would authorise a force push over work it never made.
func TestPushRecords_DeleteForBranchClearsEveryRemote(t *testing.T) {
	s := tempStore(t)

	s.SetPushRecord("origin", "gone", "sha1")
	s.SetPushRecord("upstream", "gone", "sha2")
	s.SetPushRecord("origin", "stays", "sha3")

	if err := s.DeletePushRecordsForBranch("gone"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if got := s.PushRecordFor("origin", "gone"); got != "" {
		t.Errorf("origin/gone survived: %q", got)
	}
	if got := s.PushRecordFor("upstream", "gone"); got != "" {
		t.Errorf("upstream/gone survived: %q", got)
	}
	if got := s.PushRecordFor("origin", "stays"); got != "sha3" {
		t.Errorf("unrelated branch was clobbered: %q", got)
	}
}

func TestPushRecords_DeleteMissingBranchIsNoOp(t *testing.T) {
	s := tempStore(t)
	if err := s.DeletePushRecordsForBranch("never-existed"); err != nil {
		t.Fatalf("deleting an absent branch should be a no-op, got %v", err)
	}
}

func TestStoreInit_CreatesRollbackDir(t *testing.T) {
	s := tempStore(t)
	info, err := os.Stat(filepath.Join(s.Root(), "rollback"))
	if err != nil || !info.IsDir() {
		t.Fatalf("Init should create .stackr/rollback: %v", err)
	}
}
