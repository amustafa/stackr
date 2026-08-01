package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/amustafa/stackr/internal/context"
	gitpkg "github.com/amustafa/stackr/internal/git"
	"github.com/amustafa/stackr/internal/graph"
	"github.com/amustafa/stackr/internal/store"
	"github.com/amustafa/stackr/internal/ui"
)

// PreflightResult is what Submit needs to know once everything is settled.
type PreflightResult struct {
	// Ready lists the branches to push, bottom-up, each with the disposition
	// and lease pin decided for it.
	Ready []Classification

	// Dropped names branches removed from the push set, with the reason. A
	// dropped branch takes its whole lineage with it.
	Dropped []DroppedBranch

	// Stopped is true when the user chose to stop. Nothing has been pushed;
	// remediations already made are deliberately kept.
	Stopped bool

	// RollbackID identifies the token recording pre-remediation branch tips.
	RollbackID string

	// Mutated is true if any local branch was changed.
	Mutated bool
}

// DroppedBranch records a branch excluded from this submit.
type DroppedBranch struct {
	Name   string
	Reason string
}

// remediation menu labels. Deliberately flat rather than nested: preflight can
// prompt several times in one run, and a submenu doubles the keystrokes.
const (
	optStop            = "Stop                  (handle it manually, nothing is pushed)"
	optMergeSquash     = "Merge — squash        (their changes as one new commit on top)"
	optMergeCherryPick = "Merge — cherry-pick   (replay their commits individually)"
	optOverwriteLocal  = "Overwrite local       (discard your commits on this branch)"
	optOverwriteRemote = "Overwrite remote      (push yours anyway, their commits are lost)"
)

// Preflight settles every branch in the push set against its remote before
// anything is published.
//
// The order is bottom-up and the loop reloads the graph after every local
// change, because remediating a branch moves its dependents: classifying a
// branch that is about to be rewritten would be answering the wrong question,
// and Restack writes its own graph instance (restack.go), so a caller-held
// graph goes stale the moment we restack.
func Preflight(c *context.Context, opts SubmitOpts, cfg *store.Config, set []string) (*PreflightResult, error) {
	result := &PreflightResult{}

	if !c.Quiet {
		fmt.Printf("Fetching from %s...\n", cfg.Remote)
	}
	if err := c.Git.Fetch(cfg.Remote); err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}

	dropped := map[string]bool{}

	for i := 0; i < len(set); i++ {
		name := set[i]

		// Recomputed each round: a remediation can reshape the graph, and a
		// dropped branch takes its lineage with it.
		g, err := c.Store.ReadGraph()
		if err != nil {
			return nil, err
		}
		b := g.Branches[name]
		if b == nil {
			continue
		}
		if dropped[b.ParentBranchName] {
			dropped[name] = true
			result.Dropped = append(result.Dropped, DroppedBranch{name,
				"stacked on " + b.ParentBranchName + ", which was left out of this submit"})
			continue
		}

		class, err := ClassifyBranch(c, cfg.Remote, name)
		if err != nil {
			return nil, err
		}

		if class.Disposition != DispNeedsDecision {
			// --no-force means never force-push, including the losslessly
			// force-pushable case that is otherwise the whole point of this
			// feature. The flag is a contract about the operation, not about
			// the risk.
			if opts.NoForce && class.Disposition == DispPushForce {
				return result, fmt.Errorf(
					"%s needs a force push (its history was rewritten locally) — refusing with --no-force", name)
			}
			result.Ready = append(result.Ready, class)
			continue
		}

		// Content Divergence. Decide, then re-classify — the remediation may
		// have made the push trivially safe.
		action, err := decideRemediation(c, opts, name, class)
		if err != nil {
			return result, err
		}

		switch action {
		case optStop:
			result.Stopped = true
			return result, nil

		case optOverwriteRemote:
			class.Disposition = DispPushForce
			result.Ready = append(result.Ready, class)
			continue
		}

		// The remaining actions all mutate local. Record where we were first so
		// the user has a way back.
		if result.RollbackID == "" {
			// Every tracked branch, not just the push set: remediation restacks
			// the full upstack, which reaches branches this submit never
			// intended to publish. A token that only covered the push set could
			// not put those back.
			id, err := writeRollbackToken(c, trackedBranches(g))
			if err != nil && !c.Quiet {
				fmt.Printf("Note: could not write a rollback token: %v\n", err)
			}
			result.RollbackID = id
		}

		if err := applyRemediation(c, cfg, name, class, action); err != nil {
			if gitpkg.IsMergeConflict(err) {
				// A conflict mid-remediation is not a separate state to resume
				// from — it degrades into Stop, which is an option the user
				// already has.
				fmt.Printf("\n%s could not be merged cleanly. Nothing has been pushed.\n"+
					"Resolve it by hand and run `sr submit` again.\n", name)
				result.Stopped = true
				result.Mutated = true
				return result, nil
			}
			return result, err
		}
		result.Mutated = true

		// Local moved, so everything above it is stale. Restack immediately and
		// reload, or the next branch is classified in a shape it will not keep.
		if err := restackAfterRemediation(c, opts, name, dropped, result); err != nil {
			if err == errPreflightStopped {
				result.Stopped = true
				return result, nil
			}
			return result, err
		}

		if dropped[name] {
			continue
		}

		reclass, err := ClassifyBranch(c, cfg.Remote, name)
		if err != nil {
			return result, err
		}
		if reclass.Disposition == DispNeedsDecision {
			return result, fmt.Errorf(
				"%s still differs from the remote after remediation (%s) — resolve it by hand",
				name, reclass.Reason)
		}
		result.Ready = append(result.Ready, reclass)
	}

	// Drop anything whose lineage was excluded.
	filtered := result.Ready[:0]
	for _, class := range result.Ready {
		if !dropped[class.Branch] {
			filtered = append(filtered, class)
		}
	}
	result.Ready = filtered

	return result, nil
}

var errPreflightStopped = fmt.Errorf("preflight stopped")

// decideRemediation asks what to do about Content Divergence.
//
// Non-interactive submit fails rather than skipping — unlike `sr get`, which
// only mutates locally. Submit publishes, and a branch skipped mid-stack strands
// every PR above it on a base that was never updated.
func decideRemediation(c *context.Context, opts SubmitOpts, name string, class Classification) (string, error) {
	if opts.Force {
		if !c.Quiet {
			fmt.Printf("%s: %s — overwriting the remote (--force)\n", name, class.Reason)
		}
		return optOverwriteRemote, nil
	}
	if opts.NoForce {
		return "", fmt.Errorf("%s: %s (refusing to force with --no-force)", name, class.Reason)
	}
	if !c.Interactive {
		return "", fmt.Errorf("%s: %s\n"+
			"Nothing was pushed. Re-run interactively to resolve it, or pass --force to overwrite the remote.",
			name, class.Reason)
	}

	title := fmt.Sprintf("%s: %s", name, class.Reason)
	if n := len(class.RemoteOnly); n > 0 {
		title = fmt.Sprintf("%s (%d commit(s) only on the remote)", title, n)
	}

	options := []string{optStop, optMergeSquash, optMergeCherryPick, optOverwriteLocal, optOverwriteRemote}
	if len(class.RemoteOnly) == 0 {
		// With nothing to replay, cherry-pick has no meaning.
		options = []string{optStop, optMergeSquash, optOverwriteLocal, optOverwriteRemote}
	}
	return ui.Select(title, options)
}

// applyRemediation performs the chosen local change. The branch is checked out
// first: every option here rewrites the working branch.
func applyRemediation(c *context.Context, cfg *store.Config, name string, class Classification, action string) error {
	orig, _ := c.Git.CurrentBranch()
	if orig != name {
		if err := c.Git.Checkout(name); err != nil {
			return err
		}
		defer func() { _ = c.Git.Checkout(orig) }()
	}

	remoteRef := cfg.Remote + "/" + name

	switch action {
	case optMergeSquash:
		if err := c.Git.MergeSquash(remoteRef); err != nil {
			if gitpkg.IsMergeConflict(err) {
				_ = c.Git.MergeSquashAbort()
			}
			return err
		}
		msg := fmt.Sprintf("Merge remote changes into %s", name)
		if err := c.Git.Commit(msg, gitpkg.CommitOpts{}); err != nil {
			return fmt.Errorf("committing the squashed remote changes: %w", err)
		}

	case optMergeCherryPick:
		if err := c.Git.CherryPick(class.RemoteOnly...); err != nil {
			if gitpkg.IsMergeConflict(err) {
				_ = c.Git.CherryPickAbort()
			}
			return err
		}

	case optOverwriteLocal:
		if err := c.Git.ResetHard(remoteRef); err != nil {
			return err
		}
		// We now hold exactly what the remote holds, and we accept it as ours —
		// which is a Push Record write even though nothing was pushed.
		if err := c.Store.SetPushRecord(cfg.Remote, name, class.RemoteSHA); err != nil {
			return err
		}
	}

	// Keep the graph's idea of this branch's tip honest before anything reads it.
	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}
	if b := g.Branches[name]; b != nil {
		if rev, rerr := c.Git.RevParse(name); rerr == nil {
			b.BranchRevision = rev
		}
	}
	return c.Store.WriteGraph(g)
}

// restackAfterRemediation rebuilds everything above a branch we just changed.
// Blocked branches are offered to the user as stop-or-skip; skipping takes the
// whole lineage out of the push set.
func restackAfterRemediation(c *context.Context, opts SubmitOpts, name string,
	dropped map[string]bool, result *PreflightResult) error {

	before, err := branchTips(c)
	if err != nil {
		return err
	}

	if err := Restack(c, RestackOpts{Branch: name, SkipBlocked: true}); err != nil {
		return err
	}

	after, err := branchTips(c)
	if err != nil {
		return err
	}

	// A branch that should have moved but did not was blocked (dirty worktree,
	// conflict, or a frozen wall above it). Restack reported why; here we only
	// decide whether the submit continues without it.
	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}

	// The remediated branch itself can be blocked too — we just rewrote it, and
	// rebasing it onto its parent may conflict. Checking only its dependents
	// would let a branch that is not actually stacked on its parent sail
	// through, and its PR would then show its parent's commits as its own.
	subjects := append([]string{name}, g.UpstackTopo(name)...)

	for _, child := range subjects {
		b := g.Branches[child]
		if b == nil || b.IsTrunk || dropped[child] {
			continue
		}
		if isStackedOn(c, child, after[b.ParentBranchName]) {
			continue
		}
		if before[child] != after[child] {
			continue // it moved; it is fine
		}

		if !c.Interactive {
			return fmt.Errorf("%s could not be restacked onto %s — nothing was pushed",
				child, b.ParentBranchName)
		}
		choice, err := ui.Select(
			fmt.Sprintf("%s could not be restacked onto %s.", child, b.ParentBranchName),
			[]string{
				"Skip it and everything above it",
				"Stop (handle it manually, nothing is pushed)",
			})
		if err != nil {
			return err
		}
		if strings.HasPrefix(choice, "Stop") {
			return errPreflightStopped
		}
		dropped[child] = true
		result.Dropped = append(result.Dropped, DroppedBranch{child, "could not be restacked"})
	}

	return nil
}

// trackedBranches lists every non-trunk branch in the graph.
func trackedBranches(g *graph.Graph) []string {
	var names []string
	for name, b := range g.Branches {
		if b != nil && !b.IsTrunk {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func branchTips(c *context.Context) (map[string]string, error) {
	g, err := c.Store.ReadGraph()
	if err != nil {
		return nil, err
	}
	tips := map[string]string{}
	for name := range g.Branches {
		if rev, err := c.Git.RevParse(name); err == nil {
			tips[name] = rev
		}
	}
	return tips, nil
}

// buildPushSet returns the branches to submit, bottom-up.
//
// Frozen is a HOLE here, not a wall (ADR-0015): only the frozen branch itself is
// withheld, because every branch around it carries changes of its own that are
// worth publishing whether or not the frozen one moved.
func buildPushSet(c *context.Context, g *graph.Graph, cfg *store.Config, opts SubmitOpts,
	current string) ([]string, []DroppedBranch, error) {

	b := g.Branches[current]
	if b == nil {
		trunk := g.TrunkName()
		trunkExists, _ := c.Git.BranchExists(trunk)
		if trunk != "" && !trunkExists {
			return nil, nil, fmt.Errorf("branch %q not in stack graph — trunk is recorded as %q which no longer exists (renamed to %q?). Run: sr init --trunk %s --reset", current, trunk, current, current)
		}
		return nil, nil, fmt.Errorf("branch %q not found in stack graph — track it with `sr create` or `sr track`", current)
	}
	if b.IsTrunk {
		return nil, nil, fmt.Errorf("cannot submit trunk branch")
	}

	// Downstack ancestors, bottom-up, then current.
	var candidates []string
	ds := g.Downstack(current)
	for i := len(ds) - 1; i >= 0; i-- {
		if !g.IsTrunk(ds[i]) {
			candidates = append(candidates, ds[i])
		}
	}
	if opts.Stack {
		candidates = append(candidates, g.UpstackTopo(current)...)
	}

	var set []string
	var dropped []DroppedBranch
	for _, name := range candidates {
		br := g.Branches[name]
		if br == nil || br.IsTrunk {
			continue
		}
		// Submitting a frozen branch you are standing on is an explicit
		// instruction, not an automatic sweep.
		if br.Frozen && name != current {
			dropped = append(dropped, DroppedBranch{name, "frozen"})
			continue
		}
		if opts.UpdateOnly && !alreadyPublished(c, cfg, name) {
			dropped = append(dropped, DroppedBranch{name, "no PR or remote branch yet (--update-only)"})
			continue
		}
		set = append(set, name)
	}

	return set, dropped, nil
}

// alreadyPublished reports whether a branch has ever been submitted — it has a
// recorded PR, or a branch on the remote.
func alreadyPublished(c *context.Context, cfg *store.Config, name string) bool {
	if prInfo, err := c.Store.ReadPRInfo(); err == nil {
		if pr := prInfo.Branches[name]; pr != nil && pr.Number != 0 {
			return true
		}
	}
	exists, _ := c.Git.RemoteBranchExists(cfg.Remote, name)
	return exists
}

// rollbackToken records where branches stood before preflight touched them.
//
// Deliberately separate from the undo snapshot, which restores the graph only
// (ADR-0002) and so cannot undo a reset or a cherry-pick. Local, like the Push
// Record: it describes this clone's working state, not anything shared.
type rollbackToken struct {
	ID        string            `json:"id"`
	CreatedAt string            `json:"createdAt"`
	Branches  map[string]string `json:"branches"`
}

func writeRollbackToken(c *context.Context, branches []string) (string, error) {
	tok := rollbackToken{
		ID:        time.Now().UTC().Format("20060102-150405"),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Branches:  map[string]string{},
	}
	for _, name := range branches {
		if rev, err := c.Git.RevParse(name); err == nil {
			tok.Branches[name] = rev
		}
	}

	dir := filepath.Join(c.Store.Root(), "rollback")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, tok.ID+".json"), append(data, '\n'), 0o644); err != nil {
		return "", err
	}
	return tok.ID, nil
}
