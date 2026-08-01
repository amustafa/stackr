package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/amustafa/stackr/internal/context"
	gitpkg "github.com/amustafa/stackr/internal/git"
	"github.com/amustafa/stackr/internal/graph"
	"github.com/amustafa/stackr/internal/store"
	"github.com/amustafa/stackr/internal/ui"
)

// SubmitOpts controls push/PR behavior.
type SubmitOpts struct {
	Draft      bool
	Stack      bool // Push all branches in the stack
	UpdateOnly bool // Only update already-pushed branches

	// Force answers every Content Divergence with "overwrite remote" instead of
	// prompting. It does NOT skip preflight and does NOT relax the lease: the
	// push is still pinned to the commit that was inspected, so a remote that
	// moves after the decision still rejects.
	Force bool
	// NoForce refuses to force-push at all, failing instead. Mutually exclusive
	// with Force.
	NoForce bool

	DryRun bool
	Reviewers  []string
	Title      string // PR title (programmatic mode — skip interactive)
	Body       string // PR body (programmatic mode — skip interactive)
	BodyFile   string // Read PR body from file instead of --body
	AI         bool   // Spawn Claude session to own the submit flow
	AIPrepare  bool   // Output JSON context and exit (no push, no PR)

	// PRMeta holds the raw --pr-meta values: JSON blobs, or paths to files of
	// JSON, each carrying one branch's PR content. A submit creates a PR for
	// every branch it pushes, so PR content has to be addressable per branch;
	// --title/--body can only ever describe one of them.
	PRMeta []string
}

// Submit pushes branches to the remote and manages PRs.
func Submit(c *context.Context, opts SubmitOpts) error {
	// Mode 1a: --aiprepare outputs JSON context and exits.
	if opts.AIPrepare {
		return submitAIPrepare(c, opts)
	}

	// Mode 3: --ai spawns a Claude session that owns the flow.
	if opts.AI {
		return submitAI(c, opts)
	}

	// Resolve body from file if specified.
	if opts.BodyFile != "" {
		data, err := os.ReadFile(opts.BodyFile)
		if err != nil {
			return fmt.Errorf("could not read body file: %w", err)
		}
		opts.Body = string(data)
	}

	// --title/--body is the one-branch shorthand for --pr-meta, and is folded
	// into it here so there is a single path from here on. The two are mutually
	// exclusive at the flag layer, so only one of them ever contributes.
	prMeta, err := ParsePRMeta(opts.PRMeta)
	if err != nil {
		return err
	}
	if opts.Title != "" {
		prMeta = append(prMeta, PRMeta{Title: opts.Title, Body: opts.Body})
	}

	if err := ghCheckInstalled(); err != nil {
		return err
	}

	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}
	cfg, err := c.Store.ReadConfig()
	if err != nil {
		return err
	}

	current, err := c.Git.CurrentBranch()
	if err != nil {
		return err
	}

	prInfo, err := c.Store.ReadPRInfo()
	if err != nil {
		return err
	}

	if opts.Force && opts.NoForce {
		return fmt.Errorf("--force and --no-force are mutually exclusive")
	}

	set, dropped, err := buildPushSet(c, g, cfg, opts, current)
	if err != nil {
		return err
	}
	reportDropped(c, dropped)
	if len(set) == 0 {
		fmt.Println("Nothing to submit")
		return nil
	}

	// Catch a misspelt branch name here, while nothing has been pushed. The rest
	// of the --pr-meta validation needs the PR survey and runs after the push.
	if err := CheckPRMetaBranches(prMeta, set); err != nil {
		return err
	}

	if opts.DryRun {
		return dryRunReport(c, cfg, set)
	}

	pre, err := Preflight(c, opts, cfg, set)
	if err != nil {
		return err
	}
	reportDropped(c, pre.Dropped)

	if pre.Stopped {
		fmt.Println("\nStopped. Nothing was pushed.")
		if pre.Mutated && pre.RollbackID != "" {
			fmt.Printf("Local branches were changed. To put them back: sr rollback %s\n", pre.RollbackID)
		}
		return nil
	}

	pushed, err := pushPhase(c, cfg, pre.Ready)
	if err != nil {
		return err
	}

	return reconcilePhase(c, opts, cfg, prInfo, current, pushed, prMeta)
}

// reportDropped names every branch left out of this submit. An operation that
// silently treats one branch differently from its neighbours is
// indistinguishable from a bug.
func reportDropped(c *context.Context, dropped []DroppedBranch) {
	if c.Quiet || len(dropped) == 0 {
		return
	}
	for _, d := range dropped {
		fmt.Printf("Skipping %s (%s)\n", d.Name, d.Reason)
	}
}

// dryRunReport classifies without touching anything — no fetch side effects
// beyond updating remote-tracking refs, no remediation, no restack, no push.
func dryRunReport(c *context.Context, cfg *store.Config, set []string) error {
	if err := c.Git.Fetch(cfg.Remote); err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}
	fmt.Println("[dry-run] nothing will be changed locally or on the remote")
	for _, name := range set {
		class, err := ClassifyBranch(c, cfg.Remote, name)
		if err != nil {
			return err
		}
		switch class.Disposition {
		case DispNeedsDecision:
			fmt.Printf("  %s: needs a decision — %s\n", name, class.Reason)
		case DispPushForce:
			fmt.Printf("  %s: force push (lossless, lease pinned to %s)\n", name, abbrev(class.RemoteSHA))
		default:
			fmt.Printf("  %s: %s\n", name, class.Disposition)
		}
	}
	return nil
}

// pushPhase publishes the settled branches bottom-up.
//
// It re-fetches first: preflight may have sat waiting on a human for a long
// time, and every lease was pinned to what the remote held before that pause.
// If anything moved we publish nothing rather than half a stack.
func pushPhase(c *context.Context, cfg *store.Config, ready []Classification) ([]string, error) {
	if err := c.Git.Fetch(cfg.Remote); err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}

	// Verify every branch before publishing any of them. Letting the lease catch
	// a change later would be safe for that branch but would already have
	// published the ones below it — the partial update this phase exists to
	// avoid. That includes branches we expected to be ABSENT: a ref created
	// during preflight fails the empty-expect lease just as surely.
	for _, class := range ready {
		ref := cfg.Remote + "/" + class.Branch
		now, err := c.Git.RevParse(ref)
		exists := err == nil

		switch {
		case class.RemoteSHA == "" && exists:
			return nil, fmt.Errorf(
				"%s was created on the remote while you were resolving — nothing was pushed.\n"+
					"Run `sr submit` again to re-check it.", class.Branch)
		case class.RemoteSHA != "" && !exists:
			return nil, fmt.Errorf(
				"%s was deleted from the remote while you were resolving — nothing was pushed.\n"+
					"Run `sr submit` again to re-check it.", class.Branch)
		case class.RemoteSHA != "" && now != class.RemoteSHA:
			return nil, fmt.Errorf(
				"%s changed on the remote while you were resolving — nothing was pushed.\n"+
					"Run `sr submit` again to re-check it.", class.Branch)
		}
	}

	var pushed []string
	for _, class := range ready {
		if class.Disposition == DispNoPush {
			if !c.Quiet {
				fmt.Printf("%s: already up to date\n", class.Branch)
			}
			pushed = append(pushed, class.Branch)
			continue
		}

		if !c.Quiet {
			if class.Disposition == DispPushForce {
				fmt.Printf("Force pushing %s -> %s/%s\n", class.Branch, cfg.Remote, class.Branch)
			} else {
				fmt.Printf("Pushing %s -> %s/%s\n", class.Branch, cfg.Remote, class.Branch)
			}
		}

		if err := c.Git.PushPinned(cfg.Remote, class.Branch, class.RemoteSHA, true); err != nil {
			if gitpkg.IsStaleLease(err) {
				return pushed, fmt.Errorf(
					"%w\n\nPushed: %s\nNot pushed: %s\nRun `sr submit` again to re-check the rest.",
					err, joinOrNone(pushed), joinOrNone(remaining(ready, class.Branch)))
			}
			return pushed, err
		}

		// Record what we just left there. This is what makes the next submit of
		// this branch provably safe to force (ADR-0014) — so a failure to write
		// it must be said out loud, not swallowed: without the record, a later
		// rewind of this branch by someone else looks like an ordinary
		// fast-forward and gets silently resurrected.
		newSHA, err := c.Git.RevParse(class.Branch)
		if err == nil {
			err = c.Store.SetPushRecord(cfg.Remote, class.Branch, newSHA)
		}
		if err != nil {
			fmt.Printf("Warning: %s was pushed but its push record could not be saved (%v).\n"+
				"  The next submit of this branch will fall back to comparing content.\n",
				class.Branch, err)
		}
		pushed = append(pushed, class.Branch)
	}

	return pushed, nil
}

func remaining(ready []Classification, from string) []string {
	var out []string
	seen := false
	for _, class := range ready {
		if class.Branch == from {
			seen = true
		}
		if seen {
			out = append(out, class.Branch)
		}
	}
	return out
}

func joinOrNone(names []string) string {
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}

// reconcilePhase brings GitHub in line with what was just published: PR bases,
// a PR for the branch being submitted, then stack registration.
//
// Bottom-up push order guarantees a parent already exists on the remote before
// its child's base is retargeted.
func reconcilePhase(c *context.Context, opts SubmitOpts, cfg *store.Config,
	prInfo *store.PRInfo, current string, pushed []string, prMeta []PRMeta) error {

	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}

	for _, name := range pushed {
		b := g.Branches[name]
		if b == nil {
			continue
		}
		if prInfo.Branches[name] == nil {
			prInfo.Branches[name] = &store.BranchPR{}
		}
		pr := prInfo.Branches[name]

		// A frozen parent that was never published leaves this branch's PR base
		// naming a ref GitHub does not have. Say so against the frozen branch
		// rather than letting GitHub reject this one.
		if parent := g.Branches[b.ParentBranchName]; parent != nil && parent.Frozen {
			if exists, _ := c.Git.RemoteBranchExists(cfg.Remote, b.ParentBranchName); !exists {
				fmt.Printf("Warning: %s is based on frozen branch %s, which has never been pushed — "+
					"its PR cannot be based on it. Unfreeze and submit %s first.\n",
					name, b.ParentBranchName, b.ParentBranchName)
				continue
			}
		}

		if pr.Number != 0 && pr.BaseBranch != b.ParentBranchName {
			if err := ghUpdatePRBase(pr.Number, b.ParentBranchName); err != nil && c.Debug {
				fmt.Printf("Note: could not retarget PR #%d to %s yet: %v\n", pr.Number, b.ParentBranchName, err)
			}
		}
		pr.BaseBranch = b.ParentBranchName
		if pr.State == "" {
			pr.State = "open"
		}
	}

	// Every branch that was pushed needs a PR, not just the one the user is
	// standing on. A branch left without one is a hole in the stack: the PR
	// above it is opened against a ref nobody is reviewing, and the GitHub stack
	// chain — which is built from PRs — silently skips over it.
	//
	// pushed is bottom-up (see buildPushSet), so each base already has its PR by
	// the time its child is created.
	eligible, existing, needPR, err := surveyPRs(g, pushed)
	if err != nil {
		return err
	}

	// Bind before creating anything. A --pr-meta payload naming a branch this
	// submit does not touch, or leaving a branch ambiguous, is a mistake in the
	// whole job — reporting it after two of five PRs already exist leaves a
	// half-built stack to unpick by hand.
	content, err := bindPRMeta(prMeta, eligible, needPR, current)
	if err != nil {
		return err
	}

	for _, name := range pushed {
		if err := ensurePR(c, opts, g, prInfo, name, existing[name], content[name]); err != nil {
			return err
		}
	}

	syncGitHubStacks(g, prInfo, pushed, c.Quiet, c.Interactive)

	if err := c.Store.WritePRInfo(prInfo); err != nil {
		return err
	}
	TryPushMeta(c)
	return nil
}

// surveyPRs asks GitHub, once, which of the pushed branches already have a pull
// request.
//
// Hoisted out of ensurePR so the answer is known before the first PR is created.
// bindPRMeta needs the full list of branches still needing one to decide whether
// a branchless --pr-meta entry is unambiguous, and that question has to be
// settled before any of those PRs exist to change the answer.
//
// eligible is every pushed branch that could carry a PR (trunk and branches the
// graph does not know about cannot). needPR is the subset with none yet, in the
// same bottom-up order as pushed.
func surveyPRs(g *graph.Graph, pushed []string) (eligible []string, existing map[string]*PRResult, needPR []string, err error) {
	existing = make(map[string]*PRResult, len(pushed))

	for _, name := range pushed {
		b := g.Branches[name]
		if b == nil || b.IsTrunk {
			continue
		}
		eligible = append(eligible, name)

		pr, err := ghPRForBranch(name)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to check PR status for %s: %w", name, err)
		}
		if pr != nil {
			existing[name] = pr
			continue
		}
		needPR = append(needPR, name)
	}

	return eligible, existing, needPR, nil
}

// ensurePR makes sure branch has a PR, creating one if it does not.
//
// meta is this branch's --pr-meta entry, or nil if the user supplied none for
// it. Content is addressed per branch rather than applied to whichever branch
// submit was invoked on: a submit creates a PR for every branch it pushes, and
// one title cannot describe several changes.
func ensurePR(c *context.Context, opts SubmitOpts, g *graph.Graph, prInfo *store.PRInfo,
	branch string, existing *PRResult, meta *PRMeta) error {

	b := g.Branches[branch]
	if b == nil || b.IsTrunk {
		return nil
	}

	if existing != nil {
		if prInfo.Branches[branch] == nil {
			prInfo.Branches[branch] = &store.BranchPR{}
		}
		pr := prInfo.Branches[branch]
		pr.Number = existing.Number
		pr.URL = existing.URL
		pr.State = existing.State
		pr.Title = existing.Title
		pr.Draft = existing.Draft
		if !c.Quiet {
			fmt.Printf("PR #%d for %s (%s)\n", existing.Number, branch, existing.URL)
		}
		return applyPRMetaToExisting(c, branch, existing, meta)
	}

	var title, body string
	if meta != nil {
		title, body = meta.Title, meta.Body
	}

	if title == "" {
		if !c.Interactive {
			if !c.Quiet {
				fmt.Printf("No PR for %s and nothing to build one from — pushed without creating a PR.\n", branch)
				fmt.Printf("  Without one the stack has a gap here: the PRs above %s open against a ref nobody reviews.\n", branch)
				fmt.Printf("  Pass content for it with --pr-meta '{\"branch\":%q,\"title\":\"...\",\"bodyFile\":\"...\"}'.\n", branch)
			}
			return nil
		}

		choice, err := ui.Select("No PR exists for "+branch, []string{"Push only", "Create PR"})
		if err != nil {
			return err
		}
		if choice == "Push only" {
			return nil
		}

		// Consume a sandbox-deposited PR Suggestion (reserved `pr` context
		// entry) as editable defaults, if present (ADR-0010).
		sugTitle, sugBody, hasSug := lookupPRSuggestion(b)
		if hasSug && !c.Quiet {
			fmt.Println("Using PR suggestion from branch context (edit as needed).")
		}
		prompt := "PR title"
		if sugTitle != "" {
			prompt = fmt.Sprintf("PR title [%s]", sugTitle)
		}
		title, err = ui.Input(prompt)
		if err != nil {
			return err
		}
		if title == "" {
			title = sugTitle
		}
		if title == "" {
			return fmt.Errorf("PR title cannot be empty")
		}
		body, err = ui.EditText(sugBody)
		if err != nil {
			return fmt.Errorf("editor failed: %w", err)
		}
	}

	draft := meta.DraftFor(opts.Draft)
	result, err := ghCreatePR(GHCreateOpts{
		Base:  b.ParentBranchName,
		Head:  branch,
		Title: title,
		Body:  body,
		Draft: draft,
	})
	if err != nil {
		return err
	}
	storePRResult(prInfo, branch, b.ParentBranchName, result, draft)
	if !c.Quiet {
		fmt.Printf("Created PR #%d: %s\n", result.Number, result.URL)
	}
	return nil
}

// applyPRMetaToExisting handles a --pr-meta entry naming a branch whose pull
// request already exists.
//
// The policy is to leave the pull request alone. A PR that exists already has a
// description, and the only safe assumption about it is that someone meant what
// it says — a reviewer's checklist, an edit made in the web UI, a summary
// rewritten after discussion. `sr submit` runs on every push, so a payload that
// overwrote would replay that overwrite for as long as the file sits on disk;
// the damage is silent, repeated, and lands on exactly the branches furthest
// through review.
//
// The cost is that a genuinely stale description stays stale, and that is
// accepted: rewriting a PR body is a deliberate act, and `gh pr edit` is already
// the tool for it. Nothing here is silent — a submit that swallowed content the
// user typed would be the same failure in the other direction.
func applyPRMetaToExisting(c *context.Context, branch string, existing *PRResult, meta *PRMeta) error {
	if meta == nil || c.Quiet {
		return nil
	}

	fmt.Printf("  Kept the existing title and body — the --pr-meta entry for %s was not applied.\n", branch)
	fmt.Printf("  To replace them deliberately: gh pr edit %d --title '...' --body-file ...\n", existing.Number)
	return nil
}

// submitAIPrepare gathers context and outputs JSON to stdout.
func submitAIPrepare(c *context.Context, opts SubmitOpts) error {
	result, err := PrepareAI(c, opts)
	if err != nil {
		return err
	}
	result.Prompt = BuildAISystemPrompt()
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal aiprepare result: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

// submitAI spawns a Claude session to generate and submit a PR.
func submitAI(c *context.Context, opts SubmitOpts) error {
	if _, err := exec.LookPath("claude"); err != nil {
		return fmt.Errorf("claude CLI not found — install it from https://claude.ai/code")
	}

	result, err := PrepareAI(c, opts)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal context: %w", err)
	}

	systemPrompt := BuildAISystemPrompt()
	goal := "/goal PR is created with a title and description, and the branch is pushed"
	if opts.DryRun {
		goal += ". This is a dry-run — show what you would do but do not push or create the PR"
	}
	if opts.Draft {
		goal += ". Mark the PR as a draft (add --draft flag)"
	}

	// Write, not just Edit: the flow asks the session to create a body file per
	// branch plus the --pr-meta payload, and Edit cannot create a new file.
	allowedTools := "Read,Edit,Write,Bash(sr *),Bash(git *),Bash(gh *)"
	request := buildAIRequest(goal, data)

	if c.Debug || opts.DryRun {
		echoAIRequest("submit", allowedTools, systemPrompt, request)
	}

	cmd := exec.Command("claude", claudeAgentArgs(allowedTools, systemPrompt)...)
	cmd.Stdin = strings.NewReader(request)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if !c.Quiet {
		fmt.Println("Launching Claude to generate and submit PR...")
	}

	return cmd.Run()
}

// buildAIRequest joins the goal and its JSON context into the single message
// piped to the spawned session.
//
// The goal used to travel as a `-p <string>` argument while the JSON came in on
// stdin, which split one request across two channels: the prompt was subject to
// argv length limits and shell quoting, and nothing tied the two halves
// together. Piping the whole thing keeps it one document, and `claude` runs
// non-interactively on its own when stdin is not a TTY, so no print flag is
// needed to get there.
func buildAIRequest(goal string, contextJSON []byte) string {
	return goal + "\n\n" + string(contextJSON) + "\n"
}

// claudeAgentArgs builds the argv for a spawned Claude session.
//
// Deliberately no --bare. It is tempting — it skips hooks, plugin sync and
// CLAUDE.md auto-discovery, which is what you want for a scripted one-shot —
// but it also refuses to read OAuth or the keychain, taking credentials
// strictly from ANTHROPIC_API_KEY or an apiKeyHelper. Anyone signed in with
// `claude /login` has neither, so --bare fails with "Not logged in" for every
// user on a subscription.
func claudeAgentArgs(allowedTools, systemPrompt string) []string {
	return []string{
		"--allowedTools", allowedTools,
		"--append-system-prompt", systemPrompt,
	}
}

// echoAIRequest prints everything about to be handed to the spawned session:
// the tools it may use, the appended system prompt, and the piped request.
//
// Without this the only visible output is the agent's own, which makes a
// surprising run impossible to attribute — you cannot tell whether the agent
// reasoned badly or was handed the wrong context, because the context was never
// shown. It goes to stderr so stdout stays usable for whatever the agent emits.
func echoAIRequest(command, allowedTools, systemPrompt, request string) {
	w := os.Stderr
	fmt.Fprintf(w, "\n--- sr %s --ai: request to claude ---\n", command)
	fmt.Fprintf(w, "allowedTools:\n%s\n\n", allowedTools)
	fmt.Fprintf(w, "appended system prompt:\n%s\n", systemPrompt)
	fmt.Fprintf(w, "piped request:\n%s", request)
	fmt.Fprintf(w, "--- end request ---\n\n")
}

// storePRResult updates local PR metadata from a GitHub PR result.
func storePRResult(prInfo *store.PRInfo, branch, parent string, result *PRResult, draft bool) {
	if prInfo.Branches[branch] == nil {
		prInfo.Branches[branch] = &store.BranchPR{}
	}
	pr := prInfo.Branches[branch]
	pr.Number = result.Number
	pr.URL = result.URL
	pr.State = result.State
	pr.Title = result.Title
	pr.Draft = draft
	pr.BaseBranch = parent
}
