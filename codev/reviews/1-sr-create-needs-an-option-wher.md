# Review: `sr create --stay` flag

**Spec ID**: 1
**Issue**: #1
**Protocol**: SPIR

## Summary

Added `--stay` flag to `sr create` that creates a branch without checking it out. Supports all combinations with `--worktree` and `--insert`. Two files modified, one test file created.

## What Changed

### `cmd/create.go`
- Added `createFlagStay` variable and `--stay` flag registration
- Wired flag to `CreateOpts.Stay`

### `internal/engine/create.go`
- Added `Stay bool` to `CreateOpts` struct
- When `Stay` is true: uses `git branch` (CreateBranch) instead of `git checkout -b` (CheckoutNew)
- `branchRev = parentRev` optimization avoids extra `git rev-parse` call
- When `Stay && Worktree`: skips the checkout round-trip, calls `WorktreeAdd` directly
- When `!Stay && Worktree`: existing code preserved exactly (checkout-new → checkout-back → worktree-add)
- Output message includes "(stayed on <branch>)" for `--stay` without `--worktree`

### `internal/engine/create_test.go` (new)
- 6 integration tests covering all behavior matrix rows plus edge cases
- Each test creates a temporary git repo with full stackr initialization
- Tests verify: branch existence, current branch, graph state, worktree directories, insert reparenting

## Spec Compliance

| Criterion | Status |
|---|---|
| `sr create foo` unchanged | Verified (test + manual) |
| `sr create --stay foo` creates branch, stays | Verified (test + manual) |
| `sr create --worktree foo` unchanged | Verified (test + manual) |
| `sr create --stay --worktree foo` works | Verified (test + manual) |
| `sr log` correct after all variants | Verified (manual) |
| Existing tests pass | 35 pre-existing + 6 new = 41 total |
| `--stay --insert` reparents correctly | Verified (test + manual) |
| Output messages indicate stay | Verified (manual) |

## Architecture Documentation Updates

No architecture changes needed. This is a small feature addition that doesn't alter system shape or introduce new invariants. ADR-0006 (worktree hooks) was consulted and confirmed satisfied — `WorktreeAdd` handles hooks internally.

## Lessons Learned

### What went well
- The spec's behavior matrix was invaluable — it eliminated ambiguity about all flag combinations upfront
- The existing `CreateBranch` method in `internal/git/branch.go` was already available, making the implementation straightforward
- The `branchRev = parentRev` optimization (suggested by all three reviewers) simplified the code

### What was challenging
- Porch's default checks assumed npm (this is a Go project) — required architect intervention to add `porch.checks` overrides to `.codev/config.json`
- Codex consultation consistently failed with 401 Unauthorized, causing porch to loop on REQUEST_CHANGES verdicts — required architect force-advances twice
- Gemini (agy mode) spent its context navigating the filesystem without producing review output

### Methodology improvements
- Go projects need `.codev/config.json` porch check overrides set up before spawning builders — add to project init checklist
- When a consultation backend is persistently unavailable, porch should have a way to mark it as skipped rather than parsing empty output as REQUEST_CHANGES
