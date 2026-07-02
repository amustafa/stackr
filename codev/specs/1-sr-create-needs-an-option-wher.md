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

1. **`--stay` without `--worktree`**: Creates the branch using `git branch` instead of `git checkout -b`, keeping the user on their current branch. The branch is registered in the stack graph exactly as it would be with a normal create.

2. **`--stay` with `--worktree`**: Identical to `--worktree` alone — both create the worktree and leave the user on the original branch. The `--stay` flag is redundant but harmless in this combination (no error, no warning).

3. **`--worktree` alone (no behavior change)**: Continues to work as it does today — creates branch, creates worktree, user stays on original branch.

## Implementation Approach

### Approach: Modify `Create` to use `git branch` when `--stay` is set

In `internal/engine/create.go`:
- Add `Stay bool` to `CreateOpts`
- When `Stay` is true (and `Worktree` is false): use `c.Git.CreateBranch()` instead of `c.Git.CheckoutNew()`, skip the checkout entirely
- When `Stay` is true and `Worktree` is true: same as `Worktree` alone — the branch is created, worktree is set up, user stays on original branch
- When `Worktree` is true (regardless of `Stay`): the current worktree path already handles this correctly — it creates the branch via checkout, then switches back. With `--stay`, we can simplify by using `CreateBranch` directly and skipping the checkout-then-switch-back dance.

In `cmd/create.go`:
- Add `--stay` flag registration

### Trade-offs

**Simplicity vs. optimization for `--worktree`**: The current `--worktree` path does `checkout -b` → `checkout back` → `worktree add`. With `--stay`, we could simplify to `branch create` → `worktree add`. Both produce the same result, but the simplified path avoids an unnecessary checkout round-trip. We'll use the simplified path when `--stay` or `--worktree` is set.

## Constraints

- Must not change existing behavior of `sr create` or `sr create --worktree` when `--stay` is not provided
- Must maintain correct stack graph state (parent/child relationships, revisions)
- Must work with `--insert` mode
- Must work with commit flags (`-m`, `-a`, `-u`, `-p`) — staged changes are committed on the current branch *before* the new branch is created, so `--stay` doesn't affect this flow
- See ADR-0006 for worktree hook behavior — `--stay --worktree` must still run the post-worktree hook

## Success Criteria

1. `sr create foo` continues to work exactly as before (no regression)
2. `sr create --stay foo` creates branch `foo` registered in the stack, user remains on current branch
3. `sr create --worktree foo` continues to work exactly as before (no regression)
4. `sr create --stay --worktree foo` creates branch and worktree, user remains on current branch
5. `sr log` shows the new branch correctly in the stack tree after all four variants
6. All existing tests continue to pass
7. `--stay` works correctly with `--insert` mode

## Out of Scope

- Changing how `--worktree` manages the working directory (e.g., auto-`cd` into the worktree)
- Adding `--stay` to other commands
- Shell hook integration for automatic directory changes

## Open Questions

None — the issue description provides clear acceptance criteria and the implementation is straightforward.

## Consultation Log

*Pending initial consultation.*
