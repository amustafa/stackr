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
// PrepareAI never reaches the network from a test. It prints the same message
// real gh does: a bare exit 1 is no longer read as "no PR" — it is
// indistinguishable from a server failure, which is exactly the confusion
// ghPRForBranch now refuses to make.
func stubGH(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "gh")
	body := "#!/bin/sh\necho 'no pull requests found for branch' >&2\nexit 1\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
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

// branchEntry returns the job entry for a named branch.
func branchEntry(t *testing.T, r *AIPrepareResult, name string) AIPrepareBranch {
	t.Helper()
	for _, e := range r.Branches {
		if e.Branch == name {
			return e
		}
	}
	t.Fatalf("no entry for %q among %d branches in the job", name, len(r.Branches))
	return AIPrepareBranch{}
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

	result, err := PrepareAI(c, SubmitOpts{})
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

	entry := branchEntry(t, result, "feature")
	if entry.DiffCommand == nil {
		t.Fatal("no diffCommand emitted; the agent has no way to reach the patch")
	}
	if want := "git diff " + trunk + "..feature"; entry.DiffCommand.Command != want {
		t.Errorf("diffCommand.command = %q, want %q", entry.DiffCommand.Command, want)
	}
	if entry.DiffCommand.Note == "" {
		t.Error("diffCommand carries no note explaining what it returns")
	}
	if !strings.Contains(blob, `"diffCommand"`) {
		t.Error("diffCommand missing from the marshalled JSON")
	}

	if got := runEmittedCommand(t, c, entry.DiffCommand.Command); !strings.Contains(got, sentinel) {
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

	result, err := PrepareAI(c, SubmitOpts{})
	if err != nil {
		t.Fatalf("PrepareAI: %v", err)
	}
	entry := branchEntry(t, result, "b")
	if entry.DiffCommand == nil {
		t.Fatal("no diffCommand emitted")
	}
	if got := runEmittedCommand(t, c, entry.DiffCommand.Command); got != want {
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

// A submit acts on the current branch and its downstack ancestors, so the JSON
// must describe all of them. Describing only the current branch is what left an
// agent writing one PR and leaving a hole underneath it.
func TestPrepareAI_CoversTheWholeJobBottomUp(t *testing.T) {
	stubGH(t)
	c, _ := newBaseRepo(t)

	for _, n := range []string{"a", "b", "c"} {
		if err := Create(c, CreateOpts{Name: n}); err != nil {
			t.Fatalf("create %s: %v", n, err)
		}
		commitFile(t, c, n+".txt", "from "+n+"\n", n+": work")
		syncTip(t, c, n)
	}

	result, err := PrepareAI(c, SubmitOpts{})
	if err != nil {
		t.Fatalf("PrepareAI: %v", err)
	}

	if result.Target != "c" {
		t.Errorf("target = %q, want c", result.Target)
	}

	var order []string
	for _, e := range result.Branches {
		order = append(order, e.Branch)
	}
	if strings.Join(order, ",") != "a,b,c" {
		t.Fatalf("branches = %v, want [a b c] — bottom-up, so each parent precedes its child", order)
	}

	// Every entry must stand on its own: an agent writes one PR per branch and
	// must not have to infer a branch's parent or commits from a sibling.
	for _, e := range result.Branches {
		if e.Parent == "" {
			t.Errorf("%s has no parent recorded", e.Branch)
		}
		if e.DiffCommand == nil {
			t.Errorf("%s has no diffCommand", e.Branch)
		}
		if len(e.Commits) == 0 {
			t.Errorf("%s has no commits", e.Branch)
		}
	}
}

// needsPR is what tells the agent which branches are the gaps. With the gh stub
// reporting no PR anywhere, every branch in the job is one.
func TestPrepareAI_FlagsBranchesNeedingPRs(t *testing.T) {
	stubGH(t)
	c, _ := newBaseRepo(t)

	for _, n := range []string{"a", "b"} {
		if err := Create(c, CreateOpts{Name: n}); err != nil {
			t.Fatalf("create %s: %v", n, err)
		}
		commitFile(t, c, n+".txt", "from "+n+"\n", n+": work")
		syncTip(t, c, n)
	}

	result, err := PrepareAI(c, SubmitOpts{})
	if err != nil {
		t.Fatalf("PrepareAI: %v", err)
	}
	for _, e := range result.Branches {
		if !e.NeedsPR {
			t.Errorf("%s not flagged as needing a PR despite having none", e.Branch)
		}
		if e.ExistingPR != nil {
			t.Errorf("%s reports an existing PR that does not exist", e.Branch)
		}
	}
}

// The prompt drives the whole job, so it must say that more than one PR may be
// needed and in which order — an agent that stops after the target recreates
// the exact gap this change exists to close.
func TestBuildAISystemPrompt_DescribesTheWholeJob(t *testing.T) {
	prompt := BuildAISystemPrompt()

	for _, want := range []string{"needsPR", "branches", "bottom-up", "Do not stop after the target"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt never mentions %q", want)
		}
	}
}

// The prompt has to describe the multi-branch command, not the per-branch
// checkout loop it replaced. An agent told to check each branch out will do
// exactly that, one slow and interruptible PR at a time, and a run that dies
// midway leaves a half-built stack --pr-meta's up-front validation exists to
// prevent.
func TestBuildAISystemPrompt_DirectsAtASingleSubmit(t *testing.T) {
	prompt := BuildAISystemPrompt()

	for _, want := range []string{"--pr-meta", "bodyFile", `"branch"`} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt never mentions %q", want)
		}
	}
	if strings.Contains(prompt, "sr checkout <branch>") {
		t.Error("prompt still tells the agent to check out each branch in turn")
	}
}

// existingPR carries a PR's title but not its body, and submit will not
// overwrite either. Asking the agent to judge whether a body "is clearly wrong"
// would be asking for a decision on evidence it does not have — and an agent
// that guesses rewrites a description it never read.
func TestBuildAISystemPrompt_DoesNotAskTheAgentToJudgeAnUnseenBody(t *testing.T) {
	prompt := BuildAISystemPrompt()

	if !strings.Contains(prompt, "already have a PR out of the file") {
		t.Error("prompt does not tell the agent to leave branches with a PR out of the payload")
	}
	if strings.Contains(prompt, "title or body is clearly wrong") {
		t.Error("prompt still asks the agent to judge a body it cannot see")
	}
}
