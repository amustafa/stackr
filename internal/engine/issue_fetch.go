package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// fetchIssue resolves an Issue from its source. Fetching happens host-side
// (ADR-0013) so no credentials or network reach the sandbox.
func fetchIssue(source Source, ref string, withComments bool) (Issue, error) {
	switch source {
	case SourceGitHub:
		return fetchGitHubIssue(ref, withComments)
	case SourceJira:
		return fetchJiraIssue(ref, withComments)
	default:
		return Issue{}, fmt.Errorf("unknown issue source %q", source)
	}
}

// --- GitHub (gh) ---

func fetchGitHubIssue(ref string, withComments bool) (Issue, error) {
	if err := ghCheckInstalled(); err != nil {
		return Issue{}, err
	}
	fields := "title,body,labels,url"
	if withComments {
		fields += ",comments"
	}
	out, err := runIssueCmd("gh", "issue", "view", ref, "--json", fields)
	if err != nil {
		return Issue{}, fmt.Errorf("fetching GitHub issue %s: %w", ref, err)
	}
	return parseGitHubIssue([]byte(out), ref, withComments)
}

type ghIssueJSON struct {
	Title  string `json:"title"`
	Body   string `json:"body"`
	URL    string `json:"url"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Comments []struct {
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		Body string `json:"body"`
	} `json:"comments"`
}

// parseGitHubIssue maps `gh issue view --json ...` output onto an Issue.
func parseGitHubIssue(data []byte, ref string, withComments bool) (Issue, error) {
	var j ghIssueJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return Issue{}, fmt.Errorf("parsing gh output: %w", err)
	}
	iss := Issue{
		Ref:    ref,
		Source: SourceGitHub,
		Title:  j.Title,
		Body:   j.Body,
		URL:    j.URL,
	}
	for _, l := range j.Labels {
		if l.Name != "" {
			iss.Labels = append(iss.Labels, l.Name)
		}
	}
	if withComments && len(j.Comments) > 0 {
		var b strings.Builder
		for i, c := range j.Comments {
			if i > 0 {
				b.WriteString("\n\n")
			}
			author := c.Author.Login
			if author == "" {
				author = "unknown"
			}
			fmt.Fprintf(&b, "@%s: %s", author, strings.TrimSpace(c.Body))
		}
		iss.Comments = b.String()
	}
	return iss, nil
}

// --- Jira (jira-cli) ---

func jiraCheckInstalled() error {
	if _, err := exec.LookPath("jira"); err != nil {
		return fmt.Errorf("jira CLI not found — install it from https://github.com/ankitpokhrel/jira-cli")
	}
	return nil
}

func fetchJiraIssue(key string, withComments bool) (Issue, error) {
	if err := jiraCheckInstalled(); err != nil {
		return Issue{}, err
	}
	// --raw gives the plain-string summary + labels (no ADF); --plain renders
	// the description (and optional comments) to text for us (ADR-0013).
	raw, err := runIssueCmd("jira", "issue", "view", key, "--raw")
	if err != nil {
		return Issue{}, fmt.Errorf("fetching Jira issue %s: %w", key, err)
	}
	title, labels, err := parseJiraRaw([]byte(raw))
	if err != nil {
		return Issue{}, err
	}

	plainArgs := []string{"issue", "view", key, "--plain"}
	if withComments {
		plainArgs = append(plainArgs, "--comments", "10")
	}
	plain, err := runIssueCmd("jira", plainArgs...)
	if err != nil {
		return Issue{}, fmt.Errorf("rendering Jira issue %s: %w", key, err)
	}

	return Issue{
		Ref:    key,
		Source: SourceJira,
		Title:  title,
		Body:   strings.TrimSpace(stripANSI(plain)),
		Labels: labels,
	}, nil
}

type jiraRawJSON struct {
	Fields struct {
		Summary string   `json:"summary"`
		Labels  []string `json:"labels"`
	} `json:"fields"`
}

// parseJiraRaw pulls the summary (title) and labels from `jira issue view --raw`.
// Both are plain strings in the API response — the ADF description is left to
// --plain, never rendered here.
func parseJiraRaw(data []byte) (title string, labels []string, err error) {
	var j jiraRawJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return "", nil, fmt.Errorf("parsing jira --raw output: %w", err)
	}
	return j.Fields.Summary, j.Fields.Labels, nil
}

// --- shared ---

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

// runIssueCmd runs an issue-source CLI and returns stdout, wrapping failures
// with the tool's stderr (mirrors the gh helpers in github.go).
func runIssueCmd(bin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ghTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(cmd.Environ(), "GH_PROMPT_DISABLED=1", "NO_COLOR=1")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("%s timed out after %s", bin, ghTimeout)
		}
		return "", fmt.Errorf("%s failed: %s: %w", bin, strings.TrimSpace(stderr.String()), err)
	}
	return stdout.String(), nil
}
