package engine

import (
	"os/exec"
	"strings"
	"testing"
)

func TestParseGitHubIssue(t *testing.T) {
	data := []byte(`{
		"title": "Add JWT auth",
		"body": "We need stateless tokens.",
		"url": "https://github.com/o/r/issues/123",
		"labels": [{"name": "auth"}, {"name": "backend"}, {"name": ""}],
		"comments": [
			{"author": {"login": "alice"}, "body": "use RS256"},
			{"author": {"login": ""}, "body": "  agreed  "}
		]
	}`)

	iss, err := parseGitHubIssue(data, "123", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if iss.Ref != "123" || iss.Source != SourceGitHub {
		t.Errorf("ref/source = %q/%q", iss.Ref, iss.Source)
	}
	if iss.Title != "Add JWT auth" || iss.Body != "We need stateless tokens." {
		t.Errorf("title/body = %q / %q", iss.Title, iss.Body)
	}
	if iss.URL != "https://github.com/o/r/issues/123" {
		t.Errorf("url = %q", iss.URL)
	}
	if strings.Join(iss.Labels, ",") != "auth,backend" {
		t.Errorf("labels = %v (empty label should be dropped)", iss.Labels)
	}
	if !strings.Contains(iss.Comments, "@alice: use RS256") || !strings.Contains(iss.Comments, "@unknown: agreed") {
		t.Errorf("comments = %q", iss.Comments)
	}
}

func TestParseGitHubIssueNoComments(t *testing.T) {
	data := []byte(`{"title":"T","body":"B","url":"U","labels":[],"comments":[{"author":{"login":"x"},"body":"y"}]}`)
	iss, err := parseGitHubIssue(data, "5", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if iss.Comments != "" {
		t.Errorf("comments should be empty when withComments=false, got %q", iss.Comments)
	}
	if len(iss.Labels) != 0 {
		t.Errorf("labels should be empty, got %v", iss.Labels)
	}
}

func TestParseGitHubIssueBadJSON(t *testing.T) {
	if _, err := parseGitHubIssue([]byte("not json"), "1", false); err == nil {
		t.Error("expected error on malformed json")
	}
}

func TestParseJiraRaw(t *testing.T) {
	data := []byte(`{
		"key": "PROJ-456",
		"fields": {
			"summary": "Fix login redirect",
			"labels": ["auth", "regression"],
			"description": {"type": "doc", "content": [{"type": "paragraph"}]}
		}
	}`)
	title, labels, err := parseJiraRaw(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if title != "Fix login redirect" {
		t.Errorf("title = %q", title)
	}
	if strings.Join(labels, ",") != "auth,regression" {
		t.Errorf("labels = %v", labels)
	}
}

func TestParseJiraRawBadJSON(t *testing.T) {
	if _, _, err := parseJiraRaw([]byte("{")); err == nil {
		t.Error("expected error on malformed json")
	}
}

func TestStripANSI(t *testing.T) {
	in := "\x1b[1mBold\x1b[0m and \x1b[31mred\x1b[0m text"
	if got := stripANSI(in); got != "Bold and red text" {
		t.Errorf("stripANSI = %q", got)
	}
}

// Live fetch is gated on the tool being present; it never runs in CI without
// gh/jira installed (mirrors the docker-gated sandbox tests).
func TestFetchIssueUnknownSource(t *testing.T) {
	if _, err := fetchIssue(Source("gitlab"), "1", "1", false); err == nil {
		t.Error("expected error for unknown source")
	}
}

func TestJiraCheckInstalled(t *testing.T) {
	if _, err := exec.LookPath("jira"); err != nil {
		if err := jiraCheckInstalled(); err == nil {
			t.Error("jiraCheckInstalled should error when jira is absent")
		}
		t.Skip("jira not installed")
	}
	if err := jiraCheckInstalled(); err != nil {
		t.Errorf("jiraCheckInstalled should pass when jira is present: %v", err)
	}
}
