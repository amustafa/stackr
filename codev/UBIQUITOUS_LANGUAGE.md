# Stackr

Stacked-branch workflow manager for Git. Organizes branches into hierarchical dependency trees and automates the tedious parts of keeping them in sync.

## Language

### Structure

**Trunk**:
The main integration branch (e.g., `main`). The root of all stacks. Exactly one per repository.
_Avoid_: base, master (as a domain term)

**Stack**:
The set of unmerged branches a given branch depends on, plus the full subtree that depends on it. Looking downward it's always linear (one path to trunk). Looking upward it can fork.
_Avoid_: chain (for the full structure), tree (implies no directionality)

**Upstack**:
The subtree of branches that depend on the current branch. Can be multiple branches (forks).
_Avoid_: children (too narrow — upstack includes grandchildren and beyond)

**Downstack**:
The linear chain of ancestor branches from the current branch down to trunk.
_Avoid_: parents (too narrow — downstack includes the full lineage)

**Bottom**:
The first branch directly off trunk in a stack. The anchor point of the stack.

**Top**:
A leaf branch — one with no dependents.

### Branch Metadata

**Description**:
A branch's objective — what it aims to accomplish. A single string.
_Avoid_: title, summary

**Context**:
A branch's decision log — structured keyed entries recording why choices were made and what's related. Each entry has a key, text, optional sources, and optional ticket references.
_Avoid_: metadata (too generic), notes

**Source**:
A reference attached to a context entry identifying where it came from. Typed as file, url, ticket, or conversation.

**Frozen**:
A branch excluded from automatic operations (restack, submit). No implied reason — the user decides why.

### Operations

**Restack**:
Restore the stack to a valid state by rebasing each branch onto its parent. A stackr operation, not a git command.
_Avoid_: rebase (that's the underlying git operation)

**Sync**:
The full "catch up with the world" sequence: fetch trunk, restack, and prune merged branches. Pruning requires confirmation. Can pause mid-way on conflicts.

**Submit**:
Push branches to the remote and manage PRs. Pushes the current branch and its downstack ancestors. Offers to push the upstack subtree. With `--stack`, includes the upstack automatically.

**Fold**:
Merge a branch into its parent. The branch is removed and its children are reparented to the parent.

**Absorb**:
Distribute staged changes to the appropriate commits in the stack.

### Storage

**Shared Metadata**:
The branch graph, config, and PR info — stored as git objects behind `refs/stackr/data`. Travels with push/pull.

**Local Data**:
Undo snapshots and rebase state — stored on the filesystem under `.git/.stackr/`. Per-machine, never shared.

**Snapshot**:
A JSON serialization of the graph at a point in time, used for undo. Taken before every graph-mutating operation.

## Relationships

- A **Stack** is rooted at a **Trunk** child and extends upward through the full dependency subtree
- Every tracked branch has exactly one parent (except **Trunk**)
- A **Frozen** branch is skipped by **Restack** and **Submit** but can still be navigated and annotated
- **Description** answers "what" a branch does; **Context** answers "why" decisions were made
- **Sync** is composed of: fetch trunk → **Restack** → prune merged (with confirmation)
- **Submit** always pushes **Downstack** ancestors, optionally pushes **Upstack** dependents

## Example dialogue

> **Dev:** "I'm on `feat-api`. What's my stack?"
> **Domain:** "Your **downstack** is `feat-models` → `main`. Your **upstack** is `feat-api-tests` and `feat-api-docs`."

> **Dev:** "If I run `sr submit --stack`, what gets pushed?"
> **Domain:** "Your **downstack** ancestors (`feat-models`), your branch, and the full **upstack** subtree (`feat-api-tests`, `feat-api-docs`) — all without prompting."

> **Dev:** "What if I just run `sr submit`?"
> **Domain:** "Your **downstack** ancestors and your branch get pushed. Then it asks if you want to push the **upstack** too."

## Flagged ambiguities

- **"stack" as verb vs. noun** — resolved: the noun means the dependency structure; "restack" is the operation that restores it to validity. Never use "stack" as a verb meaning "to add a branch."
- **"rebase" vs. "restack"** — resolved: rebase is git's operation; restack is stackr's higher-level operation that coordinates multiple rebases.
