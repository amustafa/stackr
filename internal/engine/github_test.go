package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// gh exits 1 for "no PR exists" and for server failures alike, so the
// classification must come from the error text. Getting it wrong in one
// direction offers to create duplicate PRs; in the other, it turns a plain
// "no PR yet" into a spurious failure.

func TestGHNoPRFound(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
		want   bool
	}{
		{"no PR for branch", `no pull requests found for branch "feature-x"`, true},
		{"server unavailable", "HTTP 503: No server is currently available to service your request. (https://api.github.com/graphql)", false},
		{"auth failure", "HTTP 401: Bad credentials (https://api.github.com/graphql)", false},
		{"empty stderr", "", false},
	}
	for _, tc := range cases {
		if got := ghNoPRFound(tc.stderr); got != tc.want {
			t.Errorf("%s: ghNoPRFound = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestGHServerError(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
		want   bool
	}{
		{"503 unavailable", "HTTP 503: No server is currently available to service your request. (https://api.github.com/graphql)", true},
		{"502 bad gateway", "HTTP 502: Bad gateway (https://api.github.com/graphql)", true},
		{"500 internal", "HTTP 500: Internal server error (https://api.github.com/graphql)", true},
		{"401 is the caller's problem", "HTTP 401: Bad credentials (https://api.github.com/graphql)", false},
		{"403 rate limit is the caller's problem", "HTTP 403: API rate limit exceeded (https://api.github.com/graphql)", false},
		{"no PR is not a server error", `no pull requests found for branch "feature-x"`, false},
		{"empty stderr", "", false},
	}
	for _, tc := range cases {
		if got := ghServerError(tc.stderr); got != tc.want {
			t.Errorf("%s: ghServerError = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// stubGHScript puts a `gh` on PATH backed by the given shell script body.
func stubGHScript(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "gh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write gh stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// A 503 on the first call must not be read as "no PR" — it should be retried,
// and the PR found on the second attempt returned as if nothing happened.
func TestGHPRForBranch_RetriesServerErrorThenSucceeds(t *testing.T) {
	state := t.TempDir()
	stubGHScript(t, fmt.Sprintf(`
count=%q
if [ ! -f "$count" ]; then
  touch "$count"
  echo 'HTTP 503: No server is currently available (https://api.github.com/graphql)' >&2
  exit 1
fi
echo '{"number":42,"url":"https://example.com/pull/42","state":"OPEN","title":"t","isDraft":false}'
`, filepath.Join(state, "count")))

	pr, err := ghPRForBranch("feature")
	if err != nil {
		t.Fatalf("ghPRForBranch after transient 503: %v", err)
	}
	if pr == nil || pr.Number != 42 {
		t.Fatalf("ghPRForBranch = %+v, want PR #42", pr)
	}
}

// "no pull requests found" is a real answer, not a failure: nil PR, nil error,
// no retries.
func TestGHPRForBranch_NoPRIsNotAnError(t *testing.T) {
	stubGHScript(t, `echo 'no pull requests found for branch "feature"' >&2
exit 1
`)

	pr, err := ghPRForBranch("feature")
	if err != nil {
		t.Fatalf("ghPRForBranch on no-PR branch: %v", err)
	}
	if pr != nil {
		t.Fatalf("ghPRForBranch = %+v, want nil", pr)
	}
}

// A non-transient failure (bad credentials) must surface as an error — the old
// behavior of reading it as "no PR" is what offered to create duplicate PRs.
func TestGHPRForBranch_AuthFailureIsAnError(t *testing.T) {
	stubGHScript(t, `echo 'HTTP 401: Bad credentials (https://api.github.com/graphql)' >&2
exit 1
`)

	pr, err := ghPRForBranch("feature")
	if err == nil {
		t.Fatalf("ghPRForBranch on auth failure returned %+v, want error", pr)
	}
}
