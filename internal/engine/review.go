package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/git"
	"github.com/amustafa/stackr/internal/ui"
)

// ReviewOpts controls review behavior.
type ReviewOpts struct {
	AI        bool
	AIPrepare bool
	DryRun    bool
}

// BranchReview aggregates review state for one branch's PR.
type BranchReview struct {
	Branch   string         `json:"branch"`
	PRNumber int            `json:"prNumber"`
	PRURL    string         `json:"prUrl"`
	Threads  []ReviewThread `json:"threads"`
}

// ReviewPrepareResult is the --aiprepare output.
type ReviewPrepareResult struct {
	Prompt  string         `json:"prompt"`
	Stack   []BranchReview `json:"stack"`
	Summary string         `json:"summary"`
}

// Review is the entry point for the review command.
func Review(c *context.Context, opts ReviewOpts) error {
	if opts.AIPrepare {
		return reviewAIPrepare(c)
	}
	if opts.AI {
		return reviewAI(c, opts)
	}
	return reviewInteractive(c, opts)
}

// ReviewPrepare gathers unresolved review threads across the stack.
func ReviewPrepare(c *context.Context) (*ReviewPrepareResult, error) {
	if err := ghCheckInstalled(); err != nil {
		return nil, err
	}

	g, err := c.Store.ReadGraph()
	if err != nil {
		return nil, err
	}

	current, err := c.Git.CurrentBranch()
	if err != nil {
		return nil, err
	}

	cfg, err := c.Store.ReadConfig()
	if err != nil {
		return nil, err
	}

	prInfo, err := c.Store.ReadPRInfo()
	if err != nil {
		return nil, err
	}

	repo, err := ghGetRepoInfo()
	if err != nil {
		return nil, err
	}

	// Walk the stack bottom-to-top.
	branches := g.StackOf(current)

	var stack []BranchReview
	totalThreads := 0

	for _, name := range branches {
		b := g.Branches[name]
		if b == nil || b.IsTrunk || b.Frozen {
			continue
		}

		// Find PR number — check stored info first, then GitHub.
		prNumber := 0
		prURL := ""
		if pr := prInfo.Branches[name]; pr != nil && pr.Number > 0 {
			prNumber = pr.Number
			prURL = pr.URL
		} else {
			// Check if branch has been pushed and has a PR.
			exists, _ := c.Git.RemoteBranchExists(cfg.Remote, name)
			if !exists {
				continue
			}
			result, _ := ghPRForBranch(name)
			if result == nil {
				continue
			}
			prNumber = result.Number
			prURL = result.URL
		}

		if !c.Quiet {
			fmt.Printf("Checking %s (PR #%d)...\n", name, prNumber)
		}

		threads, err := ghFetchReviewThreads(repo, prNumber)
		if err != nil {
			if !c.Quiet {
				fmt.Printf("  Warning: could not fetch threads for %s: %v\n", name, err)
			}
			continue
		}

		if len(threads) == 0 {
			continue
		}

		stack = append(stack, BranchReview{
			Branch:   name,
			PRNumber: prNumber,
			PRURL:    prURL,
			Threads:  threads,
		})
		totalThreads += len(threads)
	}

	prCount := len(stack)
	summary := fmt.Sprintf("%d PR(s), %d unresolved thread(s)", prCount, totalThreads)

	return &ReviewPrepareResult{Stack: stack, Summary: summary}, nil
}

// reviewAIPrepare outputs the review context as JSON.
func reviewAIPrepare(c *context.Context) error {
	// stdout is a data channel here (JSON), so suppress the "Checking …"
	// progress prints that ReviewPrepare emits — they would corrupt the JSON.
	c.Quiet = true
	result, err := ReviewPrepare(c)
	if err != nil {
		return err
	}
	result.Prompt = buildReviewAISystemPrompt()
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal review result: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

// reviewAI spawns a Claude session to handle all review comments.
func reviewAI(c *context.Context, opts ReviewOpts) error {
	if _, err := exec.LookPath("claude"); err != nil {
		return fmt.Errorf("claude CLI not found — install it from https://claude.ai/code")
	}

	result, err := ReviewPrepare(c)
	if err != nil {
		return err
	}

	if len(result.Stack) == 0 {
		fmt.Println("No unresolved review threads found")
		return nil
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal review context: %w", err)
	}

	systemPrompt := buildReviewAISystemPrompt()
	goal := "/goal all review comments are addressed with code changes committed, replies posted, threads resolved, and stack restacked"

	if opts.DryRun {
		goal += ". This is a dry-run — show what you would do but do not make changes, post replies, or resolve threads"
	}

	args := []string{
		"--bare",
		"-p", goal,
		"--allowedTools", "Read,Edit,Bash(sr *),Bash(git *),Bash(gh *),Bash(cat *)",
		"--append-system-prompt", systemPrompt,
	}

	cmd := exec.Command("claude", args...)
	cmd.Stdin = strings.NewReader(string(data))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if !c.Quiet {
		fmt.Printf("Launching Claude to address %s\n", result.Summary)
	}

	return cmd.Run()
}

// reviewInteractive walks the stack and presents each comment.
func reviewInteractive(c *context.Context, opts ReviewOpts) error {
	result, err := ReviewPrepare(c)
	if err != nil {
		return err
	}

	if len(result.Stack) == 0 {
		fmt.Println("No unresolved review threads found")
		return nil
	}

	fmt.Printf("Found %s\n\n", result.Summary)

	repo, err := ghGetRepoInfo()
	if err != nil {
		return err
	}

	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}

	addressedTotal := 0

	for _, br := range result.Stack {
		fmt.Printf("━━━ %s (PR #%d) — %d unresolved thread(s) ━━━\n", br.Branch, br.PRNumber, len(br.Threads))
		fmt.Printf("    %s\n\n", br.PRURL)

		// Checkout the branch.
		if err := c.Git.Checkout(br.Branch); err != nil {
			return fmt.Errorf("could not checkout %s: %w", br.Branch, err)
		}

		filesChanged := false
		addressed := 0

		for i, thread := range br.Threads {
			fmt.Printf("  [%d/%d] %s:%d\n", i+1, len(br.Threads), thread.Path, thread.Line)

			// Show all comments in the thread.
			for _, comment := range thread.Comments {
				fmt.Printf("    @%s: %s\n", comment.Author, comment.Body)
			}
			fmt.Println()

			// Show code context around the line.
			showCodeContext(thread.Path, thread.Line)

			const (
				optEdit    = "Edit file"
				optReply   = "Reply & resolve"
				optSkip    = "Skip"
			)

			choice, err := ui.Select("Action", []string{optEdit, optReply, optSkip})
			if err != nil {
				return err
			}

			switch choice {
			case optEdit:
				if err := openEditorAtLine(thread.Path, thread.Line); err != nil {
					fmt.Printf("    Editor failed: %v\n", err)
					continue
				}
				filesChanged = true

				reply, err := ui.Input("Reply (what you changed)")
				if err != nil {
					return err
				}
				if reply != "" && !opts.DryRun {
					firstComment := thread.Comments[0]
					if err := ghReplyToComment(repo, br.PRNumber, firstComment.DatabaseID, reply); err != nil {
						fmt.Printf("    Reply failed: %v\n", err)
					} else {
						if err := ghResolveThread(thread.ThreadID); err != nil {
							fmt.Printf("    Resolve failed: %v\n", err)
						} else {
							fmt.Println("    Resolved")
							addressed++
						}
					}
				}

			case optReply:
				reply, err := ui.Input("Reply")
				if err != nil {
					return err
				}
				if reply != "" && !opts.DryRun {
					firstComment := thread.Comments[0]
					if err := ghReplyToComment(repo, br.PRNumber, firstComment.DatabaseID, reply); err != nil {
						fmt.Printf("    Reply failed: %v\n", err)
					} else {
						if err := ghResolveThread(thread.ThreadID); err != nil {
							fmt.Printf("    Resolve failed: %v\n", err)
						} else {
							fmt.Println("    Resolved")
							addressed++
						}
					}
				}

			case optSkip:
				fmt.Println("    Skipped")
			}
			fmt.Println()
		}

		addressedTotal += addressed

		// If files changed, offer to commit.
		if filesChanged {
			dirty, _ := c.Git.IsDirty()
			if dirty {
				yes, err := ui.Confirm("Commit changes?")
				if err != nil {
					return err
				}
				if yes && !opts.DryRun {
					if err := c.Git.AddAll(); err != nil {
						return fmt.Errorf("git add failed: %w", err)
					}
					if err := c.Git.Commit("address review comments", git.CommitOpts{}); err != nil {
						return fmt.Errorf("commit failed: %w", err)
					}
					fmt.Println("  Committed")

					// Restack descendants.
					b := g.Branches[br.Branch]
					if b != nil && len(b.Children) > 0 {
						fmt.Println("  Restacking descendants...")
						if err := Restack(c, RestackOpts{}); err != nil {
							return fmt.Errorf("restack failed: %w", err)
						}
					}
				}
			}
		}
	}

	fmt.Printf("\nDone — addressed %d thread(s) across %d PR(s)\n", addressedTotal, len(result.Stack))
	return nil
}

// showCodeContext prints a few lines of code around the target line.
func showCodeContext(path string, line int) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	lines := strings.Split(string(data), "\n")
	start := max(0, line-4)
	end := min(len(lines), line+3)

	for i := start; i < end; i++ {
		marker := "  "
		if i+1 == line {
			marker = "→ "
		}
		fmt.Printf("    %s%4d │ %s\n", marker, i+1, lines[i])
	}
	fmt.Println()
}

// openEditorAtLine opens the user's editor at a specific file and line.
func openEditorAtLine(path string, line int) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}

	// Most editors support +line syntax.
	cmd := exec.Command(editor, fmt.Sprintf("+%d", line), path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func buildReviewAISystemPrompt() string {
	var b strings.Builder
	b.WriteString("You are a PR review assistant for stackr, a stacked-branch git workflow.\n\n")
	b.WriteString("You are given JSON containing unresolved review threads across a stack of PRs.\n\n")
	b.WriteString("Work the stackr way: prefer `sr` over raw git so the stack graph stays in sync, ")
	b.WriteString("and address branches bottom-to-top so each fix lands before the branches that build on it.\n\n")
	b.WriteString("Your job:\n")
	b.WriteString("1. Read all unresolved threads and plan a resolution strategy.\n")
	b.WriteString("2. Start from the bottom of the stack (first entry in the stack array).\n")
	b.WriteString("3. For each branch:\n")
	b.WriteString("   a. Run: sr checkout <branch>\n")
	b.WriteString("   b. Read each comment and make the requested code changes.\n")
	b.WriteString("   c. Reply to each thread explaining what you changed:\n")
	b.WriteString("      gh api repos/OWNER/REPO/pulls/NUMBER/comments -f body='Fixed — ...' -F in_reply_to=COMMENT_ID\n")
	b.WriteString("   d. Resolve each thread:\n")
	b.WriteString("      gh api graphql -f query='mutation($id:ID!){resolveReviewThread(input:{threadId:$id}){thread{isResolved}}}' -f id=THREAD_ID\n")
	b.WriteString("   e. Commit with stackr: sr commit -m 'address review comments'\n")
	b.WriteString("   f. Restack dependents: sr restack\n")
	b.WriteString("   g. If conflicts arise: resolve them, then sr continue\n")
	b.WriteString("4. Move to the next branch up the stack and repeat.\n")
	b.WriteString("5. When all threads are resolved, you are done.\n\n")
	b.WriteString("Use the thread's threadId for resolving and the first comment's id (database ID) for replies.\n")
	b.WriteString("The OWNER and REPO can be found with: gh repo view --json owner,name\n")
	return b.String()
}

