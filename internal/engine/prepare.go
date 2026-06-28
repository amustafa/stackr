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

// AIPrepareResult holds all context an agent needs to craft a PR.
type AIPrepareResult struct {
	Branch      string               `json:"branch"`
	Parent      string               `json:"parent"`
	Description string               `json:"description,omitempty"`
	Context     []graph.BranchContext `json:"context,omitempty"`
	Commits     []AIPrepareCommit    `json:"commits,omitempty"`
	DiffStat    string               `json:"diffStat,omitempty"`
	Diff        string               `json:"diff,omitempty"`
	ExistingPR  *PRResult            `json:"existingPR,omitempty"`
	PRTemplate  string               `json:"prTemplate,omitempty"`
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

	diffStat, _ := c.Git.DiffStat(b.ParentBranchName, current)
	result.DiffStat = diffStat

	diffPatch, _ := c.Git.DiffPatch(b.ParentBranchName, current)
	result.Diff = diffPatch

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
	b.WriteString("You are a PR submission assistant for a stacked-branch git workflow.\n\n")
	b.WriteString("You will receive JSON containing branch info, diff, commits, context entries, and optionally an existing PR.\n\n")
	b.WriteString("Your job:\n")
	b.WriteString("1. Read the JSON carefully.\n")
	b.WriteString("2. If an existing PR is present, note its current title and body — you may update or keep them.\n")
	b.WriteString("3. Generate a concise PR title (no prefix like 'feat:' unless the project uses conventional commits).\n")
	b.WriteString("4. Generate a PR body in markdown. If a prTemplate is provided, fill it in. Otherwise use:\n")
	b.WriteString("   ## Summary\n   <what changed and why>\n\n   ## Changes\n   <bulleted list>\n\n   ## Test Plan\n   <how to verify>\n\n")
	b.WriteString("5. Run the following command to submit the PR:\n")
	b.WriteString("   sr submit --title '<title>' --body '<body>'\n\n")
	b.WriteString("   If the body is long, write it to a temp file and use:\n")
	b.WriteString("   sr submit --title '<title>' --body-file /tmp/pr-body.md\n\n")
	b.WriteString("6. After the command succeeds, you are done. Do not run any other commands.\n")
	return b.String()
}
