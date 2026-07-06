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

### Sandboxing

**Sandbox**:
A disposable Docker container that runs Claude with `--dangerously-skip-permissions` on one branch's **worktree**, isolating the host while preserving the developer's normal Claude environment (config, skills, credentials, and session history). Identified by its branch name. Managed via `sr sandbox`.
_Avoid_: fork (git "fork" is a different concept), container (too generic — the sandbox is the whole disposable session, not just the container), builder (that's an `afx` agent).

**Manifest**:
The record of a sandbox — its bind mounts, launch command, and session id — stored at `.git/.stackr/sandboxes/<branch>.json`. **Local Data**. Lets a destroyed container be reconstructed and its session resumed.
_Avoid_: config (the manifest is per-sandbox runtime state, not user-chosen configuration).

**Attach**:
Connect the current terminal to a running sandbox's live session (`docker exec` + `zellij attach`). Detaching leaves the container and Claude running. Distinct from resuming, which reconstructs a session from host-side logs.

**Sandbox Config**:
User-chosen sandbox settings, split three ways: **portable** (git-ref `config.json` — network policy, base image, firewall, cache toggle, sandbox bin dir), **machine-specific** (git-ignored `.git/.stackr/sandbox.local.json` — host cache paths, extra mounts, extra PATH mounts), and **auto-derived** (never stored — worktree paths, repo root, HOME, UID:GID, session hash). The guiding rule: config holds only what needs a human decision.

**Sandbox Bin Dir / PATH Mounts**:
Two config-driven ways to put extra executables on the container's `PATH`. The **Sandbox Bin Dir** is a portable repo-local folder (default `.stackr/sandbox/bin/`), prepended to `PATH`. **PATH Mounts** are machine-specific host directories bind-mounted at their real paths and added to `PATH`. Host binaries must match the container's OS/arch/libc.

**Base Image**:
The single cached Docker image (`stackr-sandbox:base`) every sandbox runs from, holding the universal toolchain. A repo may extend it with a per-project `.stackr/sandbox/Dockerfile`. Containers differ only by their bind mounts and launch command, never by bespoke image builds.

**Sandbox Status**:
The current interaction state of a **Sandbox**, published by Claude Code hooks (loaded via a sandbox-only `--settings` file — ADR-0011) to `.git/.stackr/sandboxes/<branch>.status`. States: `working`, `awaiting-input` (a question), `awaiting-choice` (options), `exited`. Carries a `reason` — the pending question/options/summary — surfaced in `sr sandbox ls`, the attach selector, and **Watch**. **Local Data**.
_Avoid_: "waiting" (ambiguous — waiting on the agent vs. waiting on the human; the awaiting states specifically mean waiting on the human).

**Watch**:
`sr sandbox watch` — the attention surface. `--notify` runs a headless notifier firing desktop notifications on transitions into an awaiting state. Bare, it opens a live two-pane dashboard: left has an awaiting-input section over an all-sessions section, right shows the selected session's detail. Navigable by keyboard and mouse, with a hotkey to the first awaiting session and click-to-attach. Scope defaults to the current project (config), or `--all`.

**PR Suggestion**:
A proposed PR title/body a **Sandbox** records as a reserved **Branch Context** entry (key `pr`) instead of pushing — it has no credentials. Host-side **Submit** reads the entry and uses it as the PR title/body directly (skipping AI regeneration), offering to edit. Lives in the branch graph (**Shared Metadata**); the sandbox never pushes it, the host does at submit time.
_Avoid_: draft PR (a GitHub state); a separate file (it is not — it is a **Branch Context** entry).

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
- A **Sandbox** operates on exactly one branch's **worktree**; git's one-worktree-per-branch rule means one sandbox per branch
- A **Sandbox** is disposable — its durable state (the worktree, session logs in `~/.claude/projects`) is host-side, so destroying the container never loses Claude progress; the **Manifest** lets it be reconstructed
- The **Manifest** is **Local Data** (like the **Prompt Cache** and **Snapshot**); **Sandbox Config**'s portable tier is **Shared Metadata**, its machine-specific tier is **Local Data**
- A **Sandbox** has no credentials and never touches the remote; it records a **PR Suggestion** as **Branch Context** (key `pr`) that a host-side **Submit** reads to open the PR
- A **PR Suggestion** is **Branch Context**, so like all **Branch Context** it is lost on squash — the sandbox should set it after any squash, before teardown
- **Sandbox Status** is published by hooks loaded via a sandbox-only `--settings` file (ADR-0011), so the developer's `~/.claude` is never mutated; `SR_SANDBOX` only tells the hook which sandbox it is
- **Watch** and the **Prompt Cache** both consume **Sandbox Status**: the Prompt Cache surfaces an ambient awaiting-count; **Watch** surfaces the full dashboard

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
- **"fork" vs. "sandbox"** — resolved: the feature is named **Sandbox** (`sr sandbox`) end-to-end. "Fork" is eliminated from all sandbox naming (internal paths, image tags, labels) to avoid collision with git forks; the word survives only in its unrelated stack sense (an **Upstack** that branches).
- **"session" ambiguity** — a Claude session (the JSONL conversation under `~/.claude/projects`) vs. a zellij session (the multiplexer instance inside the container). The **Sandbox** hosts a zellij session that runs a Claude session; **Attach** connects to the zellij session, **resume**/`--continue` reconstructs the Claude session.
