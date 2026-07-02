# Plan: `sr create --stay` flag

## Metadata
- **ID**: plan-2026-07-02-sr-create-stay
- **Status**: draft
- **Specification**: codev/specs/1-sr-create-needs-an-option-wher.md
- **Created**: 2026-07-02

## Executive Summary

Add a `--stay` boolean flag to `sr create` that creates a branch without checking it out. When combined with `--worktree`, the branch is created and a worktree is set up, but the user remains on their current branch. The implementation modifies two files (`cmd/create.go`, `internal/engine/create.go`) and adds a new test file.

## Success Metrics
- [ ] All specification criteria met (8 success criteria from spec)
- [ ] `sr create foo` unchanged (regression test)
- [ ] `sr create --stay foo` creates branch, stays on current
- [ ] `sr create --worktree foo` unchanged (regression test)
- [ ] `sr create --stay --worktree foo` creates branch + worktree, stays on current
- [ ] `sr log` shows correct stack graph in all cases
- [ ] `--stay` + `--insert` correctly reparents children
- [ ] All existing tests continue to pass

## Phases (Machine Readable)

```json
{
  "phases": [
    {"id": "phase_1", "title": "Phase 1: Core --stay Implementation"},
    {"id": "phase_2", "title": "Phase 2: Tests"}
  ]
}
```

## Phase Breakdown

### Phase 1: Core --stay Implementation
**Dependencies**: None

#### Objectives
- Add `--stay` flag to the CLI and implement the branch-creation-without-checkout logic
- Handle all four behavior matrix combinations correctly

#### Deliverables
- [ ] `Stay bool` field added to `CreateOpts` struct
- [ ] `--stay` flag registered in `cmd/create.go`
- [ ] `Create` function in `internal/engine/create.go` modified to support `--stay`
- [ ] Output messages updated for `--stay` variants

#### Implementation Details

**File: `cmd/create.go`**
- Add `createFlagStay bool` variable
- Register `--stay` flag: `createCmd.Flags().BoolVar(&createFlagStay, "stay", false, "create branch without checking it out")`
- Wire to `CreateOpts.Stay`

**File: `internal/engine/create.go`**
- Add `Stay bool` to `CreateOpts` struct
- Restructure the branch creation + checkout logic (lines ~99-155) into three paths:

  1. **`Stay && !Worktree`** (new path):
     - `c.Git.CreateBranch(branchName, "")` — creates branch at HEAD without checkout
     - `branchRev = parentRev` — optimization, no extra git call
     - Graph operations (insert, description, etc.) proceed as normal
     - Output: `Created branch "foo" on top of "main" (stayed on main)`

  2. **`Stay && Worktree`** (simplified worktree path):
     - `c.Git.CreateBranch(branchName, "")` — creates branch
     - `branchRev = parentRev`
     - Graph operations proceed as normal
     - `WorktreeAdd(c, WorktreeAddOpts{Name: branchName})` — creates worktree (hooks fire automatically)
     - Output: `Created branch "foo" with worktree (parent: main)`

  3. **`!Stay && Worktree`** (existing worktree path — unchanged):
     - Existing code preserved exactly as-is
     - `CheckoutNew` → `Checkout(current)` → `WorktreeAdd`

  4. **`!Stay && !Worktree`** (existing default path — unchanged):
     - Existing code preserved exactly as-is
     - `CheckoutNew` → continue

#### Acceptance Criteria
- [ ] `sr create foo` works identically to before (verified manually)
- [ ] `sr create --stay foo` creates branch, `git branch --show-current` shows original branch
- [ ] `sr create --worktree foo` works identically to before
- [ ] `sr create --stay --worktree foo` creates branch + worktree, user on original branch
- [ ] `sr create --stay --insert foo` correctly reparents children in graph
- [ ] `sr log` shows correct graph after each variant
- [ ] `go build ./...` passes
- [ ] `go vet ./...` passes (excluding pre-existing shell_hook issue)

#### Rollback Strategy
Revert the two modified files. No database, config, or external system changes.

#### Risks
- **Risk**: Changing the branch creation flow in `Create` breaks the default path
  - **Mitigation**: The default path (`!Stay && !Worktree`) is not touched. The `Stay` check branches early, before the existing `CheckoutNew` call.

---

### Phase 2: Tests
**Dependencies**: Phase 1

#### Objectives
- Add integration tests verifying all four behavior matrix rows plus edge cases
- Ensure regressions are caught for future changes

#### Deliverables
- [ ] New test file `internal/engine/create_test.go`
- [ ] Tests for all four behavior matrix combinations
- [ ] Test for `--stay` + `--insert` interaction
- [ ] All tests pass

#### Implementation Details

**File: `internal/engine/create_test.go`** (new file)

Test infrastructure:
- Each test creates a temporary git repository with `sr init`
- Creates an initial commit so we have a valid HEAD
- Uses the existing `context.Context` and `engine.Create` API directly

Test cases:
1. **TestCreate_Default**: `sr create foo` — verify branch exists AND is checked out
2. **TestCreate_Stay**: `sr create --stay foo` — verify branch exists, current branch unchanged
3. **TestCreate_Worktree**: `sr create --worktree foo` — verify branch exists, worktree exists, current branch unchanged
4. **TestCreate_StayWorktree**: `sr create --stay --worktree foo` — verify branch exists, worktree exists, current branch unchanged
5. **TestCreate_StayInsert**: `sr create --stay --insert foo` — verify graph reparenting, current branch unchanged
6. **TestCreate_StayGraphCorrectness**: Verify `sr log` (graph read) shows correct parent/child after `--stay`

#### Acceptance Criteria
- [ ] All 6+ test cases pass
- [ ] `go test ./internal/engine/...` passes
- [ ] Tests are independent (each sets up and tears down its own repo)

#### Test Plan
- **Unit Tests**: Not applicable (engine functions operate on real git repos)
- **Integration Tests**: All 6 cases above — each creates a real temporary git repo
- **Manual Testing**: Run the actual `sr` binary with all four flag combinations

#### Rollback Strategy
Delete the test file. No production code impact.

#### Risks
- **Risk**: Test setup complexity (creating temporary git repos with stackr initialization)
  - **Mitigation**: Study existing test patterns in `internal/store/integration_test.go` and `internal/git/plumbing_test.go` for repo setup helpers

## Dependency Map
```
Phase 1 ──→ Phase 2
```

## Resource Requirements
### Development Resources
- **Expertise**: Go, cobra CLI framework, git internals
- **Environment**: Local development with git

### Infrastructure
- No database changes
- No new services
- No configuration changes
- No monitoring additions

## Risk Analysis
### Technical Risks
| Risk | Probability | Impact | Mitigation |
|------|------------|--------|------------|
| Default path regression | Low | High | Default path code not modified; integration tests verify |
| Graph state inconsistency | Low | Medium | Tests verify graph after each variant |
| Pre-existing go vet failure blocks CI | Medium | Low | Out of scope per spec; test specific packages, not `./...` |

## Validation Checkpoints
1. **After Phase 1**: Manual verification of all four flag combinations against a real git repo
2. **After Phase 2**: All automated tests pass, `go test ./internal/engine/...` green

## Documentation Updates Required
- [ ] `sr create --help` updated (automatic via cobra flag registration)

## Post-Implementation Tasks
- [ ] Manual smoke test of all four flag combinations
- [ ] Verify `sr log` output after each variant

## Expert Review

### Round 1 — Plan Consultation

**Claude (APPROVE, HIGH confidence)**:
- Plan is well-structured with full spec coverage
- Noted test fixture setup complexity — builder should study existing test patterns for repo setup
- No critical issues

**Gemini (APPROVE, HIGH confidence)**:
- Plan is technically sound and ready for implementation
- Recommended setting `user.name`/`user.email` in test git repos to avoid git config warnings
- No critical issues

**Codex**: Skipped (401 auth failure). Architect approved 2/3 consultations.

## Notes
- The pre-existing `go vet` failure in `cmd/shell_hook.go:56` is out of scope (per spec). Tests should target specific packages rather than `./...` to avoid this.
- ADR-0006 constrains that post-worktree hooks must fire — this is automatically satisfied because `WorktreeAdd` handles hooks internally.
- Test fixtures should set `user.name` and `user.email` in temporary repos (per Gemini recommendation).
