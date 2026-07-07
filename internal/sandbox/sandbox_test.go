package sandbox

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManifestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	m := &Manifest{
		Branch:    "feat-auth",
		Image:     "stackr-sandbox:base",
		Container: "sb-feat-auth",
		Mounts:    []Mount{{Source: "/a", Target: "/a"}, {Source: "/b", Target: "/b", ReadOnly: true}},
		Command:   []string{"zellij", "attach", "--create", "feat-auth"},
		SessionID: "abc",
	}
	if err := WriteManifest(dir, m); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	got, err := ReadManifest(dir, "feat-auth")
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if got.Branch != m.Branch || got.Image != m.Image || len(got.Mounts) != 2 || got.Mounts[1].ReadOnly != true {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestSlashBranchEncoding(t *testing.T) {
	dir := t.TempDir()
	m := &Manifest{Branch: "builder/spir-1", Image: "img", Command: []string{"x"}}
	if err := WriteManifest(dir, m); err != nil {
		t.Fatalf("WriteManifest slash branch: %v", err)
	}
	// The file must be flat in dir (no nested "builder/" subdir).
	if _, err := os.Stat(filepath.Join(dir, "builder%2Fspir-1.json")); err != nil {
		t.Fatalf("expected encoded flat filename: %v", err)
	}
	got, err := ReadManifest(dir, "builder/spir-1")
	if err != nil {
		t.Fatalf("ReadManifest slash branch: %v", err)
	}
	if got.Branch != "builder/spir-1" {
		t.Fatalf("branch not preserved: %q", got.Branch)
	}
}

func TestStatusRoundTripAndAwaiting(t *testing.T) {
	dir := t.TempDir()
	s := &Status{Branch: "feat-x", State: StateAwaitingChoice, Reason: "JWT or sessions?", UpdatedAt: time.Now()}
	if err := WriteStatus(dir, s); err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}
	got, err := ReadStatus(dir, "feat-x")
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	if got.State != StateAwaitingChoice || !got.State.Awaiting() || got.Reason != "JWT or sessions?" {
		t.Fatalf("status round-trip mismatch: %+v", got)
	}
	if StateWorking.Awaiting() {
		t.Fatal("working must not count as awaiting")
	}
}

func TestListStatuses(t *testing.T) {
	dir := t.TempDir()
	// Missing dir → empty, no error.
	if got, err := ListStatuses(filepath.Join(dir, "nope")); err != nil || got != nil {
		t.Fatalf("missing dir should be empty: %v %v", got, err)
	}
	_ = WriteStatus(dir, &Status{Branch: "b", State: StateWorking})
	_ = WriteStatus(dir, &Status{Branch: "a", State: StateAwaitingInput})
	_ = WriteManifest(dir, &Manifest{Branch: "a", Image: "img", Command: []string{"x"}}) // must be ignored
	got, err := ListStatuses(dir)
	if err != nil {
		t.Fatalf("ListStatuses: %v", err)
	}
	if len(got) != 2 || got[0].Branch != "a" || got[1].Branch != "b" {
		t.Fatalf("expected [a,b] sorted, got %+v", got)
	}
}
