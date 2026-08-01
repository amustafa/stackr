package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/amustafa/stackr/internal/context"
)

// RollbackOpts controls a rollback.
type RollbackOpts struct {
	ID   string // empty = most recent token
	List bool
}

// Rollback restores branch tips recorded before a Preflight mutated them.
//
// This is deliberately not `sr undo`. Undo restores the branch graph — a JSON
// snapshot of which branch depends on which (ADR-0002) — and cannot put back a
// `reset --hard` or a cherry-pick, because those change git refs rather than
// stackr metadata. A rollback token records the refs themselves.
//
// The commits are not gone in the meantime: they stay reachable through each
// branch's reflog, well inside git's default expiry, which is why the token
// only has to record SHAs rather than protect them.
func Rollback(c *context.Context, opts RollbackOpts) error {
	dir := filepath.Join(c.Store.Root(), "rollback")

	if opts.List {
		return listRollbackTokens(dir)
	}

	tok, err := loadRollbackToken(dir, opts.ID)
	if err != nil {
		return err
	}

	current, _ := c.Git.CurrentBranch()

	names := make([]string, 0, len(tok.Branches))
	for name := range tok.Branches {
		names = append(names, name)
	}
	sort.Strings(names)

	var restored, missing []string
	for _, name := range names {
		sha := tok.Branches[name]
		if !c.Git.ObjectExists(sha) {
			missing = append(missing, fmt.Sprintf("%s (%s is no longer in the repository)", name, abbrev(sha)))
			continue
		}
		if now, err := c.Git.RevParse(name); err == nil && now == sha {
			continue // already there
		}

		// The checked-out branch needs its worktree moved too; the rest are
		// plain ref updates.
		if name == current {
			if err := c.Git.ResetHard(sha); err != nil {
				return fmt.Errorf("restoring %s: %w", name, err)
			}
		} else if err := c.Git.RunGit("update-ref", "refs/heads/"+name, sha); err != nil {
			return fmt.Errorf("restoring %s: %w", name, err)
		}
		restored = append(restored, name)
	}

	// The graph's recorded tips must follow the refs, or the next restack works
	// from revisions that no longer exist.
	g, err := c.Store.ReadGraph()
	if err != nil {
		return err
	}
	for _, name := range restored {
		if b := g.Branches[name]; b != nil {
			b.BranchRevision = tok.Branches[name]
		}
	}
	if err := c.Store.WriteGraph(g); err != nil {
		return err
	}

	if !c.Quiet {
		if len(restored) == 0 {
			fmt.Printf("Nothing to restore — every branch in %s is already where it was.\n", tok.ID)
		} else {
			fmt.Printf("Restored %d branch(es) from %s: %s\n", len(restored), tok.ID, strings.Join(restored, ", "))
		}
		for _, m := range missing {
			fmt.Printf("Could not restore %s\n", m)
		}
		if len(restored) > 0 {
			fmt.Println("Branches above the restored ones may now need `sr restack`.")
		}
	}
	return nil
}

func loadRollbackToken(dir, id string) (*rollbackToken, error) {
	if id == "" {
		latest, err := latestRollbackID(dir)
		if err != nil {
			return nil, err
		}
		id = latest
	}

	data, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no rollback point %q — run `sr rollback --list` to see what is available", id)
		}
		return nil, err
	}
	var tok rollbackToken
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, fmt.Errorf("rollback point %s is unreadable: %w", id, err)
	}
	return &tok, nil
}

func latestRollbackID(dir string) (string, error) {
	ids, err := rollbackIDs(dir)
	if err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "", fmt.Errorf("no rollback points recorded")
	}
	// IDs are UTC timestamps, so lexical order is chronological.
	return ids[len(ids)-1], nil
}

func rollbackIDs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if name := e.Name(); strings.HasSuffix(name, ".json") {
			ids = append(ids, strings.TrimSuffix(name, ".json"))
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func listRollbackTokens(dir string) error {
	ids, err := rollbackIDs(dir)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		fmt.Println("No rollback points recorded")
		return nil
	}
	for _, id := range ids {
		tok, err := loadRollbackToken(dir, id)
		if err != nil {
			continue
		}
		names := make([]string, 0, len(tok.Branches))
		for name := range tok.Branches {
			names = append(names, name)
		}
		sort.Strings(names)
		fmt.Printf("%s  %s  %s\n", id, tok.CreatedAt, strings.Join(names, ", "))
	}
	return nil
}
