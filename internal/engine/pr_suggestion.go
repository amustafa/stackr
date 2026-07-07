package engine

import (
	"strings"

	"github.com/amustafa/stackr/internal/graph"
)

// prSuggestionKey is the reserved Branch Context key a sandbox sets (via
// `sr context set pr ...`) to propose a PR, consumed by submit (ADR-0010).
const prSuggestionKey = "pr"

// lookupPRSuggestion returns the proposed PR title/body from the reserved `pr`
// context entry on a branch, if present.
func lookupPRSuggestion(b *graph.BranchState) (title, body string, ok bool) {
	if b == nil {
		return "", "", false
	}
	for _, ce := range b.Context {
		if ce.Key == prSuggestionKey {
			t, bd := parsePRSuggestion(ce.Text)
			return t, bd, true
		}
	}
	return "", "", false
}

// parsePRSuggestion splits a suggestion into title (first line) and body (the
// remainder), trimming surrounding whitespace and a leading blank line.
func parsePRSuggestion(text string) (title, body string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ""
	}
	parts := strings.SplitN(text, "\n", 2)
	title = strings.TrimSpace(parts[0])
	if len(parts) > 1 {
		body = strings.TrimSpace(parts[1])
	}
	return title, body
}
