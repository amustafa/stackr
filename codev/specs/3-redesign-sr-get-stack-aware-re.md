# Spec 3: Redesign `sr get` — Stack-Aware Remote Sync

## Problem Statement

### Current State
`sr get` is a simple "fetch one branch from remote" command. It either creates a local branch tracking the remote or (with `-f`) overwrites the local branch. It has no awareness of the stack dependency graph — it operates on a single branch in isolation.

### Desired State
`sr get` becomes a **stack-aware remote sync** that walks the dependency path from trunk to the target branch, reconciling each branch with its remote counterpart. It handles divergence gracefully (prompting the user or forcing), supports worktree placement, and can optionally sync upstack branches.

### Why This Matters
The current design conflates two distinct operations:
- **Pulling from remote** — getting other people's changes
- **Local rebasing** — keeping your stack consistent

This redesign cleanly separates responsibilities:
- **`sr get`** — pull from remote (no rebasing)
- **`sr restack`** — local-only rebasing (unchanged)
- **`sr sync`** — pull from remote + restack (combines both)

Without this separation, users who want to pull a colleague's branch must manually figure out the dependency chain, fetch each branch, and handle divergence themselves.

## Stakeholders
- **Primary**: Developers working on shared stacks across a team
- **Secondary**: Solo developers pulling their own branches across machines

## Constraints
- Per ADR-0001: all shared metadata lives in `refs/stackr/data`; branch discovery uses the shared graph
- Must reuse existing navigation pattern (`NavigateResult` + `handleNavigateResult` + `__sr_cd:`)
- Must reuse existing pause/resume pattern (`RebaseState` via `sr continue`)
- Must not modify `sr restack` or `sr sync` semantics (those are separate concerns)
- Must guard against concurrent state: error if `HasRebaseState()` is true (existing rebase/get conflict in progress)
- The old `--restack` flag is removed — restacking after get is now `sr restack`'s job

## Solution Design

### Command Signature
```
sr get [branch|PR#] [flags]
```

### Core Algorithm: Downstack Walk + Sync

Given a target branch, the algorithm:
1. Guard: error if `HasRebaseState()` is true (resolve existing conflict first)
2. Fetch from remote, pull shared metadata (`TryPullMeta`)
3. Resolves the target (branch name, PR number, or current stack)
4. Computes the **walk path**: `Downstack(target)` returns target→trunk; reverse it to get trunk→target order
5. For each branch on the path (skipping trunk itself), performs a **per-branch sync**:
   - Skip if branch is trunk (trunk sync is `sr sync`'s job)
   - Skip if branch doesn't exist on remote (local-only branch)
   - Fast-forward if remote is strictly ahead
   - Skip if already up-to-date
   - Prompt on divergence (replace with remote / keep local / merge)
   - **Update graph**: set `BranchRevision` to the new HEAD after sync
6. Optionally syncs local upstack branches (beyond the target), including all forks
7. Navigates to the target branch (checkout or worktree CD)

**Frozen branches**: `sr get` is an explicit user-initiated operation, not automatic — frozen branches on the walk path are synced normally. Frozen only excludes branches from `sr restack` and `sr submit`.

### Per-Branch Sync Logic
For each branch on the walk path:

```
local_rev  = rev-parse(branch)
remote_rev = rev-parse(remote/branch)

if local_rev == remote_rev:
    skip (up to date)
else if is_ancestor(local_rev, remote_rev):
    fast-forward: reset branch to remote_rev
else if is_ancestor(remote_rev, local_rev):
    local is ahead — skip (nothing to pull)
else:
    DIVERGED — prompt user:
      1. Replace with remote (discard local)
      2. Keep local (skip this branch)
      3. Merge remote into local
```

With `--force`: always replace with remote, no prompts.

### Branch Discovery & Target Resolution

**PR number resolution**: A bare integer argument is treated as a PR number.
1. Look up PR number → branch name in the `PRInfo` store
2. Fallback: query `gh pr view <number> --json headRefName`
3. If both branch name and PR number are provided and conflict, PR number wins

**Branch not in graph** (remote-only branch not yet tracked):
1. Shared metadata was already pulled at the start; check the refreshed graph
2. If branch appears in the refreshed graph, use its stack position
3. If still not in graph and interactive: prompt — skip tracking, infer stack from PR base, or stack on trunk
4. If still not in graph and non-interactive (`--force`): default to stacking on trunk

**No argument** (`sr get` with no branch):
- Sync the current stack: walk the current branch's downstack path, then sync upstack branches that exist locally

### Upstack Behavior

- **Default** (no flags): After syncing trunk→target, also sync any **locally existing** branches upstack of the target (including all forks — `Upstack()` returns BFS over the full subtree)
- **`--downstack`**: Only sync trunk→target, skip all upstack branches
- **`--remote-upstack` / `-u`**: Also pull upstack branches that **only exist on remote** (creating them locally and tracking them)

### Worktree-Aware Sync

When a branch on the walk path is checked out in a worktree:
1. Detect dirty state in that worktree (using a `Runner` pointed at the worktree dir)
2. If dirty and interactive: prompt — skip this branch, or stash and continue
3. If stash: stash push → sync the branch → stash pop
4. If stash pop has conflicts: pause and save state for `sr continue`
5. Continue to next branch

### Target Navigation

After sync completes:
- **Default**: checkout target (or CD to its worktree if one exists)
- **`--worktree`**: create worktree if none exists, CD there; reuse existing worktree if present
- **`--stay`**: no navigation at all
- **`--worktree` without branch arg**: error

### Conflict Pause/Resume State

Extend the existing `RebaseState` pattern with a new `GetState` (or extend `RebaseState` itself):

```go
type GetState struct {
    Operation     string   // "get"
    OrigBranch    string   // where the user was before sr get
    Target        string   // the requested target branch
    WalkPath      []string // full walk path
    Completed     []string // branches already synced
    CurrentBranch string   // branch where conflict occurred
    Flags         GetFlags // preserved flags for resume
}
```

When a merge conflict occurs mid-walk:
1. Save `GetState` to store
2. Print instructions: "Resolve conflicts, then run `sr continue`"
3. `sr continue` detects the "get" operation, resumes from the next branch

### Flags

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--downstack` | | bool | false | Only sync trunk→target, skip local upstack |
| `--remote-upstack` | `-u` | bool | false | Also pull upstack branches that only exist on remote |
| `--worktree` | | bool | false | Place target in a worktree and CD there |
| `--stay` | | bool | false | Don't navigate to target after sync |
| `--force` | `-f` | bool | false | Always replace with remote, no prompts |

### Return Value

The engine function returns a `GetResult`:
```go
type GetResult struct {
    NavigateResult NavigateResult // for handleNavigateResult
    Synced         []string       // branches that were synced
    Skipped        []string       // branches skipped (up-to-date or user chose keep)
    Created        []string       // new branches created from remote
    Conflicts      bool           // true if paused for conflict resolution
}
```

The cmd layer uses `handleNavigateResult` to emit `__sr_cd:` for worktree navigation.

## Files to Modify

| File | Change |
|------|--------|
| `cmd/get.go` | New flags, accept optional arg (0 or 1), return `GetResult`, emit `__sr_cd:` via `handleNavigateResult` |
| `internal/engine/get.go` | Full rewrite: walk algorithm, per-branch sync, divergence detection |
| `internal/engine/get_state.go` (new) | `GetState` struct and store read/write/clear methods |
| `internal/git/git.go` or new file | New helpers: `MergeFF`, `Merge`, `HasDiverged` |
| `internal/store/pr_info.go` | PR number → branch name lookup helper |
| `internal/engine/continue.go` (new or extend) | Handle "get" operation in `sr continue`; dispatch on `rs.Operation` field |
| `internal/store/get_state.go` (new) | `GetState` store read/write/clear — mirrors `RebaseState` pattern |
| `codev/UBIQUITOUS_LANGUAGE.md` | Add **Get** operation term |

## Open Questions

### Important (affects design)
- **Merge strategy for diverged branches**: The spec says "merge" as one option. Should this be `git merge` (creating a merge commit) or should it offer `git rebase` as well? → **Decision: `git merge` only** — rebasing is `sr restack`'s job. Keep the separation clean.

### Nice-to-know (optimization)
- **Batch fetch**: Should we fetch all branches in one `git fetch` call (with multiple refspecs) or fetch per-branch? → **Decision: single `git fetch` at the start** fetches everything; per-branch sync just compares local vs remote refs.
- **Graph refresh frequency**: Should we `TryPullMeta` once at the start or per-branch? → **Decision: once at the start**, before computing the walk path.

## Success Criteria

### Functional Requirements (MUST)
1. `sr get <branch>` syncs the full downstack path (trunk→branch) from remote
2. `sr get <branch>` also syncs locally-existing upstack branches by default
3. `sr get --downstack <branch>` syncs only trunk→branch
4. `sr get -u <branch>` also pulls remote-only upstack branches
5. `sr get <PR#>` resolves PR number to branch name and syncs
6. `sr get` (no args) syncs the current stack
7. Fast-forward when remote is strictly ahead
8. Prompt on divergence with replace/keep/merge options
9. `sr get -f` replaces without prompting
10. `sr get --worktree <branch>` creates/reuses worktree and CDs there
11. `sr get --stay <branch>` syncs without navigation
12. `sr get --worktree` (no branch) errors
13. Conflict mid-walk saves state; `sr continue` resumes
14. Worktree-aware: detects dirty worktrees on the walk path, prompts accordingly

### Functional Requirements (SHOULD)
1. Branch not in graph: prompt for tracking strategy
2. PR number resolution falls back to `gh` CLI if not in store
3. `sr get --worktree --stay <branch>` creates worktree without CDing

### Non-Functional Requirements
- No performance regression for the simple case (`sr get <branch>` where branch is a leaf off trunk)
- Interactive prompts are skippable via `--force` for scripting/CI use

## Verification Scenarios

| # | Scenario | Input | Expected Behavior |
|---|----------|-------|-------------------|
| 1 | New branch from remote | `sr get feature-x` (remote-only) | Creates local, tracks, checks out |
| 2 | Existing branch sync | `sr get feature-x` (remote ahead) | Fast-forwards |
| 3 | Diverged branch | `sr get feature-x` (diverged) | Prompts replace/keep/merge |
| 4 | Force mode | `sr get -f feature-x` | Replaces without prompting |
| 5 | Worktree create | `sr get --worktree feature-x` | Creates worktree, CDs there |
| 6 | Worktree reuse | `sr get --worktree feature-x` (wt exists) | CDs to existing worktree |
| 7 | Stay flag | `sr get --stay feature-x` | Syncs, stays on current branch |
| 8 | Worktree + stay | `sr get --worktree --stay feature-x` | Creates worktree, stays in current dir |
| 9 | PR number | `sr get 42` | Resolves to branch, syncs |
| 10 | No argument | `sr get` | Syncs current stack |
| 11 | Downstack only | `sr get --downstack feature-x` | Skips upstack |
| 12 | Remote upstack | `sr get -u feature-x` | Pulls remote-only upstack branches |
| 13 | Dirty worktree on path | mid-walk dirty worktree | Prompts skip/stash |
| 14 | Conflict mid-walk | merge conflict during sync | Saves state, `sr continue` resumes |
| 15 | Worktree no branch | `sr get --worktree` | Error: branch required |
| 16 | Up-to-date | `sr get feature-x` (already synced) | Skips, reports up-to-date |
| 17 | Local ahead | `sr get feature-x` (local ahead of remote) | Skips (nothing to pull) |
| 18 | Existing rebase state | `sr get feature-x` while rebase in progress | Error: resolve existing conflict first |
| 19 | Frozen branch on path | walk path includes frozen branch | Synced normally (get is explicit) |
| 20 | Branch deleted on remote | `sr get feature-x` (graph-tracked but remote-deleted) | Skip with warning |
| 21 | Upstack with forks | `sr get feature-x` (upstack forks) | Syncs all fork branches |

## Consultation Log

### Iteration 1 — Claude Review (COMMENT, HIGH confidence)
**Key feedback addressed:**
- **(B) Frozen branches**: Added explicit statement that frozen branches are synced normally since `sr get` is explicit, not automatic
- **(D) Existing rebase state guard**: Added `HasRebaseState()` check as step 1 of core algorithm and in Constraints
- **(G) Graph state updates**: Added explicit "Update graph: set `BranchRevision`" step after each per-branch sync
- **(A) Downstack ordering**: Clarified that `Downstack()` returns target→trunk and must be reversed
- **(C) --restack removal**: Added to Constraints section
- **(H) Upstack forks**: Explicitly stated that default syncs all forks via BFS
- **(J) Non-interactive untracked branches**: Added force-mode default (stack on trunk)
- **(K) Trunk handling**: Algorithm now explicitly skips trunk (trunk sync is `sr sync`'s job)
- **(L) Remote-deleted branch**: Added verification scenario #20

**Gemini**: Skipped (agy CLI not available)
**Codex**: Skipped (auth failure)
