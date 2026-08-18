package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amustafa/stackr/internal/engine"
)

// captureStdout runs fn with os.Stdout redirected to a pipe and returns what
// it wrote. Fine for these tests' few bytes; a writer that filled the pipe
// buffer would deadlock.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old
	out, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestHandleNavigateResult_WritesCdFileInsteadOfSentinel(t *testing.T) {
	cdFile := filepath.Join(t.TempDir(), "cd-target")
	t.Setenv("SR_CD_FILE", cdFile)

	target := t.TempDir()
	out := captureStdout(t, func() {
		handleNavigateResult(engine.NavigateResult{Branch: "b", WorktreePath: target})
	})

	data, err := os.ReadFile(cdFile)
	if err != nil {
		t.Fatalf("cd file was not written: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != target {
		t.Fatalf("cd file holds %q, want %q", got, target)
	}
	if strings.Contains(out, "__sr_cd:") {
		t.Fatalf("sentinel must not be printed when SR_CD_FILE is honored; stdout: %q", out)
	}
}

func TestHandleNavigateResult_FallsBackToSentinelWithoutEnv(t *testing.T) {
	t.Setenv("SR_CD_FILE", "")
	os.Unsetenv("SR_CD_FILE")

	target := t.TempDir()
	out := captureStdout(t, func() {
		handleNavigateResult(engine.NavigateResult{Branch: "b", WorktreePath: target})
	})

	if want := "__sr_cd:" + target; !strings.Contains(out, want) {
		t.Fatalf("old hooks rely on the sentinel; stdout %q lacks %q", out, want)
	}
}

func TestHandleNavigateResult_FallsBackWhenCdFileUnwritable(t *testing.T) {
	t.Setenv("SR_CD_FILE", filepath.Join(t.TempDir(), "no", "such", "dir", "f"))

	target := t.TempDir()
	out := captureStdout(t, func() {
		handleNavigateResult(engine.NavigateResult{Branch: "b", WorktreePath: target})
	})

	if want := "__sr_cd:" + target; !strings.Contains(out, want) {
		t.Fatalf("an unwritable cd file must fall back to the sentinel; stdout: %q", out)
	}
}

func TestHandleNavigateResult_PlainCheckoutPrintsNothing(t *testing.T) {
	cdFile := filepath.Join(t.TempDir(), "cd-target")
	t.Setenv("SR_CD_FILE", cdFile)

	out := captureStdout(t, func() {
		handleNavigateResult(engine.NavigateResult{Branch: "b"})
	})

	if out != "" {
		t.Fatalf("non-worktree navigation should print nothing, got %q", out)
	}
	if _, err := os.Stat(cdFile); !os.IsNotExist(err) {
		t.Fatalf("non-worktree navigation must not write the cd file")
	}
}
