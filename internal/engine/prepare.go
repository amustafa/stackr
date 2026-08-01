package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/graph"
)

// AIPrepareCommit holds a single commit's metadata.
type AIPrepareCommit struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
}

// AIPrepareDiffCommand points the consumer at the branch's diff instead of
// carrying it.
//
// The patch is by far the largest thing we could put in this JSON and the
// consumer is an agent with a finite context window. Most PR descriptions can
// be written from the description, commits and recorded context alone, so
// embedding the patch taxed every caller for something only some of them
// needed. Handing over the command lets the agent pay that cost only when it
// decides it has to see the changes.
type AIPrepareDiffCommand struct {
	Command string `json:"command"`
	Note    string `json:"note"`
}

// AIPrepareResult holds all context an agent needs to craft a PR.
type AIPrepareResult struct {
	// Prompt carries the system prompt in --aiprepare mode, where the caller is
	// an agent already in session and the JSON is all it gets. The --ai path
	// leaves it empty because the same text is passed as --append-system-prompt,
	// so omitempty keeps an empty key out of the payload piped to that session.
	Prompt      string                `json:"prompt,omitempty"`
	Branch      string                `json:"branch"`
	Parent      string                `json:"parent"`
	Description string                `json:"description,omitempty"`
	Context     []graph.BranchContext `json:"context,omitempty"`
	Commits     []AIPrepareCommit     `json:"commits,omitempty"`
	DiffCommand *AIPrepareDiffCommand `json:"diffCommand,omitempty"`
	ExistingPR  *PRResult             `json:"existingPR,omitempty"`
	PRTemplate  string                `json:"prTemplate,omitempty"`
}

// PrepareAI gathers all the context needed to create or update a PR.
func PrepareAI(c *context.Context) (*AIPrepareResult, error) {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return nil, err
	}

	current, err := c.Git.CurrentBranch()
	if err != nil {
		return nil, err
	}

	b := g.Branches[current]
	if b == nil {
		return nil, fmt.Errorf("branch %q not found in stack graph", current)
	}
	if b.IsTrunk {
		return nil, fmt.Errorf("cannot submit trunk branch")
	}

	result := &AIPrepareResult{
		Branch:      current,
		Parent:      b.ParentBranchName,
		Description: b.Description,
		Context:     b.Context,
	}

	result.DiffCommand = buildDiffCommand(b.ParentBranchName, current)

	commits, _ := c.Git.CommitsBetween(b.ParentBranchName, current)
	for _, entry := range commits {
		result.Commits = append(result.Commits, AIPrepareCommit{
			SHA:     entry.SHA[:min(7, len(entry.SHA))],
			Subject: entry.Subject,
		})
	}

	existing, _ := ghPRForBranch(current)
	if existing != nil {
		result.ExistingPR = existing
	}

	result.PRTemplate = findPRTemplate(c)

	return result, nil
}

// buildDiffCommand renders the command that reproduces the patch this result
// used to embed. The revision range must stay identical to the one
// git.DiffPatch runs (two-dot, parent..branch) — a three-dot range would show
// the branch against the merge base instead of against the parent's tip, which
// is a different change set the moment the parent moves ahead.
func buildDiffCommand(parent, branch string) *AIPrepareDiffCommand {
	return &AIPrepareDiffCommand{
		Command: "git diff " + shellArg(parent+".."+branch),
		Note: "The diff is not included in this JSON to keep it small. " +
			"Run this command to see the full patch of this branch against its parent.",
	}
}

// shellArg quotes s only when it holds something a shell would interpret.
// Branch names are almost always plain, and an unquoted `git diff a..b` is the
// command a human would type — quoting every name would make the common case
// harder to read for no gain.
func shellArg(s string) string {
	const safe = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._/@+-"
	if s != "" && strings.Trim(s, safe) == "" {
		return s
	}
	return shellQuote(s)
}

func findPRTemplate(c *context.Context) string {
	repoRoot, err := c.Git.RepoRoot()
	if err != nil {
		return ""
	}

	candidates := []string{
		".github/PULL_REQUEST_TEMPLATE.md",
		".github/pull_request_template.md",
		"PULL_REQUEST_TEMPLATE.md",
		"pull_request_template.md",
		"docs/pull_request_template.md",
	}

	for _, candidate := range candidates {
		data, err := os.ReadFile(filepath.Join(repoRoot, candidate))
		if err == nil {
			return string(data)
		}
	}
	return ""
}

// BuildAISystemPrompt returns the system prompt for the Claude session
// spawned by sr submit --ai.
func BuildAISystemPrompt() string {
	var b strings.Builder
	b.WriteString("You are a PR submission assistant for stackr, a stacked-branch git workflow.\n\n")
	b.WriteString("You are given JSON containing this branch's info, commits, context entries, and optionally an existing PR.\n\n")
	b.WriteString("The diff is NOT included — it would be the largest thing in the JSON and you often will not need it. ")
	b.WriteString("The `diffCommand` field holds the command that prints this branch's full patch against its parent; run it yourself if the description, commits and context leave you unsure what actually changed.\n\n")
	b.WriteString("This branch is ONE branch in a stack. It builds on its parent, and its parent is a separate PR reviewed on its own. ")
	b.WriteString("Describe only THIS branch's change — do not summarize the whole stack or the parent's work. ")
	b.WriteString("The `context` entries are design decisions the author recorded with `sr context`; use them to explain the why.\n\n")
	b.WriteString("Your job:\n")
	b.WriteString("1. Read the JSON carefully.\n")
	b.WriteString("2. If an existing PR is present, note its current title and body — you may update or keep them.\n")
	b.WriteString("3. Generate a concise PR title (no prefix like 'feat:' unless the project uses conventional commits).\n")
	b.WriteString("4. Generate a PR body in markdown. If a prTemplate is provided, fill it in. Otherwise use:\n")
	b.WriteString("   ## Summary\n   <what changed and why>\n\n   ## Changes\n   <bulleted list>\n\n   ## Test Plan\n   <how to verify>\n\n")
	b.WriteString("   Describe the change in prose. Do NOT paste raw diffs or `git --stat` output into the body — keep it focused on what changed and why.\n\n")
	b.WriteString("5. Run the following command to submit the PR:\n")
	b.WriteString("   sr submit --title '<title>' --body '<body>'\n\n")
	b.WriteString("   If the body is long, write it to a temp file and use:\n")
	b.WriteString("   sr submit --title '<title>' --body-file /tmp/pr-body.md\n\n")
	b.WriteString("6. After the command succeeds, you are done. Do not run any other commands.\n")
	return b.String()
}
