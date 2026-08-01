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
func ghStackAPI(method, path string, body any, extra ...string) ([]byte, error) {
	args := []string{"api",
		"--method", method,
		path,
		"-H", "Accept: application/vnd.github+json",
		"-H", "X-GitHub-Api-Version: " + ghStackAPIVersion,
	}
	args = append(args, extra...)

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

// ghListStacks returns every stack in the repository.
//
// GitHub is the only authority on which stack a pull request belongs to. The
// StackNumber recorded in pr_info.json is a cache, and it can be wrong in every
// direction: lost (a failed reconcile used to zero it), stale (someone regrouped
// in the web UI), or simply absent (a fresh clone, or a stack a teammate made).
// Reconciling against the cache rather than against GitHub is what made a single
// transient failure unrecoverable — with no recorded number the next submit
// takes the "create a fresh stack" path, and GitHub answers 422 "already part of
// a stack" for as long as the real grouping survives, which is forever.
//
// --paginate because a repository accumulates a stack per merged series and the
// endpoint is paged. A missed page reads exactly like "this PR is in no stack",
// which is the failure this function exists to remove.
func ghListStacks() ([]GHStack, error) {
	out, err := ghStackAPI("GET", "repos/{owner}/{repo}/stacks", nil, "--paginate")
	if err != nil {
		return nil, err
	}
	var stacks []GHStack
	if err := json.Unmarshal(out, &stacks); err != nil {
		return nil, fmt.Errorf("failed to parse stack list: %w", err)
	}
	return stacks, nil
}

// stacksContaining narrows the repository's stacks to those holding at least one
// of our pull requests — the only ones that can stand between this segment and
// the grouping it wants. Ordered by stack number so unstacks happen in a
// deterministic order and warnings read the same way twice.
func stacksContaining(all []GHStack, prNumbers []int) []GHStack {
	want := make(map[int]bool, len(prNumbers))
	for _, n := range prNumbers {
		want[n] = true
	}

	var out []GHStack
	for _, s := range all {
		for _, pr := range s.PullRequests {
			if want[pr.Number] {
				out = append(out, s)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out
}

// stackNumbersOf lists the numbers of a set of stacks, in the order given.
func stackNumbersOf(groups []GHStack) []int {
	out := make([]int, 0, len(groups))
	for _, g := range groups {
		out = append(out, g.Number)
	}
	return out
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
func syncGitHubStacks(g *graph.Graph, prInfo *store.PRInfo, submitted []string, quiet, interactive bool) {
	segments := linearSegments(g, submitted)
	if len(segments) == 0 {
		return
	}

	// One listing for the whole submit, since every segment reconciles against
	// the same repository-wide picture. Failing to read it is not the same as
	// finding nothing: without the listing we cannot tell an ungrouped PR from
	// one already in a stack, and guessing "ungrouped" is precisely what makes
	// ghCreateStack fail with 422. Say so and leave the recorded numbers alone.
	all, err := ghListStacks()
	if err != nil {
		fmt.Printf("Warning: could not read GitHub stacks (%v) — stack grouping was left unchanged.\n", err)
		return
	}

	for _, segment := range segments {
		seg := mapSegment(prInfo, segment)

		if len(seg.prNumbers) < minStackSize {
			continue
		}

		sync, err := reconcileStack(seg.prNumbers, seg.baseByPR, all, interactive)

		// Record what was taken apart before looking at the error. A dissolved
		// grouping is gone whether or not the rest of the reconcile succeeded, so
		// keeping its number would leave these branches pointing at a stack they
		// are no longer in — and dropping it when nothing was dissolved would
		// throw away the only local trace of a stack that still exists.
		if len(sync.Dissolved) > 0 {
			for _, name := range seg.branches {
				prInfo.Branches[name].StackNumber = 0
			}
			all = withoutStacks(all, sync.Dissolved)
		}

		if err != nil {
			fmt.Printf("Warning: could not sync GitHub stack for %s: %v\n",
				strings.Join(seg.branches, " -> "), err)
			continue
		}
		if sync.Stack == nil {
			continue
		}

		for _, name := range seg.branches {
			prInfo.Branches[name].StackNumber = sync.Stack.Number
		}
		if !quiet {
			fmt.Printf("GitHub stack #%d: %s\n", sync.Stack.Number, strings.Join(seg.branches, " -> "))
		}
	}
}

// withoutStacks drops dissolved stacks from the cached listing.
//
// The listing is read once per submit, but a fork produces several segments and
// two of them can be covered by one remote stack — dissolving it for the first
// segment would otherwise leave the second reconciling against a group that no
// longer exists, and treating a live PR as already-taken.
func withoutStacks(all []GHStack, numbers []int) []GHStack {
	gone := make(map[int]bool, len(numbers))
	for _, n := range numbers {
		gone[n] = true
	}

	out := all[:0:0]
	for _, s := range all {
		if !gone[s.Number] {
			out = append(out, s)
		}
	}
	return out
}

// segmentPRs is one linear segment resolved against the recorded PR metadata.
type segmentPRs struct {
	branches  []string       // branches that have a PR, bottom-up
	prNumbers []int          // their PR numbers, same order
	baseByPR  map[int]string // PR number -> the base branch the local graph wants
}

// mapSegment resolves a segment's branches to pull requests, dropping any branch
// that was pushed but has no PR yet — a stack can only contain pull requests.
//
// Deliberately no StackNumber here. Which stack a PR belongs to is read back
// from GitHub (ghListStacks) rather than taken from this file: the recorded
// number is a cache that goes stale whenever anyone regroups on the remote, and
// treating it as the truth is what let one failed reconcile wedge the
// integration permanently. StackNumber is now written by the sync and read only
// for display (`sr info`, `sr log`).
func mapSegment(prInfo *store.PRInfo, segment []string) segmentPRs {
	seg := segmentPRs{baseByPR: map[int]string{}}

	for _, name := range segment {
		pr := prInfo.Branches[name]
		if pr == nil || pr.Number == 0 {
			continue
		}
		seg.prNumbers = append(seg.prNumbers, pr.Number)
		seg.branches = append(seg.branches, name)
		seg.baseByPR[pr.Number] = pr.BaseBranch
	}

	return seg
}

// stackSync is the outcome of reconciling one segment.
//
// Dissolved names the stacks actually taken apart, and is reported alongside the
// error rather than instead of it, because the caller has to record different
// things in the two failure modes. A create or an /add that never dissolved
// anything leaves the remote grouping exactly as it was, so the recorded stack
// numbers are still true and must be kept. A failure after an unstack leaves the
// pull requests genuinely ungrouped, so keeping the number would point them at a
// stack they are no longer in.
//
// Clearing unconditionally is what wedged this integration before: one failed
// /add erased the only local record of the stack, so every later submit took the
// "create fresh" path and GitHub answered 422 forever.
type stackSync struct {
	Stack     *GHStack
	Dissolved []int
}

// unstackAll dissolves each stack, reporting which ones actually came apart.
//
// A stack that has already gone away is not an error: absent is exactly the
// state unstacking was meant to reach, and it is not counted as dissolved
// because nothing changed.
func unstackAll(numbers []int) ([]int, error) {
	var dissolved []int
	for _, n := range numbers {
		if err := ghUnstack(n); err != nil {
			if isMissingStack(err) {
				continue
			}
			return dissolved, fmt.Errorf("could not unstack #%d: %w", n, err)
		}
		dissolved = append(dissolved, n)
	}
	return dissolved, nil
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
func rebuildStack(dissolve []int, prNumbers []int, baseByPR map[int]string) (stackSync, error) {
	dissolved, err := unstackAll(dissolve)
	if err != nil {
		return stackSync{Dissolved: dissolved}, fmt.Errorf("%w (before rebuilding the stack)", err)
	}

	// ghCreateStack validates that each PR's base ref equals the previous PR's
	// head ref. A PR's base cannot be retargeted while it is grouped into a
	// stack, so submit's own attempt to do this earlier necessarily failed for
	// every PR here — retry now that the groups are dissolved, or the create
	// fails right back with the same chain-validation error.
	retargetBases(prNumbers, baseByPR)

	s, err := ghCreateStack(prNumbers)
	return stackSync{Stack: s, Dissolved: dissolved}, err
}

// isMissingStack reports whether an error means the stack is already gone.
func isMissingStack(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "404") || strings.Contains(msg, "Not Found")
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

// chooseAnchor decides which existing stack this segment should grow into, and
// which stacks have to be dissolved to get there.
//
// A stack is an eligible anchor when it is still live (open, non-empty) and its
// members appear inside our segment as a contiguous run in the same order. That
// is the only shape GitHub's /add endpoint can extend: it appends above the
// current top and cannot splice into the middle or below the bottom.
//
// Among eligible stacks the LOWEST run wins. Anchoring as far down the segment
// as possible leaves the most PRs to append and the fewest to dissolve, and it
// keeps the stack number stable for the branches at the bottom — the ones most
// likely to be under review already, and so the ones whose stack link is most
// expensive to churn.
//
// Every other stack holding one of our PRs goes in dissolve. GitHub allows a
// pull request in only one stack, so a rival grouping over part of our segment
// cannot be merged into the anchor; it has to go away first. That is the
// a-b-c-d-e case where the remote holds {a,b} and {d,e}: {a,b} anchors, {d,e}
// dissolves, and c, d, e are then appended to {a,b}.
func chooseAnchor(prNumbers []int, groups []GHStack) (anchor *GHStack, start int, dissolve []int) {
	best := -1
	bestStart := 0

	for i := range groups {
		g := &groups[i]
		if stackNeedsRebuild(g) {
			continue
		}
		below, _, found := classifyAgainstRemote(prNumbers, g.prNumbers())
		if !found {
			continue
		}
		if best == -1 || len(below) < bestStart {
			best, bestStart = i, len(below)
		}
	}

	for i := range groups {
		if i != best {
			dissolve = append(dissolve, groups[i].Number)
		}
	}
	if best == -1 {
		return nil, 0, dissolve
	}
	return &groups[best], bestStart, dissolve
}

// reconcileStack brings one segment's grouping on GitHub in line with the local
// PR chain, given the repository's current stacks.
//
// Four outcomes, matching the four shapes the remote can be in:
//
//   - No stack holds any of our PRs — create one.
//   - A stack holds exactly this segment — adopt it. Nothing changes on GitHub;
//     we just record the number we had lost.
//   - A stack holds a lower run of the segment — append the rest above it,
//     dissolving any rival grouping over the PRs being appended.
//   - Nothing eligible to extend — rebuild the group from scratch.
//
// baseByPR maps each PR number to the base branch the local graph wants, used to
// re-point PRs that a dissolve has just freed.
func reconcileStack(prNumbers []int, baseByPR map[int]string, all []GHStack, interactive bool) (stackSync, error) {
	groups := stacksContaining(all, prNumbers)
	if len(groups) == 0 {
		s, err := ghCreateStack(prNumbers)
		return stackSync{Stack: s}, err
	}

	anchor, start, dissolve := chooseAnchor(prNumbers, groups)
	diverged := anchor == nil

	if anchor != nil {
		below := prNumbers[:start]
		above := prNumbers[start+len(anchor.PullRequests):]

		// PRs below the anchor's bottom are normally ones GitHub dropped when
		// they merged — nothing to do, the stack simply moved up under us. But a
		// still-OPEN PR down there was genuinely inserted beneath the anchor
		// (`sr restack` onto a new base, say), and /add only appends upward, so
		// the group has to be rebuilt to represent it.
		spliceNeeded := false
		if len(below) > 0 {
			open, err := anyPROpen(below)
			if err != nil {
				return stackSync{}, err
			}
			spliceNeeded = open
		}

		if !spliceNeeded {
			return growAnchor(anchor, above, dissolve, baseByPR)
		}
		dissolve = stackNumbersOf(groups)
	}

	// Nothing in our segment can be extended: the remote grouping has genuinely
	// diverged rather than merely fallen behind. Local reshaping (`sr move`,
	// `sr fold`, `sr reorder`) does this, but so does someone regrouping by hand
	// in the web UI, and rebuilding would silently discard their intent.
	//
	// The policy is to rebuild — stackr treats the local graph as the source of
	// truth everywhere else, and a stacking tool whose remote grouping quietly
	// disagrees with its local one is worse than an occasionally opinionated one
	// — but only with a human present. An unattended submit warns and leaves the
	// remote alone rather than regrouping someone else's pull requests.
	if diverged && !interactive {
		fmt.Printf("Warning: local stack %v does not line up with GitHub stack(s) %v — "+
			"leaving the GitHub grouping alone. Re-run interactively to rebuild it.\n",
			prNumbers, stackNumbersOf(groups))
		return stackSync{Stack: &groups[0]}, nil
	}

	return rebuildStack(dissolve, prNumbers, baseByPR)
}

// growAnchor dissolves the rival groupings and appends the rest of the segment
// above the anchor's top.
func growAnchor(anchor *GHStack, above []int, dissolve []int, baseByPR map[int]string) (stackSync, error) {
	dissolved, err := unstackAll(dissolve)
	if err != nil {
		return stackSync{Dissolved: dissolved}, err
	}

	if len(above) == 0 {
		// The anchor already holds exactly this segment. Adopt it: record the
		// number and change nothing on GitHub. This is the path that heals a lost
		// StackNumber, and it must stay side-effect free — a repo whose only
		// problem was a stale cache should not have its PRs regrouped to fix it.
		return stackSync{Stack: anchor, Dissolved: dissolved}, nil
	}

	if len(dissolved) > 0 {
		// A PR just freed from another stack still carries the base ref that
		// stack gave it, and ghAddToStack validates that each PR's base is the
		// previous one's head. Re-point them now, while they are ungrouped and
		// their bases are mutable.
		retargetBases(above, baseByPR)
	}

	s, err := ghAddToStack(anchor.Number, above)
	return stackSync{Stack: s, Dissolved: dissolved}, err
}

// retargetBases points each PR's base at what the local graph says it should be.
func retargetBases(prNumbers []int, baseByPR map[int]string) {
	for _, n := range prNumbers {
		base, ok := baseByPR[n]
		if !ok || base == "" {
			continue
		}
		if err := ghUpdatePRBase(n, base); err != nil {
			fmt.Printf("Warning: could not retarget PR #%d to %s: %v\n", n, base, err)
		}
	}
}
