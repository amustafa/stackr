package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// PRMeta is the pull-request content for one branch of a submit.
//
// A submit acts on a whole stack and may create a PR for every branch in it, so
// PR content has to be addressable per branch. --title/--body could only ever
// describe one change, which left every branch below the target either prompted
// for interactively or pushed without a PR at all.
type PRMeta struct {
	// Branch names the branch this content belongs to. Optional only when the
	// submit creates exactly one PR — see bindPRMeta for why that is the only
	// case where leaving it out is unambiguous.
	Branch string `json:"branch,omitempty"`

	Title string `json:"title"`
	Body  string `json:"body,omitempty"`

	// BodyFile reads the body from a file instead. PR bodies are long, contain
	// backticks and newlines, and are usually generated — quoting one into a
	// shell argument is where that breaks.
	BodyFile string `json:"bodyFile,omitempty"`

	// Draft overrides the submit-wide --draft for this branch. A pointer so
	// "absent" is distinguishable from an explicit false, which is what lets a
	// single entry opt out of a --draft submit.
	Draft *bool `json:"draft,omitempty"`
}

// ParsePRMeta reads every --pr-meta value into a flat list of entries.
//
// Each value is either inline JSON or a path to a file of JSON, and either form
// may hold one object or an array of them. The distinction is made on the first
// non-space character rather than on a prefix like @: a value starting with {
// or [ cannot be a sensible filename, and requiring a sigil would be one more
// thing to get wrong in the generated command line this flag exists to serve.
//
// Bodies are resolved here, at parse time, so an unreadable bodyFile is
// reported before anything is pushed rather than halfway through creating a
// stack's worth of pull requests.
func ParsePRMeta(values []string) ([]PRMeta, error) {
	var out []PRMeta

	for _, raw := range values {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return nil, fmt.Errorf("--pr-meta was given an empty value")
		}

		source := "inline JSON"
		data := []byte(trimmed)
		if trimmed[0] != '{' && trimmed[0] != '[' {
			file, err := os.ReadFile(trimmed)
			if err != nil {
				return nil, fmt.Errorf("could not read --pr-meta file %q: %w", trimmed, err)
			}
			source = trimmed
			data = file
		}

		entries, err := decodePRMeta(data)
		if err != nil {
			return nil, fmt.Errorf("invalid --pr-meta (%s): %w", source, err)
		}

		for _, e := range entries {
			if err := e.resolveBody(); err != nil {
				return nil, err
			}
			if e.Title == "" {
				return nil, fmt.Errorf("--pr-meta entry for %q has no \"title\"", e.describe())
			}
			out = append(out, e)
		}
	}

	return out, nil
}

// decodePRMeta accepts either a single object or an array of them, so a caller
// writing content for one branch is not made to wrap it in brackets.
func decodePRMeta(data []byte) ([]PRMeta, error) {
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "[") {
		var many []PRMeta
		if err := json.Unmarshal([]byte(trimmed), &many); err != nil {
			return nil, err
		}
		return many, nil
	}

	var one PRMeta
	if err := json.Unmarshal([]byte(trimmed), &one); err != nil {
		return nil, err
	}
	return []PRMeta{one}, nil
}

// resolveBody folds bodyFile into body.
func (m *PRMeta) resolveBody() error {
	if m.BodyFile == "" {
		return nil
	}
	if m.Body != "" {
		return fmt.Errorf("--pr-meta entry for %q sets both \"body\" and \"bodyFile\"", m.describe())
	}

	data, err := os.ReadFile(m.BodyFile)
	if err != nil {
		return fmt.Errorf("could not read bodyFile %q for %q: %w", m.BodyFile, m.describe(), err)
	}
	m.Body = string(data)
	m.BodyFile = ""
	return nil
}

// describe names an entry in an error message. A branchless entry has only its
// title to be known by, and an entry with neither is what the message is about.
func (m *PRMeta) describe() string {
	switch {
	case m.Branch != "":
		return m.Branch
	case m.Title != "":
		return m.Title
	default:
		return "(no branch, no title)"
	}
}

// DraftFor reports whether this branch's PR should be a draft, given the
// submit-wide default.
func (m *PRMeta) DraftFor(submitDefault bool) bool {
	if m != nil && m.Draft != nil {
		return *m.Draft
	}
	return submitDefault
}

// CheckPRMetaBranches rejects entries naming a branch this submit will not act
// on, before anything is pushed.
//
// The full binding cannot run this early: deciding whether a branchless entry is
// ambiguous means knowing which branches already have a pull request, which is a
// round-trip per branch and only worth paying once the push has settled what the
// set actually is. A misspelt branch name needs none of that, and catching it
// here is the difference between a typo costing a message and a typo costing a
// stack's worth of pushes followed by a message.
func CheckPRMetaBranches(entries []PRMeta, set []string) error {
	inSet := make(map[string]bool, len(set))
	for _, name := range set {
		inSet[name] = true
	}

	for _, e := range entries {
		if e.Branch != "" && !inSet[e.Branch] {
			return errBranchNotSubmitted(e.Branch, set)
		}
	}
	return nil
}

func errBranchNotSubmitted(branch string, set []string) error {
	return fmt.Errorf(
		"--pr-meta names branch %q, which this submit does not act on.\nSubmitting: %s",
		branch, joinOrNone(set))
}

// bindPRMeta maps each entry onto the branch it applies to, rejecting anything
// ambiguous BEFORE a single pull request is created.
//
// submitting is every branch this submit acts on; needPR is the subset with no
// pull request yet, bottom-up; target is the branch the user invoked submit on.
//
// A missing "branch" is allowed only when the submit creates exactly one pull
// request. That is the case where there is nothing to be ambiguous about — the
// user wrote one description and there is one PR for it to describe. The moment
// a second PR is being created, the same entry could reasonably belong to
// either, and silently picking one would label a branch with another branch's
// summary: a wrong PR description is worse than an error, because it survives
// review as though someone meant it.
//
// Validation is deliberately total rather than lazy. Discovering the third
// entry names a branch that is not being submitted, after two PRs already
// exist, leaves a half-built stack that the user has to unpick by hand.
func bindPRMeta(entries []PRMeta, submitting, needPR []string, target string) (map[string]*PRMeta, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	inSubmit := make(map[string]bool, len(submitting))
	for _, name := range submitting {
		inSubmit[name] = true
	}

	bound := make(map[string]*PRMeta, len(entries))
	var loose *PRMeta

	for i := range entries {
		e := &entries[i]

		if e.Branch == "" {
			if loose != nil {
				return nil, fmt.Errorf(
					"two --pr-meta entries have no \"branch\" (%q and %q) — "+
						"only one entry may leave it out, and only when the submit creates a single PR",
					loose.Title, e.Title)
			}
			loose = e
			continue
		}

		if !inSubmit[e.Branch] {
			return nil, errBranchNotSubmitted(e.Branch, submitting)
		}
		if _, dup := bound[e.Branch]; dup {
			return nil, fmt.Errorf("--pr-meta has two entries for branch %q", e.Branch)
		}
		bound[e.Branch] = e
	}

	if loose == nil {
		return bound, nil
	}

	branch := target
	switch {
	case len(needPR) > 1:
		return nil, fmt.Errorf(
			"--pr-meta entry %q has no \"branch\", but this submit creates %d PRs (%s).\n"+
				"Name the branch each entry belongs to.",
			loose.Title, len(needPR), strings.Join(needPR, ", "))
	case len(needPR) == 1:
		// Exactly one PR to create: unambiguous whatever branch the user is
		// standing on, which matters because submit reaches downstack.
		branch = needPR[0]
	}

	if _, dup := bound[branch]; dup {
		return nil, fmt.Errorf(
			"--pr-meta entry %q has no \"branch\" and would bind to %q, which another entry already names",
			loose.Title, branch)
	}
	loose.Branch = branch
	bound[branch] = loose

	return bound, nil
}
