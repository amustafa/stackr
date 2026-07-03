# Builder spir-3 Thread

## Phase: Specify
Starting specification for issue #3 — redesign `sr get` into a stack-aware remote sync.

### Codebase exploration
- Current `sr get` is simple: fetch + checkout/create single branch + track. ~50 lines in `internal/engine/get.go`.
- `sr sync` exists but only handles trunk fetch + restack + prune. Doesn't do per-branch remote sync.
- Navigation pattern (`NavigateResult` + `handleNavigateResult` + `__sr_cd:`) is well-established for worktree CD.
- `RebaseState` in store provides pause/resume pattern for conflict mid-walk.
- `IsAncestor` already exists in git helpers. Need `MergeFF`, `Merge`, `HasDiverged`.
- Graph has `Downstack()`, `Upstack()`, `UpstackTopo()` for walk algorithms.
- PR info store exists with `BranchPR` struct containing PR number and branch mapping.
- ADR-0001: metadata lives in `refs/stackr/data`, shared via git push/fetch.
