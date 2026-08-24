package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const ghTimeout = 15 * time.Second

// PRResult holds metadata returned from GitHub about a pull request.
type PRResult struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	State  string `json:"state"`
	Title  string `json:"title"`
	Draft  bool   `json:"isDraft"`
}

// GHCreateOpts holds options for creating a PR via gh.
type GHCreateOpts struct {
	Base  string // base branch (parent)
	Head  string // head branch (current)
	Title string
	Body  string
	Draft bool
}

// ghCheckInstalled verifies that the gh CLI is available on PATH.
func ghCheckInstalled() error {
	_, err := exec.LookPath("gh")
	if err != nil {
		return fmt.Errorf("gh CLI not found — install it from https://cli.github.com")
	}
	return nil
}

// ghPRForBranch checks whether a PR exists for the given branch.
// Returns nil, nil when no PR exists.
//
// gh exits 1 both for "no pull requests found" and for server-side failures
// (HTTP 503 and friends), so the exit code cannot distinguish "no PR" from
// "GitHub didn't answer". Mistaking the second for the first made submit
// offer to create PRs that already existed — the answer has to come from the
// error text. Server-side failures are retried before giving up, since one
// flaky call otherwise poisons a whole stack survey.
func ghPRForBranch(branch string) (*PRResult, error) {
	const attempts = 3
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			time.Sleep(time.Duration(i) * time.Second)
		}
		result, stderr, err := ghPRView(branch)
		switch {
		case err == nil:
			return result, nil
		case ghNoPRFound(stderr):
			return nil, nil
		case !ghServerError(stderr):
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

// ghPRView runs one `gh pr view` and returns its stderr alongside the error,
// so the caller can classify the failure. It never interprets exit codes.
func ghPRView(branch string) (*PRResult, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ghTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gh", "pr", "view", branch,
		"--json", "number,url,state,title,isDraft")
	cmd.Env = append(cmd.Environ(), "GH_PROMPT_DISABLED=1")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, "", fmt.Errorf("gh pr view timed out after %s", ghTimeout)
		}
		msg := strings.TrimSpace(stderr.String())
		return nil, msg, fmt.Errorf("gh pr view failed: %s: %w", msg, err)
	}

	var result PRResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, "", fmt.Errorf("failed to parse gh output: %w", err)
	}
	return &result, "", nil
}

// ghNoPRFound reports whether a gh failure means the branch genuinely has no
// pull request — gh prints `no pull requests found for branch "x"` for it.
func ghNoPRFound(stderr string) bool {
	return strings.Contains(stderr, "no pull requests found")
}

// ghServerError reports whether a gh failure is GitHub's, not ours: an HTTP
// 5xx from the API (gh prints "HTTP 503: ..."). Worth retrying; everything
// else (auth, rate limits, bad requests) fails the same way again.
func ghServerError(stderr string) bool {
	return strings.Contains(stderr, "HTTP 5")
}

// ghUpdatePRBase retargets an existing PR's base branch on GitHub.
//
// This is REST, not `gh pr edit --base`: that command's mutation also
// requests project-card data GitHub has since deprecated, which makes the
// whole edit fail with a GraphQL error even though the base-branch change
// itself would have succeeded. PATCHing the REST endpoint directly sidesteps
// that field entirely.
func ghUpdatePRBase(number int, base string) error {
	ctx, cancel := context.WithTimeout(context.Background(), ghTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gh", "api",
		"--method", "PATCH",
		fmt.Sprintf("repos/{owner}/{repo}/pulls/%d", number),
		"-f", "base="+base)
	cmd.Env = append(cmd.Environ(), "GH_PROMPT_DISABLED=1")

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("gh api PATCH pulls/%d timed out after %s", number, ghTimeout)
		}
		return fmt.Errorf("gh api PATCH pulls/%d failed: %s", number, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// ghMergedHeadBranches returns the head-branch names whose PRs are merged,
// each keyed to its PR number, in a single batched query.
//
// This is the only reliable way to detect a squash-merged or rebase-merged
// branch: those strategies rewrite the commits when landing them, so no local
// ancestry test will ever recognize them as merged.
//
// The second return reports whether GitHub actually answered. Callers need
// the distinction because an empty set means two very different things: "none
// of these branches merged" — trustworthy, and the forge's word is final — or
// "gh is missing, offline, or errored" — the local fallbacks are all there is.
// Best-effort by design either way: sync must still work with no network.
//
// dir is the repository the query is about. gh resolves the target repo from
// the git remotes of its working directory, and this can differ from the
// process cwd (`--cwd`, worktrees, tests) — an answer about the wrong repo
// would be trusted as final and silently disable the local fallbacks.
func ghMergedHeadBranches(dir string) (map[string]int, bool) {
	merged := map[string]int{}

	if err := ghCheckInstalled(); err != nil {
		return merged, false
	}

	prs, ok := ghListPRHeads(dir, "merged")
	if !ok {
		return merged, false
	}
	for _, pr := range prs {
		// The list is most-recently-updated first; for a head name reused
		// across several merged PRs, the newest one is the relevant claim.
		if pr.HeadRefName != "" && merged[pr.HeadRefName] == 0 {
			merged[pr.HeadRefName] = pr.Number
		}
	}
	return merged, true
}

// ghClosedUnmergedHeadBranches returns head-branch names that have a PR
// closed WITHOUT merging, keyed to the newest such PR's number. A newer
// merged PR can exist for the same head name; callers must let the merged
// claim win, as cleanMergedBranches does.
//
// Deleting these branches discards work — their commits exist on no other
// ref; see cleanMergedBranches for the consent policy that guards them.
func ghClosedUnmergedHeadBranches(dir string) map[string]int {
	closed := map[string]int{}

	if err := ghCheckInstalled(); err != nil {
		return closed
	}

	prs, ok := ghListPRHeads(dir, "closed")
	if !ok {
		return closed
	}
	for _, pr := range prs {
		// gh routes --search through GitHub's search API, where "closed"
		// includes merged PRs; the state field tells them apart. Keep only
		// the newest claim per head name, as in ghMergedHeadBranches.
		if pr.State == "CLOSED" && pr.HeadRefName != "" && closed[pr.HeadRefName] == 0 {
			closed[pr.HeadRefName] = pr.Number
		}
	}
	return closed
}

type ghPRHead struct {
	HeadRefName string `json:"headRefName"`
	Number      int    `json:"number"`
	State       string `json:"state"`
}

// ghListPRHeads runs one batched `gh pr list` for the given state and returns
// the raw entries, newest-updated first. The bool reports whether gh answered.
func ghListPRHeads(dir, state string) ([]ghPRHead, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), ghTimeout)
	defer cancel()

	// --limit caps the result at the 200 most recent matches of whatever the
	// query sorts by. Without an explicit sort, gh orders by creation date, so
	// a branch merged today but created long ago could fall outside the
	// window. "sort:updated-desc" orders by recency of the state change
	// instead, which is what determines whether cleanup needs to see it.
	cmd := exec.CommandContext(ctx, "gh", "pr", "list",
		"--state", state,
		"--search", "sort:updated-desc",
		"--limit", "200",
		"--json", "headRefName,number,state")
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), "GH_PROMPT_DISABLED=1")

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = nil

	if err := cmd.Run(); err != nil {
		return nil, false
	}

	var prs []ghPRHead
	if err := json.Unmarshal(stdout.Bytes(), &prs); err != nil {
		return nil, false
	}
	return prs, true
}

// ghCreatePR creates a new PR via gh and returns the result.
func ghCreatePR(opts GHCreateOpts) (*PRResult, error) {
	args := []string{"pr", "create",
		"--base", opts.Base,
		"--head", opts.Head,
		"--title", opts.Title,
		"--body", opts.Body,
	}
	if opts.Draft {
		args = append(args, "--draft")
	}

	fmt.Printf("Creating PR: %s -> %s", opts.Head, opts.Base)
	if opts.Draft {
		fmt.Print(" (draft)")
	}
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Env = append(cmd.Environ(), "GH_PROMPT_DISABLED=1")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("gh pr create timed out")
		}
		return nil, fmt.Errorf("gh pr create failed: %s: %w", strings.TrimSpace(stderr.String()), err)
	}

	fmt.Println("Fetching PR metadata...")

	// gh pr create prints the PR URL on success. Fetch full metadata.
	result, err := ghPRForBranch(opts.Head)
	if err != nil {
		return nil, fmt.Errorf("PR created but failed to fetch metadata: %w", err)
	}
	if result == nil {
		// Shouldn't happen, but handle gracefully.
		url := strings.TrimSpace(stdout.String())
		return &PRResult{URL: url, State: "OPEN", Title: opts.Title, Draft: opts.Draft}, nil
	}
	return result, nil
}
