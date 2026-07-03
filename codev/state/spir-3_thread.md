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

### Specify phase complete
- Spec drafted, Claude review addressed 3 medium issues (frozen branches, rebase state guard, graph updates)
- Gemini/Codex unavailable (agy not installed, Codex 401 auth). Architect approved proceeding with Claude only.
- spec-approval gate approved.

## Phase: Plan
- 5 phases: git primitives → store layer → engine core → command layer → continue integration
- Phases 1-2 parallelizable (no mutual deps)
- Plan approved. Moving to implement.

## Phase: Implement
### Phase 1: Git Primitives — DONE
- `internal/git/merge.go`: MergeFF, Merge, HasDiverged, MergeConflictError, IsMergeInProgress
- 8 new tests, all passing. Claude APPROVED with HIGH confidence.

### Phase 2: Store Layer — DONE
- `internal/store/get_state.go`: GetState persistence (mirrors RebaseState)
- `internal/store/refstore.go`: RefStore delegation
- `internal/store/iface.go`: Backend interface extended
- `internal/store/pr_info.go`: BranchForPR lookup
- 2 new tests, all passing. Claude APPROVED.

### Phase 3: Engine Core — DONE
- Full rewrite of `internal/engine/get.go`: walk algorithm, per-branch sync, divergence detection
- Target resolution: string, PR#, or current stack
- Worktree-aware sync with stash→sync→pop cycle (fixed after review)
- Defensive slice allocation in upstack conflict path (fixed after review)
- cmd/get.go also rewritten (Phase 4 pulled forward for compilation)
- 8 integration tests, all passing. Claude COMMENT (fixed stash leak + append mutation).

### Phase 4: Command Layer — DONE (merged with Phase 3)
- New flags: --downstack, -u, --worktree, --stay, -f
- handleNavigateResult wiring, --worktree no-branch validation
- Claude APPROVED.

### Phase 5: Continue Integration — DONE
- Extended Continue() and Abort() in conflict.go for GetState dispatch
- continueGet: finalize merge, update graph, resume walk
- abortGet: abort merge, clear state, return to original branch
- Awaiting Claude review.
