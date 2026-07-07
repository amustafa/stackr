package engine

import (
	"strings"
	"testing"
)

func TestDetectSource(t *testing.T) {
	tests := []struct {
		name     string
		ref      string
		override string
		want     Source
		wantRef  string
		wantErr  bool
	}{
		{name: "plain number", ref: "123", want: SourceGitHub, wantRef: "123"},
		{name: "hash number", ref: "#123", want: SourceGitHub, wantRef: "123"},
		{name: "jira key", ref: "PROJ-456", want: SourceJira, wantRef: "PROJ-456"},
		{name: "jira key lowercased", ref: "proj-456", want: SourceJira, wantRef: "PROJ-456"},
		{name: "jira key with digit in project", ref: "AB2-9", want: SourceJira, wantRef: "AB2-9"},
		{name: "github url", ref: "https://github.com/owner/repo/issues/42", want: SourceGitHub, wantRef: "42"},
		{name: "jira browse url", ref: "https://co.atlassian.net/browse/PROJ-456", want: SourceJira, wantRef: "PROJ-456"},
		{name: "self-hosted jira browse url", ref: "https://jira.internal/browse/team-7", want: SourceJira, wantRef: "TEAM-7"},
		{name: "whitespace trimmed", ref: "  123  ", want: SourceGitHub, wantRef: "123"},

		{name: "override github on number", ref: "77", override: "github", want: SourceGitHub, wantRef: "77"},
		{name: "override jira on key", ref: "abc-1", override: "jira", want: SourceJira, wantRef: "ABC-1"},
		{name: "override github on url", ref: "https://github.com/o/r/issues/9", override: "GitHub", want: SourceGitHub, wantRef: "9"},

		{name: "empty", ref: "", wantErr: true},
		{name: "ambiguous word", ref: "banana", wantErr: true},
		{name: "single-letter project not a key", ref: "P-1", wantErr: true},
		{name: "override mismatch: github on jira key", ref: "PROJ-1", override: "github", wantErr: true},
		{name: "override mismatch: jira on number", ref: "123", override: "jira", wantErr: true},
		{name: "invalid override", ref: "123", override: "gitlab", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src, ref, err := detectSource(tc.ref, tc.override)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got source=%q ref=%q", src, ref)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if src != tc.want {
				t.Errorf("source = %q, want %q", src, tc.want)
			}
			if ref != tc.wantRef {
				t.Errorf("ref = %q, want %q", ref, tc.wantRef)
			}
		})
	}
}

func TestDeriveBranchName(t *testing.T) {
	tests := []struct {
		name string
		iss  Issue
		want string
	}{
		{
			name: "github basic",
			iss:  Issue{Ref: "123", Source: SourceGitHub, Title: "Add JWT auth"},
			want: "123-add-jwt-auth",
		},
		{
			name: "jira lowercased ref",
			iss:  Issue{Ref: "PROJ-456", Source: SourceJira, Title: "Fix login redirect"},
			want: "proj-456-fix-login-redirect",
		},
		{
			name: "punctuation collapses",
			iss:  Issue{Ref: "7", Source: SourceGitHub, Title: "  Fix: the (weird)  bug!! "},
			want: "7-fix-the-weird-bug",
		},
		{
			name: "empty slug falls back to ref",
			iss:  Issue{Ref: "PROJ-9", Source: SourceJira, Title: "日本語"},
			want: "proj-9",
		},
		{
			name: "long title capped without trailing dash",
			iss:  Issue{Ref: "123", Source: SourceGitHub, Title: "a very long issue title that keeps going and going and going forever"},
			want: "123-a-very-long-issue-title-that-keeps-going-and-g",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveBranchName(tc.iss)
			if got != tc.want {
				t.Errorf("deriveBranchName = %q, want %q", got, tc.want)
			}
			if len(got) > branchNameCap {
				t.Errorf("name %q exceeds cap %d", got, branchNameCap)
			}
			if strings.Contains(got, "/") {
				t.Errorf("name %q must be flat (no slash)", got)
			}
			if strings.HasSuffix(got, "-") {
				t.Errorf("name %q must not end with a dash", got)
			}
		})
	}
}

func TestBuildPrompt(t *testing.T) {
	iss := Issue{
		Ref:      "123",
		Source:   SourceGitHub,
		Title:    "Add JWT auth",
		Body:     "We need stateless tokens.",
		Labels:   []string{"auth", "backend"},
		URL:      "https://github.com/o/r/issues/123",
		Comments: "@alice: use RS256",
	}

	p := buildPrompt(iss, "123-add-jwt-auth", false)
	for _, want := range []string{
		"#123",
		"branch 123-add-jwt-auth",
		"Closes #123",
		"# Add JWT auth",
		"[labels: auth, backend]",
		"We need stateless tokens.",
		"https://github.com/o/r/issues/123",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q\n---\n%s", want, p)
		}
	}
	if strings.Contains(p, "Discussion") {
		t.Errorf("comments should be excluded when withComments=false:\n%s", p)
	}

	withC := buildPrompt(iss, "123-add-jwt-auth", true)
	if !strings.Contains(withC, "--- Discussion ---") || !strings.Contains(withC, "@alice: use RS256") {
		t.Errorf("comments should be included when withComments=true:\n%s", withC)
	}
}

func TestBuildPromptJira(t *testing.T) {
	iss := Issue{Ref: "PROJ-456", Source: SourceJira, Title: "Fix login", Body: "broken redirect"}
	p := buildPrompt(iss, "proj-456-fix-login", false)
	if !strings.Contains(p, "PROJ-456") {
		t.Errorf("jira prompt should name the key:\n%s", p)
	}
	if strings.Contains(p, "Closes #") {
		t.Errorf("jira prompt should not use GitHub Closes syntax:\n%s", p)
	}
	if strings.Contains(p, "[labels:") {
		t.Errorf("no labels should render no label bracket:\n%s", p)
	}
}
