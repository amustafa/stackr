package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amustafa/stackr/internal/context"
)

// stubGH puts a `gh` on PATH that always reports "no PR for this branch", so
// PrepareAI never reaches the network from a test.
func stubGH(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "gh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write gh stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// runEmittedCommand executes the command string exactly as emitted, so the test
// exercises what an agent would actually run rather than a reconstruction of it.
func runEmittedCommand(t *testing.T, c *context.Context, command string) string {
	t.Helper()
	fields := strings.Fields(command)
	if len(fields) < 2 || fields[0] != "git" {
		t.Fatalf("emitted command is not a git invocation: %q", command)
	}
	out, err := c.Git.RunGitCapture(fields[1:]...)
	if err != nil {
		t.Fatalf("emitted command %q failed: %v", command, err)
	}
	return out
}

// The JSON handed to an agent must not carry the patch — that was the whole
// point of the field — and it must carry a command the agent can run instead.
func TestPrepareAI_EmitsDiffCommandInsteadOfDiffContent(t *testing.T) {
	stubGH(t)
	c, trunk := newBaseRepo(t)

	if err := Create(c, CreateOpts{Name: "feature"}); err != nil {
		t.Fatalf("create feature: %v", err)
	}
	const sentinel = "SENTINEL_PATCH_CONTENT"
	commitFile(t, c, "feature.txt", sentinel+"\n", "feat: add feature")
	syncTip(t, c, "feature")

	result, err := PrepareAI(c)
	if err != nil {
		t.Fatalf("PrepareAI: %v", err)
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	blob := string(data)

	if strings.Contains(blob, sentinel) {
		t.Error("aiprepare JSON still contains patch content")
	}
	for _, marker := range []string{"diff --git", "@@ -"} {
		if strings.Contains(blob, marker) {
			t.Errorf("aiprepare JSON still contains patch markers (%q)", marker)
		}
	}
	if strings.Contains(blob, `"diff"`) {
		t.Error("aiprepare JSON still advertises a `diff` field")
	}

	if result.DiffCommand == nil {
		t.Fatal("no diffCommand emitted; the agent has no way to reach the patch")
	}
	if want := "git diff " + trunk + "..feature"; result.DiffCommand.Command != want {
		t.Errorf("diffCommand.command = %q, want %q", result.DiffCommand.Command, want)
	}
	if result.DiffCommand.Note == "" {
		t.Error("diffCommand carries no note explaining what it returns")
	}
	if !strings.Contains(blob, `"diffCommand"`) {
		t.Error("diffCommand missing from the marshalled JSON")
	}

	if got := runEmittedCommand(t, c, result.DiffCommand.Command); !strings.Contains(got, sentinel) {
		t.Error("emitted command does not produce this branch's changes")
	}
}

// The emitted command has to reproduce exactly what the field used to hold. The
// case that separates a correct two-dot range from a three-dot one is a parent
// that has moved ahead since the branch was stacked on it: two-dot compares
// against the parent's tip, three-dot against the merge base.
func TestPrepareAI_DiffCommandMatchesDiffPatch(t *testing.T) {
	stubGH(t)
	c, _ := newBaseRepo(t)

	if err := Create(c, CreateOpts{Name: "a"}); err != nil {
		t.Fatalf("create a: %v", err)
	}
	commitFile(t, c, "a.txt", "from a\n", "a: first")
	syncTip(t, c, "a")

	if err := Create(c, CreateOpts{Name: "b"}); err != nil {
		t.Fatalf("create b: %v", err)
	}
	commitFile(t, c, "b.txt", "from b\n", "b: first")
	syncTip(t, c, "b")

	// The parent gains work that `b` has not been restacked onto, so the
	// two-dot and three-dot ranges now disagree.
	if err := c.Git.Checkout("a"); err != nil {
		t.Fatalf("checkout a: %v", err)
	}
	commitFile(t, c, "a2.txt", "later work on a\n", "a: second")
	syncTip(t, c, "a")
	if err := c.Git.Checkout("b"); err != nil {
		t.Fatalf("checkout b: %v", err)
	}

	threeDot, err := c.Git.RunGitCapture("diff", "a...b")
	if err != nil {
		t.Fatalf("three-dot diff: %v", err)
	}
	want, err := c.Git.DiffPatch("a", "b")
	if err != nil {
		t.Fatalf("DiffPatch: %v", err)
	}
	if want == threeDot {
		t.Fatal("test setup is wrong: two-dot and three-dot ranges must differ here")
	}

	result, err := PrepareAI(c)
	if err != nil {
		t.Fatalf("PrepareAI: %v", err)
	}
	if result.DiffCommand == nil {
		t.Fatal("no diffCommand emitted")
	}
	if got := runEmittedCommand(t, c, result.DiffCommand.Command); got != want {
		t.Errorf("emitted command does not reproduce DiffPatch output:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// Branch names are usually plain, but git permits characters a shell would act
// on, and the command is meant to be pasted into one.
func TestBuildDiffCommand_QuotesShellUnsafeBranchNames(t *testing.T) {
	tests := []struct {
		name   string
		parent string
		branch string
		want   string
	}{
		{"plain names stay unquoted", "main", "feat/login", "git diff main..feat/login"},
		{"metacharacters are quoted", "main", "feat;rm -rf x", `git diff 'main..feat;rm -rf x'`},
		{"embedded quote is escaped", "main", "feat'x", `git diff 'main..feat'\''x'`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildDiffCommand(tt.parent, tt.branch)
			if got.Command != tt.want {
				t.Errorf("command = %q, want %q", got.Command, tt.want)
			}
		})
	}
}

// The prompt for `sr submit --ai` must not claim the model was handed a diff,
// and must point it at the command instead.
func TestBuildAISystemPrompt_PointsAtDiffCommand(t *testing.T) {
	prompt := BuildAISystemPrompt()

	if !strings.Contains(prompt, "diffCommand") {
		t.Error("prompt never mentions the diffCommand field")
	}
	if strings.Contains(prompt, "branch's info, diff, commits") {
		t.Error("prompt still tells the model it was given the diff")
	}
}
