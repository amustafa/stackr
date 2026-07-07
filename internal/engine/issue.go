package engine

import (
	"fmt"
	"regexp"
	"strings"
)

// Source identifies where an issue was fetched from.
type Source string

const (
	SourceGitHub Source = "github"
	SourceJira   Source = "jira"
)

// Issue is a source-agnostic representation of a fetched issue/ticket. Ref is
// the normalized identifier: the bare number for GitHub ("123"), the upper-case
// key for Jira ("PROJ-456").
type Issue struct {
	Ref      string
	Source   Source
	Title    string
	Body     string
	Labels   []string
	URL      string
	Comments string // rendered discussion; only populated when requested
}

// displayRef is how the ref reads in prose: "#123" for GitHub, "PROJ-456" for Jira.
func (iss Issue) displayRef() string {
	if iss.Source == SourceGitHub {
		return "#" + iss.Ref
	}
	return iss.Ref
}

// closeInstruction tells the agent how to link its PR back to the issue.
func (iss Issue) closeInstruction() string {
	if iss.Source == SourceGitHub {
		return fmt.Sprintf("end your PR description with \"Closes #%s\"", iss.Ref)
	}
	return fmt.Sprintf("reference %s in your PR description", iss.Ref)
}

var (
	ghURLRe   = regexp.MustCompile(`github\.com/[^/\s]+/[^/\s]+/issues/(\d+)`)
	jiraURLRe = regexp.MustCompile(`/browse/([A-Za-z][A-Za-z0-9]+-\d+)`)
	ghNumRe   = regexp.MustCompile(`^#?(\d+)$`)
	jiraKeyRe = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]+-\d+)$`)
)

// detectSource resolves an issue reference to a source, a normalized display
// ref, and a fetch locator. The display ref is what names the branch and reads
// in prose ("123", "PROJ-456"); the locator is what to hand the source CLI. They
// differ only for a GitHub URL, whose locator stays the full URL so `gh` targets
// the right repo rather than assuming the current one.
//
// override, when non-empty, forces the source ("github" or "jira"), erroring if
// the ref doesn't fit. With no override, the source is auto-detected by shape:
// issue URLs, then #?digits (GitHub), then KEY-N (Jira). Ambiguous input errors.
func detectSource(ref, override string) (src Source, displayRef, locator string, err error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", "", "", fmt.Errorf("an issue reference is required")
	}

	switch strings.ToLower(strings.TrimSpace(override)) {
	case "github":
		n, err := extractGitHubNumber(ref)
		if err != nil {
			return "", "", "", err
		}
		return SourceGitHub, n, githubLocator(ref, n), nil
	case "jira":
		k, err := extractJiraKey(ref)
		if err != nil {
			return "", "", "", err
		}
		return SourceJira, k, k, nil
	case "":
		// auto-detect below
	default:
		return "", "", "", fmt.Errorf("invalid --source %q (want github or jira)", override)
	}

	if m := ghURLRe.FindStringSubmatch(ref); m != nil {
		return SourceGitHub, m[1], ref, nil // locator = the URL (correct repo)
	}
	if m := jiraURLRe.FindStringSubmatch(ref); m != nil {
		key := strings.ToUpper(m[1])
		return SourceJira, key, key, nil
	}
	if m := ghNumRe.FindStringSubmatch(ref); m != nil {
		return SourceGitHub, m[1], m[1], nil
	}
	if m := jiraKeyRe.FindStringSubmatch(ref); m != nil {
		key := strings.ToUpper(m[1])
		return SourceJira, key, key, nil
	}
	return "", "", "", fmt.Errorf("could not determine issue source from %q — pass --source github|jira", ref)
}

// githubLocator returns the URL when the input was one (so gh targets that repo),
// otherwise the bare number (current repo).
func githubLocator(input, number string) string {
	if ghURLRe.MatchString(input) {
		return input
	}
	return number
}

// extractGitHubNumber pulls a GitHub issue number from a URL or a #?digits ref.
func extractGitHubNumber(ref string) (string, error) {
	if m := ghURLRe.FindStringSubmatch(ref); m != nil {
		return m[1], nil
	}
	if m := ghNumRe.FindStringSubmatch(ref); m != nil {
		return m[1], nil
	}
	return "", fmt.Errorf("%q is not a GitHub issue number or URL", ref)
}

// extractJiraKey pulls an upper-cased Jira key from a browse URL or a KEY-N ref.
func extractJiraKey(ref string) (string, error) {
	if m := jiraURLRe.FindStringSubmatch(ref); m != nil {
		return strings.ToUpper(m[1]), nil
	}
	if m := jiraKeyRe.FindStringSubmatch(ref); m != nil {
		return strings.ToUpper(m[1]), nil
	}
	return "", fmt.Errorf("%q is not a Jira key (e.g. PROJ-456) or browse URL", ref)
}

const branchNameCap = 50

// deriveBranchName builds "<ref>-<title-slug>", lower-cased and flat (no
// slashes), capped near branchNameCap characters. Falls back to just the ref
// when the title has no slug-able characters.
func deriveBranchName(iss Issue) string {
	refPart := strings.ToLower(iss.Ref)
	name := refPart
	if slug := slugify(iss.Title); slug != "" {
		name = refPart + "-" + slug
	}
	if len(name) > branchNameCap {
		name = strings.TrimRight(name[:branchNameCap], "-")
	}
	return name
}

// slugify lower-cases s and collapses every run of non-[a-z0-9] into a single
// dash, trimming leading/trailing dashes.
func slugify(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// buildPrompt renders the self-contained implementation brief: a preamble
// (naming the branch and how to close the issue) followed by the issue's title,
// labels, body, and URL. The discussion is appended only when withComments.
func buildPrompt(iss Issue, branch string, withComments bool) string {
	var b strings.Builder

	fmt.Fprintf(&b, "You are implementing %s (%q) on branch %s.\n", iss.displayRef(), iss.Title, branch)
	fmt.Fprintf(&b, "Implement it fully, commit your work with `sr commit`, and %s\n", iss.closeInstruction())
	b.WriteString("(record the PR before finishing with `sr context set pr`).\n\n")

	fmt.Fprintf(&b, "# %s", iss.Title)
	if len(iss.Labels) > 0 {
		fmt.Fprintf(&b, "   [labels: %s]", strings.Join(iss.Labels, ", "))
	}
	b.WriteString("\n\n")

	if body := strings.TrimSpace(iss.Body); body != "" {
		b.WriteString(body)
		b.WriteString("\n\n")
	}
	if iss.URL != "" {
		b.WriteString(iss.URL)
		b.WriteString("\n")
	}
	if withComments {
		if c := strings.TrimSpace(iss.Comments); c != "" {
			b.WriteString("\n--- Discussion ---\n")
			b.WriteString(c)
			b.WriteString("\n")
		}
	}
	return b.String()
}
