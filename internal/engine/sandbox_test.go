package engine

import (
	"regexp"
	"strings"
	"testing"

	"github.com/amustafa/stackr/internal/sandbox"
)

func testSpec() launchSpec {
	return launchSpec{
		Branch:       "feat-x",
		WorktreePath: "/home/me/repo/.worktrees/feat-x",
		GitCommonDir: "/home/me/repo/.git",
		Home:         "/home/me",
		ClaudeDir:    "/home/me/.claude",
		ClaudeJSON:   "/home/me/.claude.json",
		User:         "1000:1000",
		Image:        "stackr-sandbox:base",
		BinDir:       "/home/me/repo/.stackr/sandbox/bin",
		PathMounts:   []string{"/home/me/tools/bin"},
		Prompt:       "add tests",
	}
}

func TestBuildMountsIncludesClaudeJSONAndDedupes(t *testing.T) {
	s := testSpec()
	s.ExtraMounts = []sandbox.Mount{{Source: "/home/me/.claude", Target: "/home/me/.claude"}} // dup of ClaudeDir
	targets := map[string]bool{}
	for _, m := range buildMounts(s) {
		if m.Source != m.Target {
			t.Errorf("mount not path-identical: %+v", m)
		}
		if targets[m.Target] {
			t.Errorf("duplicate mount target %q", m.Target)
		}
		targets[m.Target] = true
	}
	for _, req := range []string{s.WorktreePath, s.GitCommonDir, s.ClaudeDir, s.ClaudeJSON, s.BinDir, "/home/me/tools/bin"} {
		if !targets[req] {
			t.Errorf("missing required mount %q", req)
		}
	}
}

func TestClaudeCommand(t *testing.T) {
	s := testSpec()
	got := claudeCommand(s)
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "--dangerously-skip-permissions") || !strings.HasSuffix(joined, "add tests") {
		t.Fatalf("unexpected command: %v", got)
	}
	// With a settings file, --settings must precede skip-permissions.
	s.SettingsFile = "/x/settings.json"
	got = claudeCommand(s)
	if got[1] != "--settings" || got[2] != "/x/settings.json" {
		t.Fatalf("settings flag not wired: %v", got)
	}
	// No prompt → no trailing arg.
	s.Prompt = ""
	s.SettingsFile = ""
	if last := claudeCommand(s); last[len(last)-1] != "--dangerously-skip-permissions" {
		t.Fatalf("empty prompt should not add an arg: %v", last)
	}
}

func TestBuildLayoutValidKDL(t *testing.T) {
	l := buildLayout(testSpec())
	if !strings.Contains(l, `pane command="claude"`) {
		t.Fatalf("layout missing claude pane: %s", l)
	}
	// Single args node with both values (repeated args lines are invalid KDL).
	if !strings.Contains(l, `args "--dangerously-skip-permissions" "add tests"`) {
		t.Fatalf("layout args not on one node: %s", l)
	}
	if strings.Count(l, "args") != 1 {
		t.Fatalf("expected exactly one args node: %s", l)
	}
	// Balanced braces.
	if strings.Count(l, "{") != strings.Count(l, "}") {
		t.Fatalf("unbalanced braces in layout: %s", l)
	}
}

func TestBuildPATH(t *testing.T) {
	p := buildPATH(testSpec())
	if !strings.HasPrefix(p, "/home/me/repo/.stackr/sandbox/bin:/home/me/tools/bin:") {
		t.Fatalf("PATH prefix wrong: %s", p)
	}
	if !strings.Contains(p, "/usr/bin") {
		t.Fatalf("PATH missing defaults: %s", p)
	}
}

func TestSandboxContainerNameSafeAndDeterministic(t *testing.T) {
	valid := regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
	for _, br := range []string{"feat-x", "builder/spir-1", "a/b/c"} {
		n := sandboxContainerName(br)
		if !valid.MatchString(n) {
			t.Errorf("container name %q for branch %q is not docker-safe", n, br)
		}
		if n != sandboxContainerName(br) {
			t.Errorf("container name not deterministic for %q", br)
		}
	}
	// Different branches → different names (hash suffix disambiguates).
	if sandboxContainerName("feat/x") == sandboxContainerName("feat-x") {
		t.Error("feat/x and feat-x should not collide")
	}
}
