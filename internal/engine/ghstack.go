package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/amustafa/stackr/internal/graph"
	"github.com/amustafa/stackr/internal/store"
)

// ghStackAPIVersion pins the REST API version that introduced the stacks
// endpoints. GitHub dates its API versions; without this header the stacks
// routes are not served.
const ghStackAPIVersion = "2026-03-10"

// minStackSize is GitHub's floor: a stack is "two or more pull requests".
// A lone PR is a perfectly valid PR, just not a stack.
const minStackSize = 2

// GHStack mirrors the stack object returned by the GitHub REST API.
type GHStack struct {
	ID     int    `json:"id"`
	Number int    `json:"number"`
	NodeID string `json:"node_id"`
	URL    string `json:"url"`
	Open   bool   `json:"open"`
	Base   struct {
		Ref string `json:"ref"`
	} `json:"base"`
	PullRequests []GHStackPR `json:"pull_requests"`
}

// GHStackPR is one pull request as reported inside a stack. GitHub returns a
// trimmed view here, not the full PR object.
type GHStackPR struct {
	Number   int     `json:"number"`
	State    string  `json:"state"`
	Draft    bool    `json:"draft"`
	MergedAt *string `json:"merged_at"`
	Head     struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
}

// prNumbers returns the stack's member PR numbers in bottom-to-top order.
func (s *GHStack) prNumbers() []int {
	nums := make([]int, 0, len(s.PullRequests))
	for _, pr := range s.PullRequests {
		nums = append(nums, pr.Number)
	}
	return nums
}

// ghStackAPI invokes `gh api` against a stacks endpoint. A nil body sends no
// request payload (GET); a non-nil body is marshalled to JSON on stdin, which
// avoids `gh`'s field-flag syntax entirely and keeps the array ordering exact.
//
// gh expands the {owner}/{repo} placeholders from the current repository, so
// stackr never has to resolve the remote itself.
func ghStackAPI(method, path string, body any) ([]byte, error) {
	args := []string{"api",
		"--method", method,
		path,
		"-H", "Accept: application/vnd.github+json",
		"-H", "X-GitHub-Api-Version: " + ghStackAPIVersion,
	}

	var stdin bytes.Buffer
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to encode request: %w", err)
		}
		stdin.Write(data)
		args = append(args, "--input", "-")
	}

	ctx, cancel := context.WithTimeout(context.Background(), ghTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Env = append(cmd.Environ(), "GH_PROMPT_DISABLED=1")
	if body != nil {
		cmd.Stdin = &stdin
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("gh api %s timed out after %s", path, ghTimeout)
		}
		return nil, fmt.Errorf("gh api %s %s failed: %s", method, path, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// ghCreateStack registers a new stack from PR numbers ordered bottom to top.
// GitHub validates that each PR's base ref matches the previous PR's head ref
// and returns 422 if the chain does not hold.
func ghCreateStack(prNumbers []int) (*GHStack, error) {
	out, err := ghStackAPI("POST", "repos/{owner}/{repo}/stacks",
		map[string]any{"pull_requests": prNumbers})
	if err != nil {
		return nil, err
	}
	var s GHStack
	if err := json.Unmarshal(out, &s); err != nil {
		return nil, fmt.Errorf("failed to parse stack response: %w", err)
	}
	return &s, nil
}

// ghGetStack reads a stack by its stack number.
// Returns nil, nil when the stack no longer exists.
func ghGetStack(number int) (*GHStack, error) {
	out, err := ghStackAPI("GET", fmt.Sprintf("repos/{owner}/{repo}/stacks/%d", number), nil)
	if err != nil {
		// A dissolved or merged-away stack is an expected state, not a failure.
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "Not Found") {
			return nil, nil
		}
		return nil, err
	}
	var s GHStack
	if err := json.Unmarshal(out, &s); err != nil {
		return nil, fmt.Errorf("failed to parse stack response: %w", err)
	}
	return &s, nil
}

// ghAddToStack appends PRs above the current top of an existing stack.
// Only the new PRs are sent, ordered from the current top upward.
func ghAddToStack(stackNumber int, prNumbers []int) (*GHStack, error) {
	out, err := ghStackAPI("POST", fmt.Sprintf("repos/{owner}/{repo}/stacks/%d/add", stackNumber),
		map[string]any{"pull_requests": prNumbers})
	if err != nil {
		return nil, err
	}
	var s GHStack
	if err := json.Unmarshal(out, &s); err != nil {
		return nil, fmt.Errorf("failed to parse stack response: %w", err)
	}
	return &s, nil
}

// ghUnstack dissolves a stack on GitHub, leaving its pull requests intact and
// their base refs untouched. Only the grouping goes away.
func ghUnstack(stackNumber int) error {
	_, err := ghStackAPI("POST", fmt.Sprintf("repos/{owner}/{repo}/stacks/%d/unstack", stackNumber), nil)
	return err
}

// linearSegments decomposes the submitted branches into maximal linear runs,
// each of which can become one GitHub stack.
//
// stackr's graph is a tree — a branch may have several children — but a GitHub
// stack is strictly linear, and a PR may only belong to one stack. The two
// models therefore do not map one-to-one, and a fork has to be split.
//
// The rule is to cut at every fork and let each child start a fresh run whose
// base is the fork point:
//
//	main <- A <- B <- C          segments: [A B]  [C]  [D]
//	              \- D           stacks:   [A B]  (C and D are single PRs,
//	                                        already based on B, so they are
//	                                        left unregistered until they grow)
//
// This keeps every registered stack valid on GitHub's terms and never puts a
// PR in two stacks. What it gives up is that GitHub shows no relationship
// between [A B] and the PRs sitting on top of B — the base refs still chain
// correctly, so nothing is broken, but the shared history is not visible as a
// single stack. That is a limitation of GitHub's linear model, not of the
// decomposition.
//
// A branch starts a new segment when its parent is outside the submitted set
// (nothing to attach to) or when its parent has more than one submitted child
// (a fork). Otherwise it extends its parent's segment.
func linearSegments(g *graph.Graph, submitted []string) [][]string {
	inSet := make(map[string]bool, len(submitted))
	for _, name := range submitted {
		b := g.Branches[name]
		if b == nil || b.IsTrunk {
			continue
		}
		inSet[name] = true
	}

	childrenInSet := func(name string) []string {
		var kids []string
		for _, child := range g.ChildrenOf(name) {
			if inSet[child] {
				kids = append(kids, child)
			}
		}
		return kids
	}

	// Walk `submitted` in order so segments come out deterministically; callers
	// push bottom-up, which makes the lowest branch of each run come first.
	var segments [][]string
	claimed := make(map[string]bool, len(inSet))

	for _, name := range submitted {
		if !inSet[name] || claimed[name] {
			continue
		}
		parent := g.Branches[name].ParentBranchName
		startsSegment := !inSet[parent] || len(childrenInSet(parent)) != 1
		if !startsSegment {
			continue
		}

		// Extend upward while the run stays unambiguously linear.
		segment := []string{name}
		claimed[name] = true
		for {
			kids := childrenInSet(segment[len(segment)-1])
			if len(kids) != 1 {
				break
			}
			segment = append(segment, kids[0])
			claimed[kids[0]] = true
		}
		segments = append(segments, segment)
	}

	return segments
}

// syncGitHubStacks registers the submitted branches as stacks on GitHub so the
// remote reflects the local stack shape.
//
// Best-effort by design, matching ghMergedHeadBranches: the PRs are already
// created and pushed by the time this runs, so a repository without the preview
// enabled, an offline machine, or an older gh must not turn a successful submit
// into a failure. Problems are reported, not fatal.
func syncGitHubStacks(g *graph.Graph, prInfo *store.PRInfo, submitted []string, quiet bool) {
	for _, segment := range linearSegments(g, submitted) {
		seg := mapSegment(prInfo, segment)

		if len(seg.prNumbers) < minStackSize {
			continue
		}

		stack, err := reconcileStack(seg.recorded, seg.prNumbers, seg.baseByPR)
		if err != nil {
			fmt.Printf("Warning: could not sync GitHub stack for %s: %v\n",
				strings.Join(seg.branches, " -> "), err)
			continue
		}
		if stack == nil {
			continue
		}

		for _, name := range seg.branches {
			prInfo.Branches[name].StackNumber = stack.Number
		}
		if !quiet {
			fmt.Printf("GitHub stack #%d: %s\n", stack.Number, strings.Join(seg.branches, " -> "))
		}
	}
}

// segmentPRs is one linear segment resolved against the recorded PR metadata.
type segmentPRs struct {
	branches  []string       // branches that have a PR, bottom-up
	prNumbers []int          // their PR numbers, same order
	baseByPR  map[int]string // PR number -> the base branch the local graph wants
	recorded  []int          // every stack these PRs are already registered against
}

// mapSegment resolves a segment's branches to pull requests, dropping any branch
// that was pushed but has no PR yet — a stack can only contain pull requests.
//
// recorded collects EVERY distinct stack number, not just the first one found.
// Local reshaping (`sr move`, `sr fold`, `sr reorder`) can merge two previously
// separate segments into one, and GitHub allows a PR in only one stack — so a
// segment spanning two recorded stacks has to dissolve both before it can be
// regrouped. Keeping only the first would make every subsequent submit fail the
// same way, with no path back to a consistent state.
func mapSegment(prInfo *store.PRInfo, segment []string) segmentPRs {
	seg := segmentPRs{baseByPR: map[int]string{}}
	recorded := map[int]bool{}

	for _, name := range segment {
		pr := prInfo.Branches[name]
		if pr == nil || pr.Number == 0 {
			continue
		}
		seg.prNumbers = append(seg.prNumbers, pr.Number)
		seg.branches = append(seg.branches, name)
		seg.baseByPR[pr.Number] = pr.BaseBranch
		if pr.StackNumber != 0 {
			recorded[pr.StackNumber] = true
		}
	}

	seg.recorded = sortedKeys(recorded)
	return seg
}

// rebuildStack regroups a segment from scratch: dissolve every stack its pull
// requests are currently registered against, re-point their base refs, then
// create the new one.
//
// Dissolving first is not optional. GitHub allows a pull request to belong to
// only one stack, and a stack keeps its members even once it is closed — only an
// explicit unstack removes them. Creating without dissolving is therefore
// rejected for any PR GitHub still considers grouped, which is the shape of
// every failure this path exists to recover from.
//
// A stack that has already gone away is not an error: absent is exactly the
// state unstacking was meant to reach.
func rebuildStack(recorded []int, prNumbers []int, baseByPR map[int]string) (*GHStack, error) {
	for _, n := range recorded {
		if err := ghUnstack(n); err != nil {
			if isMissingStack(err) {
				continue
			}
			return nil, fmt.Errorf("could not unstack #%d before rebuilding: %w", n, err)
		}
	}

	// ghCreateStack validates that each PR's base ref equals the previous PR's
	// head ref. A PR's base cannot be retargeted while it is grouped into a
	// stack, so submit's own attempt to do this earlier necessarily failed for
	// every PR here — retry now that the groups are dissolved, or the create
	// fails right back with the same chain-validation error.
	for _, n := range prNumbers {
		base, ok := baseByPR[n]
		if !ok || base == "" {
			continue
		}
		if err := ghUpdatePRBase(n, base); err != nil {
			fmt.Printf("Warning: could not retarget PR #%d to %s: %v\n", n, base, err)
		}
	}

	return ghCreateStack(prNumbers)
}

// isMissingStack reports whether an error means the stack is already gone.
func isMissingStack(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "404") || strings.Contains(msg, "Not Found")
}

// sortedKeys returns a set's members in ascending order, so stacks are
// dissolved in a deterministic order and warnings read the same way twice.
func sortedKeys(set map[int]bool) []int {
	out := make([]int, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}

// stackNeedsRebuild reports whether a recorded stack can no longer be extended,
// so the next submit has to start a fresh one.
//
// A stack whose pull requests have all merged is NOT deleted: it stays
// queryable with open:false and an emptied member list. Checking only for a
// 404 would therefore miss the most common end-of-life case and leave us
// POSTing /add against a closed stack.
func stackNeedsRebuild(remote *GHStack) bool {
	return remote == nil || !remote.Open || len(remote.PullRequests) == 0
}

// classifyAgainstRemote locates remote's entire PR sequence as a contiguous,
// exact-order run within prNumbers, and splits what's left into newBelow (what
// precedes the run) and newAbove (what follows it). found is false when
// remote's sequence cannot be located as a contiguous run at all — a genuine
// divergence, not just growth in one direction.
//
// GitHub drops merged PRs out of a stack and retargets what remains, so the
// remote list is normally a suffix of ours rather than an exact match — that
// shows up as a non-empty newBelow, same shape as a PR genuinely inserted
// below the stack's current bottom (e.g. a restack onto a new base branch).
// The two are positionally indistinguishable; callers must check newBelow's
// PR state to tell them apart before deciding whether a rebuild is needed.
func classifyAgainstRemote(prNumbers, remoteNums []int) (newBelow, newAbove []int, found bool) {
	if len(remoteNums) == 0 {
		return nil, prNumbers, true
	}
	if len(remoteNums) > len(prNumbers) {
		return nil, nil, false
	}

	top := remoteNums[len(remoteNums)-1]
	for i, n := range prNumbers {
		if n != top {
			continue
		}
		start := i - len(remoteNums) + 1
		if start < 0 {
			continue
		}
		matched := true
		for j, want := range remoteNums {
			if prNumbers[start+j] != want {
				matched = false
				break
			}
		}
		if matched {
			return prNumbers[:start], prNumbers[i+1:], true
		}
	}
	return nil, nil, false
}

// anyPROpen reports whether any of the given PR numbers is still open on
// GitHub, as opposed to merged or closed.
func anyPROpen(prNumbers []int) (bool, error) {
	for _, n := range prNumbers {
		out, err := ghStackAPI("GET", fmt.Sprintf("repos/{owner}/{repo}/pulls/%d", n), nil)
		if err != nil {
			return false, err
		}
		var pr struct {
			State string `json:"state"`
		}
		if err := json.Unmarshal(out, &pr); err != nil {
			return false, fmt.Errorf("failed to parse PR #%d response: %w", n, err)
		}
		if pr.State == "open" {
			return true, nil
		}
	}
	return false, nil
}

// reconcileStack brings one segment's stack on GitHub in line with the local
// PR chain: creating it, extending it, or leaving it alone if it already
// matches. Returns nil when there is nothing to record.
//
// recorded holds every stack number the segment's PRs are currently registered
// against. Normally that is zero or one, but local reshaping can merge two
// previously separate segments, and GitHub allows a PR in only one stack — so
// more than one means the group has to be rebuilt from scratch rather than
// extended.
//
// baseByPR maps each PR number to the base branch it should have, per the
// local graph — used only to re-sync base refs when a stack is rebuilt.
func reconcileStack(recorded []int, prNumbers []int, baseByPR map[int]string) (*GHStack, error) {
	if len(recorded) == 0 {
		return ghCreateStack(prNumbers)
	}

	// A segment spanning several recorded stacks cannot be extended into any one
	// of them: /add would be rejected for every PR that belongs to another.
	if len(recorded) > 1 {
		return rebuildStack(recorded, prNumbers, baseByPR)
	}

	existing := recorded[0]
	remote, err := ghGetStack(existing)
	if err != nil {
		return nil, err
	}

	if stackNeedsRebuild(remote) {
		// Rebuild, not create. A closed stack keeps its members — only an
		// explicit unstack dissolves one — so creating straight away would be
		// rejected for PRs GitHub still considers grouped. rebuildStack
		// tolerates a stack that has genuinely gone away.
		return rebuildStack(recorded, prNumbers, baseByPR)
	}

	newBelow, newAbove, found := classifyAgainstRemote(prNumbers, remote.prNumbers())
	if !found {
		// The remote stack's top is not in our segment at all: the two have
		// genuinely diverged, not merely fallen behind. See resolveDivergedStack.
		return resolveDivergedStack(existing, remote, prNumbers)
	}

	if len(newBelow) > 0 {
		// GitHub's stacks API can only extend a stack upward — ghAddToStack
		// appends above the current top, there is no way to attach a PR below
		// the existing base. That's fine when newBelow is only PRs GitHub
		// already dropped for merging (the common case), but if any of them
		// are still open, a PR has genuinely been inserted below the stack's
		// current bottom (e.g. `sr restack` onto a new base branch), and the
		// only way to reflect that on GitHub is to rebuild the stack outright.
		open, err := anyPROpen(newBelow)
		if err != nil {
			return nil, err
		}
		if open {
			// GitHub refuses to create a stack containing a PR that's already a
			// member of another one, so the old grouping has to be dissolved
			// first. Not atomic: if the create fails after the unstack succeeds,
			// the PRs are left ungrouped — the same known gap noted on
			// resolveDivergedStack, not one this path introduces.
			return rebuildStack(recorded, prNumbers, baseByPR)
		}
	}

	if len(newAbove) == 0 {
		return remote, nil
	}
	return ghAddToStack(existing, newAbove)
}

// resolveDivergedStack decides what to do when the stack recorded on GitHub no
// longer lines up with the local segment — its top PR is not in our chain at
// all, so there is nothing to simply append to.
//
// This happens for good reasons and bad ones:
//   - `sr reorder` / `sr move` / `sr fold` reshaped the local stack, so the
//     segment now covers a different set of PRs than when it was registered.
//   - Someone regrouped the PRs by hand in the web UI or with `gh stack`.
//   - A PR mid-stack was closed rather than merged.
//
// The trade-off is whose view wins. Rebuilding (ghUnstack + ghCreateStack)
// makes GitHub mirror the local graph, which is the whole point of the
// integration — but it silently discards any deliberate grouping someone made
// on the remote. Leaving it alone (return remote, nil, after a warning) never
// destroys remote intent, but lets GitHub drift from local indefinitely with
// only a warning that is easy to miss in a long submit.
//
// TODO(implement): choose the reconciliation policy.
//
// Things worth weighing: stackr already treats the local graph as the source
// of truth everywhere else (see restack.go), which argues for rebuilding. But
// unstack-then-create is not atomic — if ghCreateStack fails after ghUnstack
// succeeds, the PRs are left ungrouped and prInfo still points at a dead stack
// number, so a failure path needs to at least clear the recorded number.
// Also consider whether a non-interactive submit should ever be destructive,
// given c.Interactive exists and is threaded through the submit flow.
func resolveDivergedStack(existing int, remote *GHStack, prNumbers []int) (*GHStack, error) {
	return nil, fmt.Errorf(
		"local stack %v has diverged from GitHub stack #%d %v — reconciliation policy not implemented",
		prNumbers, existing, remote.prNumbers())
}
