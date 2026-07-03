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

**Branch Context**:
High-level decision log for the branch — why approaches were chosen, what's related. Structured keyed entries with text, sources, and ticket references. Lives on the branch in the graph. Lost on squash unless persisted to a file.
_Avoid_: metadata (too generic), notes

**Commit Context**:
Per-step reasoning attached to individual commits via `sr commit --context`. A JSON blob of structured information explaining why a step was taken — links to plans, ADRs, tickets, or agent reasoning. Lives on the commit in the graph. Lost on squash unless persisted to a file.

**Source**:
A reference attached to a context entry identifying where it came from. Typed as file, url, ticket, or conversation.

**Post-Worktree Hook**:
A user-defined script at `.stackr/hooks/post-worktree` that runs after a worktree is created. Receives the worktree path as `$1`. Used for project-specific setup (copying `.env`, `.envrc`, editor config, etc.).

**Frozen**:
A branch excluded from automatic operations (restack, submit). No implied reason — the user decides why.

### Operations

**Restack**:
Restore the stack to a valid state by rebasing each branch onto its parent. A stackr operation, not a git command.
_Avoid_: rebase (that's the underlying git operation)

**Get**:
Pull branches from remote along the dependency path. Syncs trunk→target, then locally-existing upstack branches. Does not restack. Handles divergence by prompting or forcing. Can pause mid-walk on merge conflicts for `sr continue`.
_Avoid_: pull, fetch (those are git operations)

**Sync**:
The full "catch up with the world" sequence: fetch trunk, restack, and prune merged branches. Pruning requires confirmation. Can pause mid-way on conflicts.

**Submit**:
Push branches to the remote and manage PRs. Pushes the current branch and its downstack ancestors. Offers to push the upstack subtree. With `--stack`, includes the upstack automatically.

**Fold**:
Merge a branch into its parent. The branch is removed and its children are reparented to the parent.

**Absorb**:
Distribute staged changes to the appropriate commits in the stack.

**Stack Depth**:
The number of branches between a branch and trunk (inclusive of the branch, exclusive of trunk). Stored in the graph as a field on each branch entry. Updated on graph mutations (create, delete, fold, restack, track). Surfaced to the shell via the **Prompt Cache**.

### Shell Integration

**Shell Hook**:
A shell function that wraps the `sr` binary. Intercepts output markers (`__sr_cd:`) to perform actions the subprocess can't (like `cd`). Also installs a chpwd handler for prompt variable updates. Emitted by `sr shell-hook`.
_Avoid_: shell wrapper, shim

**Shell Setup** (`sr shell`):
The setup command that installs the **Shell Hook** into the user's shell rc file. Takes a shell name (`zsh`, `bash`). Interactive (TTY): offers to append the eval line to the rc file. Piped: prints the eval line for manual use.
_Avoid_: init (overloaded with `sr init`)

**Prompt Cache**:
A two-line flat file at `.git/.stackr/prompt-cache` containing the current branch name and stack depth. Written by `sr` on graph mutations. Read by the chpwd handler in the **Shell Hook** to set `SR_BRANCH` and `SR_STACK_DEPTH` without spawning a subprocess. **Local Data** — never shared.

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
- **Description** answers "what" a branch does; **Branch Context** answers "why" at the branch level; **Commit Context** answers "why" at the step level
- On squash, **Branch Context** and **Commit Context** are both lost — anything that should outlive the branch must be persisted to a file by the user or agent
- **Sync** is composed of: fetch trunk → **Restack** → prune merged (with confirmation)
- **Submit** always pushes **Downstack** ancestors, optionally pushes **Upstack** dependents
- **Prompt Cache** is a read-optimized projection of the graph — **Local Data**, written on graph mutations, consumed by the **Shell Hook**
- **Stack Depth** lives in the graph (shared) and is projected into the **Prompt Cache** (local) for shell access
- **Shell Setup** installs the **Shell Hook**; the hook calls `sr shell-hook` for the actual function code

## Example dialogue

> **Dev:** "I'm on `feat-api`. What's my stack?"
> **Domain:** "Your **downstack** is `feat-models` → `main`. Your **upstack** is `feat-api-tests` and `feat-api-docs`."

> **Dev:** "If I run `sr submit --stack`, what gets pushed?"
> **Domain:** "Your **downstack** ancestors (`feat-models`), your branch, and the full **upstack** subtree (`feat-api-tests`, `feat-api-docs`) — all without prompting."

> **Dev:** "What if I just run `sr submit`?"
> **Domain:** "Your **downstack** ancestors and your branch get pushed. Then it asks if you want to push the **upstack** too."

> **Dev:** "I ran `sr commit --context '{...}'`. Where does that context live?"
> **Domain:** "On the commit in the graph as **Commit Context**. It's there while the branch exists. If you squash, it's gone — persist anything important to a file first."

## Flagged ambiguities

- **"stack" as verb vs. noun** — resolved: the noun means the dependency structure; "restack" is the operation that restores it to validity. Never use "stack" as a verb meaning "to add a branch."
- **"rebase" vs. "restack"** — resolved: rebase is git's operation; restack is stackr's higher-level operation that coordinates multiple rebases.
- **"context" ambiguity** — resolved: two levels exist. **Branch Context** is high-level (decisions, approach). **Commit Context** is per-step (plan references, agent reasoning). Both are structured JSON, both live in the graph, both are lost on squash.
