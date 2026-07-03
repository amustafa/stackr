# Specification: `sr create --stay` flag

**Spec ID**: 1
**Issue**: #1 — `sr create` needs an option where the new branch is not automatically selected
**Protocol**: SPIR

## Problem Statement

When `sr create` is called, it always checks out the newly created branch. Users sometimes want to create a branch for later use without disrupting their current working context. This is especially relevant when:

- Preparing multiple branches in advance before starting work
- Creating branches for collaborators without switching context
- Using `--worktree` and wanting to remain in the current directory

## Current Behavior

| Command | Behavior |
|---|---|
| `sr create foo` | Creates branch `foo`, checks it out |
| `sr create --worktree foo` | Creates branch `foo`, checks it out, then switches back to original branch, creates a worktree |

Note: `sr create --worktree` already switches back to the original branch internally (create.go lines 138-139), but the user still ends up in the worktree's parent — the function returns without changing the working directory. The shell can't `cd` from inside a subprocess, so the user has to navigate manually.

## Proposed Solution

Add a `--stay` boolean flag that prevents `sr create` from checking out the newly created branch.

### Behavior Matrix

| Command | Creates branch | Checks out | Creates worktree | User ends on |
|---|---|---|---|---|
| `sr create foo` | Yes | Yes | No | `foo` |
| `sr create --stay foo` | Yes | No | No | original branch |
| `sr create --worktree foo` | Yes | No | Yes | original branch (worktree exists at `.worktrees/foo`) |
| `sr create --stay --worktree foo` | Yes | No | Yes | original branch (worktree exists at `.worktrees/foo`) |

### Key Design Decisions

1. **`--stay` without `--worktree`**: Creates the branch using `git branch` instead of `git checkout -b`, keeping the user on their current branch. The branch is registered in the stack graph exactly as it would be with a normal create. Since `git branch` doesn't care about working tree state (unlike `git checkout -b`), `--stay` is less likely to fail due to dirty working tree — this is a feature, not a bug.

2. **`--stay` with `--worktree`**: Identical to `--worktree` alone — both create the worktree and leave the user on the original branch. The `--stay` flag is redundant but harmless in this combination (no error, no warning).

3. **`--worktree` alone (no behavior change)**: Continues to work exactly as it does today. The existing `--worktree` implementation (checkout-new → checkout-back → worktree-add) is NOT modified. Only the `--stay` code path uses the simplified `CreateBranch` approach.

4. **`--stay` + `--insert`**: Insert mode reparents the current branch's children to the new branch in the stack graph. This is purely a graph operation — no checkout is required. With `--stay`, the user remains on the original branch, which now has only the new branch as a child, and the new branch inherits the former children. This works correctly because insert operates on graph metadata, not on the git working tree.

5. **`branchRev` when `--stay` is used**: Since `git branch foo` creates `foo` at HEAD, `branchRev == parentRev` at creation time. The implementation can use `parentRev` directly as `branchRev` to avoid an unnecessary `git rev-parse` call. This optimization only applies to the `--stay` path (and the `--stay --worktree` path); the default path continues to use `RevParse("HEAD")` after checkout.

## Implementation Approach

### Approach: Modify `Create` to use `git branch` when `--stay` is set

In `internal/engine/create.go`:
- Add `Stay bool` to `CreateOpts`
- When `Stay` is true (and `Worktree` is false): use `c.Git.CreateBranch()` instead of `c.Git.CheckoutNew()`, set `branchRev = parentRev` (no extra git call needed)
- When `Stay` is true and `Worktree` is true: use `c.Git.CreateBranch()`, then call `WorktreeAdd`, skip the checkout round-trip entirely
- When `Worktree` is true and `Stay` is false: no change — existing checkout-then-switch-back flow is preserved

In `cmd/create.go`:
- Add `--stay` flag registration
- Wire to `CreateOpts.Stay`

### Output Messages

- `sr create --stay foo` → `Created branch "foo" on top of "main" (stayed on main)`
- `sr create --stay --worktree foo` → `Created branch "foo" with worktree (parent: main)`

## Constraints

- Must not change existing behavior of `sr create` or `sr create --worktree` when `--stay` is not provided
- Must maintain correct stack graph state (parent/child relationships, revisions)
- Must work with `--insert` mode (graph-only operation, no checkout needed)
- Must work with commit flags (`-m`, `-a`, `-u`, `-p`) — staged changes are committed on the current branch *before* the new branch is created, so `--stay` doesn't affect this flow
- See ADR-0006 for worktree hook behavior — `--stay --worktree` must still run the post-worktree hook (satisfied automatically because `WorktreeAdd` handles hooks internally)

## Success Criteria

1. `sr create foo` continues to work exactly as before (no regression)
2. `sr create --stay foo` creates branch `foo` registered in the stack, user remains on current branch
3. `sr create --worktree foo` continues to work exactly as before (no regression)
4. `sr create --stay --worktree foo` creates branch and worktree, user remains on current branch
5. `sr log` shows the new branch correctly in the stack tree after all four variants
6. All existing tests continue to pass
7. `--stay` works correctly with `--insert` mode (children reparented in graph, user stays on original branch)
8. Output messages indicate the user stayed on the original branch

## Testing Strategy

No existing engine-level unit tests exist for `sr create`. The plan should include:
- Integration tests covering all four behavior matrix rows
- Verification of stack graph correctness after each variant
- `--insert` + `--stay` combination test
- Regression test for the default (no `--stay`) path

## Out of Scope

- Changing how `--worktree` manages the working directory (e.g., auto-`cd` into the worktree)
- Adding `--stay` to other commands
- Shell hook integration for automatic directory changes
- Fixing pre-existing `go vet` issue in `cmd/shell_hook.go:56` (unrelated to this feature)

## Open Questions

None — the issue description provides clear acceptance criteria and the implementation is straightforward.

## Consultation Log

### Round 1 — Initial Draft Review

**Claude (COMMENT, HIGH confidence)**:
- Identified ambiguity between trade-offs section and constraints regarding `--worktree` simplification scope
- Requested explicit description of `--stay` + `--insert` interaction
- Noted no existing engine tests — plan should include test creation
- Confirmed `branchRev == parentRev` is correct for `--stay` path

**Codex (COMMENT, HIGH confidence)**:
- Flagged pre-existing `go vet` failure in `cmd/shell_hook.go:56` (unrelated)
- Suggested `branchRev = parentRev` optimization to avoid extra git subprocess
- Confirmed `CreateBranch` exists at `internal/git/branch.go:21`
- Verified `WorktreeAdd` handles post-worktree hook internally

**Gemini (COMMENT, HIGH confidence)**:
- Confirmed technical feasibility
- Suggested `branchRev = parentRev` optimization
- Verified `--insert` works graph-only (no checkout dependency)
- Confirmed commit flags run before branch creation

**Changes made based on consultation**:
1. Clarified that `--worktree`-only path is NOT modified (only `--stay` paths use simplified flow)
2. Added explicit `--stay` + `--insert` description in Key Design Decisions
3. Added `branchRev = parentRev` optimization detail
4. Added Testing Strategy section noting absence of existing tests
5. Added pre-existing `go vet` issue to Out of Scope
6. Noted dirty working tree behavior difference as a feature
