# Review: Redesign `sr get` — Stack-Aware Remote Sync

## Summary

Redesigned `sr get` from a simple "fetch one branch" command into a stack-aware remote sync that walks the dependency path, reconciles each branch with its remote counterpart, and supports worktree placement and conflict pause/resume.

**Scope**: 12 Go files changed, ~1,576 lines added across 4 packages (`cmd/`, `internal/engine/`, `internal/git/`, `internal/store/`).

## What Was Built

### New Capabilities
- **Stack-aware walk**: `sr get <branch>` syncs the full dependency chain from trunk to target
- **Divergence handling**: Fast-forward, replace, keep, or merge — with interactive prompts or `--force`
- **Upstack sync**: Default syncs locally-existing upstack branches; `--downstack` to opt out; `-u` for remote-only upstack
- **PR number resolution**: `sr get 42` resolves via PR store, falls back to `gh` CLI
- **No-arg mode**: `sr get` syncs the current stack
- **Worktree support**: `--worktree` creates/reuses worktree with CD via `__sr_cd:` protocol
- **Conflict pause/resume**: Mid-walk merge conflicts save `GetState` for `sr continue`/`sr abort`
- **Worktree-aware sync**: Detects dirty worktrees on the walk path, stashes and pops

### New Infrastructure
- `MergeFF`, `Merge`, `HasDiverged`, `IsMergeInProgress` git helpers
- `MergeConflictError` typed error
- `GetState`/`GetFlags` store persistence (mirrors `RebaseState` pattern)
- `PRInfo.BranchForPR()` lookup
- Extended `Continue()`/`Abort()` dispatch for get operations

## What Went Well

1. **Bottom-up phasing worked cleanly**: git primitives → store → engine → cmd → continue. Each layer was independently testable and the dependency graph was acyclic.

2. **Claude reviews caught real issues**: The stash-without-pop gap (Phase 3) was a genuine data safety bug. The `append` slice mutation and missing `RefStore` delegation layer would have caused real problems.

3. **Existing pattern reuse**: `NavigateResult`, `RebaseState`, `TryPullMeta`, `ui.Select` — building on proven infrastructure reduced risk and kept the code consistent.

4. **Clean separation of concerns**: `sr get` (pull from remote) vs `sr restack` (local rebasing) vs `sr sync` (both). The spec's design principle held throughout implementation.

## What Was Challenging

1. **Phase 3 size**: The engine core was the largest phase — walk algorithm, per-branch sync, divergence handling, worktree awareness, upstack sync, and conflict state in one file. Sub-phasing guidance in the plan would have helped.

2. **cmd/get.go coupling**: Phase 4 (command layer) had to be implemented alongside Phase 3 because the Go compiler requires all files in a package to compile together. The old `Get()` signature returned `error`; the new one returns `(*GetResult, error)`. This forced pulling Phase 4 forward.

3. **Codex/Gemini unavailability**: 2 of 3 consultation reviewers were down throughout (Codex: 401 auth, Gemini: agy CLI missing). Claude was the only active reviewer.

## Lessons Learned Updates

1. **Stash operations need paired pop**: When stashing user changes as part of an automated operation, the contract is "I'll give them back." A stash without a pop is a data safety bug. Always design stash as a push/pop pair with the sync in between.

2. **Go `append` on returned slices is fragile**: `append(someFunc(), moreItems...)` may mutate the returned slice's backing array if it has spare capacity. Use defensive allocation (`make` + `append`) when the result will outlive the call.

3. **Three-layer store pattern must be complete**: Adding state to this codebase requires touching `Store` (implementation), `RefStore` (delegation), and `Backend` (interface). Missing any layer causes compile errors. Document this in arch-critical.

4. **Cmd layer can't be truly independent of engine signature changes**: In Go, if the engine function signature changes, the cmd layer must update in the same commit. Plan phases that assume these can be separate are unrealistic.

## Architecture Updates

### arch-critical.md candidate
- Three-layer store pattern: `Store` → `RefStore` → `Backend`. New persistent state (like `GetState`) must be added at all three levels.

### UBIQUITOUS_LANGUAGE.md
Updated with **Get** operation term — distinguishing it from Sync and Restack.

## Test Coverage

| Package | New Tests | Total |
|---------|-----------|-------|
| `internal/git` | 8 (merge helpers) | 15 |
| `internal/store` | 2 (GetState, BranchForPR) | 22 |
| `internal/engine` | 15 (get + conflict) | 29 |
| **Total** | **25 new** | **66** |

All 66 tests pass. No regressions in existing tests.

## Known Gaps / Future Work

1. **`--worktree --stay` doesn't create worktree**: Spec SHOULD #3 says it should create without CDing. Currently `--stay` skips the entire navigation block. Low priority.

2. **Test coverage for some verification scenarios**: Divergence prompting (scenario 3), PR number resolution (9), conflict mid-walk continue (14), and remote upstack (12) are tested via the engine's code paths but don't have dedicated end-to-end tests. The interactive UI prompts make some of these hard to test without mocking `ui.Select`.

3. **`exec.Command("gh")` has no timeout**: The PR number fallback to `gh pr view` could hang if `gh` is stuck on auth. Low risk since it's a fallback path.
