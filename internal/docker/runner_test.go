package docker

import (
	"slices"
	"strings"
	"testing"
)

func TestRunOptsArgs(t *testing.T) {
	opts := RunOpts{
		Image:   "stackr-sandbox:base",
		Name:    "sb-feat-x",
		Labels:  map[string]string{"stackr.sandbox": "abc123"},
		Env:     map[string]string{"SR_SANDBOX": "feat-x", "HOME": "/home/amustafa"},
		Workdir: "/home/amustafa/workspace/stackr/.worktrees/feat-x",
		User:    "1000:1000",
		Network: "none",
		CapAdd:  []string{"NET_ADMIN"},
		Detach:  true,
		Mounts: []Mount{
			{Source: "/home/amustafa/.claude", Target: "/home/amustafa/.claude"},
		},
		Command: []string{"zellij", "attach", "--create", "feat-x"},
	}

	got := opts.args()

	// Structural checks.
	if got[0] != "run" || !slices.Contains(got, "-d") {
		t.Fatalf("expected `run -d`, got %v", got)
	}
	// Env keys must be sorted: HOME before SR_SANDBOX.
	homeIdx := slices.Index(got, "HOME=/home/amustafa")
	srIdx := slices.Index(got, "SR_SANDBOX=feat-x")
	if homeIdx == -1 || srIdx == -1 || homeIdx > srIdx {
		t.Fatalf("env keys should be sorted (HOME before SR_SANDBOX), got %v", got)
	}
	// Image precedes the command, both at the tail.
	imgIdx := slices.Index(got, "stackr-sandbox:base")
	if imgIdx == -1 || got[imgIdx+1] != "zellij" || got[len(got)-1] != "feat-x" {
		t.Fatalf("image should immediately precede the command, got %v", got)
	}
	if !slices.Contains(got, "--cap-add") || !slices.Contains(got, "NET_ADMIN") {
		t.Fatalf("expected --cap-add NET_ADMIN, got %v", got)
	}
	if !slices.Contains(got, "--network") || !slices.Contains(got, "none") {
		t.Fatalf("expected --network none, got %v", got)
	}
}

func TestRunOptsArgsDeterministic(t *testing.T) {
	opts := RunOpts{
		Image:  "img",
		Labels: map[string]string{"a": "1", "b": "2", "c": "3"},
		Env:    map[string]string{"X": "1", "Y": "2", "Z": "3"},
	}
	first := strings.Join(opts.args(), " ")
	for range 20 {
		if got := strings.Join(opts.args(), " "); got != first {
			t.Fatalf("args() not deterministic:\n first: %s\n   got: %s", first, got)
		}
	}
}

func TestMountSpec(t *testing.T) {
	rw := Mount{Source: "/a", Target: "/b"}
	if rw.spec() != "type=bind,source=/a,target=/b" {
		t.Fatalf("rw mount spec wrong: %s", rw.spec())
	}
	ro := Mount{Source: "/a", Target: "/b", ReadOnly: true}
	if ro.spec() != "type=bind,source=/a,target=/b,readonly" {
		t.Fatalf("ro mount spec wrong: %s", ro.spec())
	}
}
