# Plan: Redesign `sr get` — Stack-Aware Remote Sync

## Metadata
- **ID**: plan-2026-07-03-sr-get-redesign
- **Status**: draft
- **Specification**: codev/specs/3-redesign-sr-get-stack-aware-re.md
- **Created**: 2026-07-03

## Executive Summary

Bottom-up implementation: git primitives → store layer → engine core → command layer → continue integration. Each phase builds on the layer below it and is independently testable. The engine core (Phase 3) contains the most complexity — the walk algorithm, per-branch sync, divergence detection, and worktree-aware sync. The command layer (Phase 4) is largely wiring. Phase 5 extends `sr continue` to handle the "get" operation.

## Success Metrics
- [ ] All 21 verification scenarios from the spec pass
- [ ] All existing tests continue to pass (no regressions)
- [ ] `sr get <branch>` works for the simple case (leaf off trunk) with no performance regression
- [ ] `--force` mode enables fully non-interactive usage

## Phases (Machine Readable)

```json
{
  "phases": [
    {"id": "phase_1", "title": "Git Primitives"},
    {"id": "phase_2", "title": "Store Layer"},
    {"id": "phase_3", "title": "Engine Core"},
    {"id": "phase_4", "title": "Command Layer"},
    {"id": "phase_5", "title": "Continue Integration"}
  ]
}
```

## Phase Breakdown

### Phase 1: Git Primitives
**Dependencies**: None

#### Objectives
- Add `MergeFF`, `Merge`, and `HasDiverged` helpers to the git `Runner`
- These are pure git operations with no dependency on stackr's graph or store

#### Deliverables
- [ ] `internal/git/merge.go` (new file) — `MergeFF`, `Merge`, `HasDiverged` methods on `Runner`
- [ ] `internal/git/merge_test.go` — unit tests for all three helpers

#### Implementation Details

**`MergeFF(branch, target string) error`**: Fast-forward `branch` to `target`. Equivalent to `git update-ref refs/heads/<branch> <target-sha>` (no checkout required). If the branch is currently checked out, use `git merge --ff-only` instead.

**`Merge(theirs string) error`**: Merge `theirs` into the current branch using `git merge --no-edit <theirs>`. Returns a `MergeConflictError` if conflicts occur (detected by exit code). The `MergeConflictError` type is new — define it in `internal/git/merge.go` alongside the helpers.

**`HasDiverged(local, remote string) (bool, error)`**: Returns true if neither is an ancestor of the other. Implementation: check `IsAncestor(local, remote)` and `IsAncestor(remote, local)` — if both are false, they've diverged.

#### Acceptance Criteria
- [ ] `MergeFF` fast-forwards a branch ref without requiring checkout
- [ ] `MergeFF` errors when not a fast-forward
- [ ] `Merge` creates a merge commit on success
- [ ] `Merge` returns `MergeConflictError` on conflict (leaves working tree in conflict state)
- [ ] `HasDiverged` correctly identifies diverged, ancestor, and identical commits
- [ ] All existing git tests still pass

#### Test Plan
- **Unit Tests**: Create test repos with known commit topologies (linear, diverged, identical). Test each helper against these topologies.

---

### Phase 2: Store Layer
**Dependencies**: None (parallel with Phase 1)

#### Objectives
- Add `GetState` persistence (pause/resume for `sr get` mid-conflict)
- Add PR number → branch name lookup helper

#### Deliverables
- [ ] `internal/store/get_state.go` (new) — `GetState` struct, `ReadGetState`, `WriteGetState`, `ClearGetState`, `HasGetState` methods on `Store`
- [ ] `internal/store/refstore.go` — add `GetState` delegation methods (following `RebaseState` pattern at lines 144-157)
- [ ] `internal/store/iface.go` — extend `Backend` interface with `ReadGetState`, `WriteGetState`, `ClearGetState`, `HasGetState`
- [ ] `internal/store/pr_info.go` — add `func (p *PRInfo) BranchForPR(number int) string` method on the struct

#### Implementation Details

**`GetState`** mirrors `RebaseState` pattern:
```go
type GetState struct {
    Operation     string   `json:"operation"`     // "get"
    OrigBranch    string   `json:"origBranch"`
    Target        string   `json:"target"`
    WalkPath      []string `json:"walkPath"`
    Completed     []string `json:"completed"`
    CurrentBranch string   `json:"currentBranch"`
    Flags         GetFlags `json:"flags"`
}

type GetFlags struct {
    Downstack    bool `json:"downstack"`
    RemoteUpstack bool `json:"remoteUpstack"`
    Worktree     bool `json:"worktree"`
    Stay         bool `json:"stay"`
    Force        bool `json:"force"`
}
```

Stored as `get_state.json` in the local store (`.git/.stackr/`), not in shared refs (per ADR-0001: local-only ephemeral data stays on filesystem).

**`BranchForPR`**: Iterate `PRInfo.Branches` map, return the branch name where `BranchPR.Number == number`. Return empty string if not found.

#### Acceptance Criteria
- [ ] `GetState` can be written, read, and cleared
- [ ] `HasGetState()` returns correct boolean
- [ ] `BranchForPR` returns correct branch name for known PR number
- [ ] `BranchForPR` returns empty string for unknown PR number
- [ ] `Backend` interface updated with new methods

#### Test Plan
- **Unit Tests**: Write/read/clear cycle for `GetState`. `BranchForPR` against a populated `PRInfo`.

---

### Phase 3: Engine Core
**Dependencies**: Phase 1 (git primitives), Phase 2 (store layer)

#### Objectives
- Full rewrite of `internal/engine/get.go` with the stack-aware walk algorithm
- This is the heart of the feature — target resolution, walk path computation, per-branch sync, worktree handling, upstack sync, and conflict state management

#### Deliverables
- [ ] `internal/engine/get.go` — rewritten with new `Get()` function signature and algorithm
- [ ] `internal/engine/get_test.go` — integration tests for the walk algorithm
- [ ] `codev/UBIQUITOUS_LANGUAGE.md` — add **Get** operation term

#### Implementation Details

**New `GetOpts`**:
```go
type GetOpts struct {
    Branch        string // target branch name or PR number (empty = current stack)
    Downstack     bool
    RemoteUpstack bool
    Worktree      bool
    Stay          bool
    Force         bool
}
```

**New `GetResult`**:
```go
type GetResult struct {
    NavigateResult NavigateResult
    Synced         []string
    Skipped        []string
    Created        []string
    Conflicts      bool
}
```

**Algorithm** (following spec steps 1-7):

1. **Guard**: Check `HasRebaseState()` and `HasGetState()` — error if either exists
2. **Fetch**: `git fetch <remote>`, `TryPullMeta()`
3. **Resolve target**:
   - If empty: use current branch, sync its full stack
   - If integer: resolve PR# via `BranchForPR`, fallback to `gh pr view`
   - If string: use as branch name
4. **Ensure target is tracked**: If not in graph, attempt discovery (metadata refresh already done). If still not in graph: prompt for tracking strategy (interactive) or stack on trunk (force mode)
5. **Compute walk path**: `Downstack(target)` → reverse → skip trunk
6. **Per-branch sync loop**: For each branch in walk path:
   - Check if branch exists on remote; skip with warning if not
   - **If branch doesn't exist locally**: create it from remote (`git checkout -b <branch> <remote>/<branch>`), track in graph, add to `Created` list, continue to next branch
   - If branch exists locally: check if it's in a worktree; handle dirty state (stash/skip prompt)
   - Compare local vs remote rev using `IsAncestor`/`HasDiverged`
   - Fast-forward, skip, or handle divergence (prompt or force-replace)
   - On merge conflict: save `GetState`, return with `Conflicts: true`
   - Update `BranchRevision` in graph
7. **Upstack sync** (unless `--downstack`): Get `Upstack(target)` from graph, apply same per-branch sync to each (skip branches without remote)
8. **Navigation** (unless `--stay` or `Conflicts`): Use `NavigateToBranch` for default; for `--worktree`, use `WorktreeAdd` then return worktree path in `NavigateResult`
9. **Persist graph**: `WriteGraph()`

#### Acceptance Criteria
- [ ] Simple case: `sr get <branch>` (leaf off trunk, remote ahead) fast-forwards
- [ ] Walk path correctly computed from trunk→target via reversed `Downstack()`
- [ ] Diverged branch triggers prompt (interactive) or replace (force)
- [ ] Graph `BranchRevision` updated after each sync
- [ ] Upstack branches synced by default, skipped with `--downstack`
- [ ] Remote-only upstack branches created with `-u`
- [ ] Worktree-aware: dirty worktrees prompt skip/stash
- [ ] Conflict mid-walk saves `GetState` for `sr continue`
- [ ] Frozen branches synced normally
- [ ] Remote-deleted branches skipped with warning
- [ ] PR number resolution works via store and `gh` fallback

#### Test Plan
- **Unit Tests**: Target resolution logic, walk path computation, per-branch sync decision (mock git)
- **Integration Tests**: End-to-end with real git repos — create scenarios for each verification scenario

---

### Phase 4: Command Layer
**Dependencies**: Phase 3 (engine core)

#### Objectives
- Rewrite `cmd/get.go` with new flags, optional arg (0 or 1), and `GetResult` handling
- Wire up `handleNavigateResult` for worktree CD

#### Deliverables
- [ ] `cmd/get.go` — rewritten with new cobra command definition

#### Implementation Details

**Key changes from current `cmd/get.go`**:
- `Args`: change from `cobra.ExactArgs(1)` to `cobra.MaximumNArgs(1)` (0 = current stack)
- Remove old `--restack` flag
- Add new flags: `--downstack`, `--remote-upstack`/`-u`, `--worktree`, `--stay`
- Keep `--force`/`-f`
- Call `engine.Get()` with new `GetOpts`
- Handle `GetResult`: call `handleNavigateResult` on `result.NavigateResult`
- Print summary: synced/skipped/created counts
- If `result.Conflicts`: print conflict instructions (already handled by engine, but cmd may add extra context)

**Validation**: `--worktree` without a branch arg → error before calling engine

#### Acceptance Criteria
- [ ] `sr get` (no args) works
- [ ] `sr get <branch>` works
- [ ] `sr get <PR#>` works (integer detection)
- [ ] `sr get --worktree` (no branch) errors with clear message
- [ ] All flags are wired correctly
- [ ] `handleNavigateResult` emits `__sr_cd:` for worktree navigation
- [ ] Summary output shows synced/skipped/created counts

#### Test Plan
- **Manual Testing**: Run against a test repo with each flag combination
- **Integration**: Verify `__sr_cd:` output for worktree navigation scenarios

---

### Phase 5: Continue Integration
**Dependencies**: Phase 2 (store layer), Phase 3 (engine core)

#### Objectives
- Extend `sr continue` to handle the "get" operation
- When `GetState` exists, resume the walk from the next branch after conflict resolution

#### Deliverables
- [ ] `internal/engine/conflict.go` — extend existing `Continue()` and `Abort()` to handle `GetState`
- [ ] `internal/engine/conflict_test.go` — tests for get-operation continue and abort flows

#### Implementation Details

`Continue()` already exists in `internal/engine/conflict.go` (line 10) and handles `RebaseState`. Extend it to dispatch on operation type:

1. Check `HasGetState()` first (get state takes priority since it may contain an in-progress merge)
2. If get state exists:
   - If merge is in progress, complete it (`git commit --no-edit` to finalize the resolved merge)
   - Update `BranchRevision` in graph for the resolved branch
   - Resume the walk from `GetState.CurrentBranch` (find its position in `WalkPath`, continue from next)
   - Clear `GetState` when walk completes
   - Return `GetResult` with navigation info
3. If rebase state exists: fall through to existing rebase-continue logic (unchanged)
4. If neither: error "nothing to continue" (existing behavior)

Also extend `Abort()` (line 65 of `conflict.go`) to handle `GetState`:
- If get state exists: abort the in-progress merge (`git merge --abort`), clear `GetState`, return to `OrigBranch`
- If rebase state exists: fall through to existing abort logic

#### Acceptance Criteria
- [ ] `sr continue` after a get-merge conflict resumes the walk
- [ ] Graph is updated for the conflict-resolved branch
- [ ] Walk completes from where it left off
- [ ] `GetState` is cleared on successful completion
- [ ] Navigation happens after successful completion (unless `--stay`)
- [ ] Existing rebase-continue still works unchanged
- [ ] `sr abort` during a get-merge conflict clears `GetState` and restores original branch
- [ ] Existing rebase-abort still works unchanged

#### Test Plan
- **Integration Tests**: Create a merge conflict during `sr get`, resolve it, run `sr continue`, verify walk resumes. Also test `sr abort` cancels the operation cleanly.

## Dependency Map
```
Phase 1 (Git Primitives) ──┐
                           ├──→ Phase 3 (Engine Core) ──→ Phase 4 (Command Layer)
Phase 2 (Store Layer) ─────┘                     │
                                                  └──→ Phase 5 (Continue Integration)
```

Phases 1 and 2 can be built in parallel (no mutual dependencies).

## Risk Analysis

### Technical Risks
| Risk | Probability | Impact | Mitigation |
|------|------------|--------|------------|
| Merge conflict in worktree requires complex state management | M | M | Follow existing `RebaseState` pattern closely; test with real worktrees |
| `HasDiverged` edge cases with merge commits | L | M | Use `IsAncestor` bidirectionally — well-tested git primitive |
| `gh pr view` fallback may be slow or unavailable | L | L | Primary resolution via `PRInfo` store; `gh` is best-effort fallback |

## Validation Checkpoints
1. **After Phase 1**: Git helpers work correctly in isolation with test repos
2. **After Phase 2**: Store operations round-trip correctly
3. **After Phase 3**: Core walk algorithm passes all verification scenarios
4. **After Phase 4**: Full CLI works end-to-end with all flags
5. **After Phase 5**: `sr continue` correctly resumes interrupted get operations

## Documentation Updates Required
- [ ] `codev/UBIQUITOUS_LANGUAGE.md` — add **Get** operation term
- [ ] CLI help text updated via cobra command definitions

## Expert Review

### Iteration 1 — Claude Review (COMMENT, HIGH confidence)
**Key feedback addressed:**
- **(A) RefStore delegation**: Added `internal/store/refstore.go` to Phase 2 deliverables — GetState needs Store + RefStore + Backend interface updates
- **(B) Continue() exists**: Fixed Phase 5 to extend existing `Continue()` in `conflict.go` (line 10), not create a new file
- **(C) Branch creation path**: Added "branch doesn't exist locally" step to per-branch sync loop (creates from remote)
- **MergeConflictError**: Noted as new type to define in Phase 1
- **Abort() for GetState**: Added to Phase 5 — extend existing `Abort()` in `conflict.go` to handle GetState
- **BranchForPR**: Clarified as method on `*PRInfo` struct (not on Store)

**Gemini**: Skipped (agy CLI not available)
**Codex**: Skipped (auth failure)

## Change Log
| Date | Change | Reason |
|------|--------|--------|
| 2026-07-03 | Initial plan draft | Based on approved spec |
| 2026-07-03 | Plan with multi-agent review | Address Claude review feedback |
