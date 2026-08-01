package engine

import (
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

// AIPrepareBranch is one branch of a submit job — everything needed to write
// that branch's PR, on its own terms.
type AIPrepareBranch struct {
	Branch      string                `json:"branch"`
	Parent      string                `json:"parent"`
	Description string                `json:"description,omitempty"`
	Context     []graph.BranchContext `json:"context,omitempty"`
	Commits     []AIPrepareCommit     `json:"commits,omitempty"`
	DiffCommand *AIPrepareDiffCommand `json:"diffCommand,omitempty"`
	ExistingPR  *PRResult             `json:"existingPR,omitempty"`

	// NeedsPR marks a branch with no PR yet. These are the gaps: a PR based on
	// such a branch is opened against a ref nobody is reviewing, which is how a
	// stack ends up with a hole in the middle.
	NeedsPR bool `json:"needsPR"`
}

// AIPrepareResult describes the whole submit job, not just the branch the user
// happens to be standing on.
//
// A submit acts on the current branch AND its downstack ancestors (plus the
// upstack with --stack), and any of them may still be missing a PR. Describing
// only the current branch left the agent unable to see the rest of the job it
// was being asked to complete — it would write one PR and leave the stack with
// a gap it was never told about.
type AIPrepareResult struct {
	// Prompt carries the system prompt in --aiprepare mode, where the caller is
	// an agent already in session and the JSON is all it gets. The --ai path
	// leaves it empty because the same text is passed as --append-system-prompt,
	// so omitempty keeps an empty key out of the payload piped to that session.
	Prompt string `json:"prompt,omitempty"`

	// Target is the branch the user invoked submit on. It is the one branch
	// --title/--body would apply to, and usually the one they care most about.
	Target string `json:"target"`

	// Branches covers the whole job, ordered bottom-up so a branch's parent
	// always precedes it. Creating PRs in this order means every base already
	// has a PR by the time its child is opened.
	Branches   []AIPrepareBranch `json:"branches"`
	PRTemplate string            `json:"prTemplate,omitempty"`
}

// PrepareAI gathers the context for every branch the submit would act on.
//
// It derives that set from buildPushSet — the same function Submit uses — so
// the described job and the performed job cannot drift apart.
func PrepareAI(c *context.Context, opts SubmitOpts) (*AIPrepareResult, error) {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return nil, err
	}
	cfg, err := c.Store.ReadConfig()
	if err != nil {
		return nil, err
	}

	current, err := c.Git.CurrentBranch()
	if err != nil {
		return nil, err
	}

	set, _, err := buildPushSet(c, g, cfg, opts, current)
	if err != nil {
		return nil, err
	}

	result := &AIPrepareResult{
		Target:     current,
		PRTemplate: findPRTemplate(c),
	}

	for _, name := range set {
		b := g.Branches[name]
		if b == nil || b.IsTrunk {
			continue
		}

		entry := AIPrepareBranch{
			Branch:      name,
			Parent:      b.ParentBranchName,
			Description: b.Description,
			Context:     b.Context,
			DiffCommand: buildDiffCommand(b.ParentBranchName, name),
		}

		commits, _ := c.Git.CommitsBetween(b.ParentBranchName, name)
		for _, e := range commits {
			entry.Commits = append(entry.Commits, AIPrepareCommit{
				SHA:     e.SHA[:min(7, len(e.SHA))],
				Subject: e.Subject,
			})
		}

		if existing, _ := ghPRForBranch(name); existing != nil {
			entry.ExistingPR = existing
		} else {
			entry.NeedsPR = true
		}

		result.Branches = append(result.Branches, entry)
	}

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
	b.WriteString("The JSON describes a whole submit job. `branches` lists every branch the submit acts on, ordered bottom-up so each branch's parent comes before it. `target` is the branch the user is standing on. Each entry carries that branch's description, commits, recorded context, any existing PR, and `needsPR`.\n\n")
	b.WriteString("Every branch with `needsPR: true` must end up with a PR. A branch left without one becomes a hole in the stack: the PR above it is opened against a ref nobody is reviewing. Do not stop after the target.\n\n")
	b.WriteString("The diff is NOT included — it would be the largest thing in the JSON and you often will not need it. ")
	b.WriteString("Each branch's `diffCommand` prints its full patch against its parent; run it for a branch whose description, commits and context leave you unsure what actually changed.\n\n")
	b.WriteString("Each branch is reviewed on its own. Describe only THAT branch's change in its PR — do not summarize the whole stack or repeat a parent's work in a child's description. ")
	b.WriteString("The `context` entries are design decisions the author recorded with `sr context`; use them to explain the why.\n\n")
	b.WriteString("Your job:\n")
	b.WriteString("1. Read the JSON carefully and list the branches with `needsPR: true`.\n")
	b.WriteString("2. Work bottom-up, in the order `branches` gives you. Opening a child before its parent has a PR is what creates the hole.\n")
	b.WriteString("3. For each such branch, generate a concise title (no prefix like 'feat:' unless the project uses conventional commits) and a body in markdown. If a prTemplate is provided, fill it in. Otherwise use:\n")
	b.WriteString("   ## Summary\n   <what changed and why>\n\n   ## Changes\n   <bulleted list>\n\n   ## Test Plan\n   <how to verify>\n\n")
	b.WriteString("   Describe the change in prose. Do NOT paste raw diffs or `git --stat` output into the body — keep it focused on what changed and why.\n\n")
	b.WriteString("4. Create each PR by checking the branch out and submitting it:\n")
	b.WriteString("   sr checkout <branch>\n")
	b.WriteString("   sr submit --title '<title>' --body-file /tmp/pr-<branch>.md\n\n")
	b.WriteString("   Write each body to its own temp file — bodies are long and quoting them inline breaks on backticks and newlines.\n\n")
	b.WriteString("5. Where a branch already has a PR, leave it alone unless its title or body is clearly wrong for what the branch now contains.\n")
	b.WriteString("6. Return to the target branch with `sr checkout <target>` when you are done, then stop. Do not run any other commands.\n")
	return b.String()
}
