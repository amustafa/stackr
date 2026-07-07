# stackr (`sr`)

A local stacked-branch workflow manager for Git. Stackr organizes your branches into hierarchical stacks — parent-child relationships that let you work on dependent changes simultaneously, rebase entire chains with one command, and submit clean PRs that build on each other.

```
◉ feat-auth-ui ←         ← you are here
│
│ ◯ feat-auth-tests
├─┘
◯ feat-auth-middleware
│
◯ feat-auth-models
│
◯ main (trunk)
```

## Why Stacked Branches?

Large features rarely fit in a single PR. Stacked branches let you:

- **Ship incrementally** — each branch is a reviewable, mergeable unit
- **Work in parallel** — start the next piece before the previous one merges
- **Keep PRs small** — reviewers see focused diffs, not 2000-line monsters
- **Rebase safely** — `sr sync` updates your entire stack when trunk moves

Stackr tracks the dependency graph so you don't have to remember which branch sits on which.

## Installation

### From Source

Requires **Go 1.25.5+** and **Git**.

```bash
# Clone and install globally
git clone https://github.com/amustafa/stackr.git
cd stackr
make install    # installs `sr` to your $GOPATH/bin

# Or build locally
make build      # outputs to build/sr
```

### Shell Completions

```bash
# Bash
sr completion bash > /etc/bash_completion.d/sr

# Zsh
sr completion zsh > "${fpath[1]}/_sr"

# Fish
sr completion fish > ~/.config/fish/completions/sr.fish
```

## Quick Start

```bash
# Initialize stackr in your repo
cd my-project
sr init

# Create your first stacked branch
sr create feat-models -m "Add user model"

# Stack another branch on top
sr create feat-api -m "Add user API endpoints"

# See your stack
sr log
# ◉ feat-api ←
# │
# ◯ feat-models
# │
# ◯ main (trunk)

# Trunk moved? Sync everything
sr sync

# Push and open PRs for the whole stack
sr submit --stack
```

## Core Concepts

### The Stack Graph

Stackr maintains a directed acyclic graph of your branches. Every tracked branch has exactly one parent (except trunk), and can have multiple children. This graph is stored alongside your git data and powers all stack operations.

```
        ┌── feat-c
feat-a ─┤
        └── feat-b ── feat-d
```

### Trunk

Your main integration branch (`main`, `master`, etc.). Set during `sr init`. All stacks root here. View or switch to it with `sr trunk`.

### Branch Metadata

Each tracked branch carries:

| Field | Purpose |
|-------|---------|
| **Parent** | Which branch this one builds on |
| **Children** | Branches that depend on this one |
| **Revision** | The git SHA at the branch tip |
| **Description** | A short objective for the branch |
| **Context** | Structured key-value entries (for humans and AI agents) |
| **Frozen** | If true, skipped during automatic restacking |

### Context Entries

Context entries are structured metadata you attach to branches — design decisions, references, ticket links. They're especially useful for AI-assisted workflows where agents need to understand *why* a branch exists.

```bash
sr context set approach "Using JWT for stateless auth" \
  --source file:internal/auth/jwt.go \
  --ticket AUTH-456
```

## Command Reference

### Initialization

| Command | Description |
|---------|-------------|
| `sr init` | Initialize stackr in a git repository |
| `sr config` | Show or modify stackr configuration |
| `sr config set <key> <value>` | Set a config value (e.g. trunk, remote) |

### Creating & Tracking Branches

| Command | Alias | Description |
|---------|-------|-------------|
| `sr create [name]` | `c` | Create a new branch on top of the current one |
| `sr track [branch]` | `tr` | Start tracking an existing branch |
| `sr untrack [branch]` | `utr` | Stop tracking a branch |
| `sr delete [branch]` | `dl` | Delete a branch and reparent its children |

**`sr create` flags:**

```
-m, --message     Commit message (creates an initial commit)
-a, --all         Stage all tracked changes
-u, --untracked   Stage tracked file changes
-p, --patch       Interactive patch selection
-i, --insert      Insert between current branch and its children
    --desc        Set branch description/objective
    --worktree    Create in a worktree instead of checking out
```

### Navigation

| Command | Alias | Description |
|---------|-------|-------------|
| `sr up [N]` | `u` | Move N branches upstack (away from trunk) |
| `sr down [N]` | `d` | Move N branches downstack (toward trunk) |
| `sr top` | `t` | Jump to the top of the current stack |
| `sr bottom` | `b` | Jump to the bottom of the current stack (first branch above trunk) |
| `sr checkout [branch]` | | Switch to a tracked branch |
| `sr trunk` | | Show or switch to trunk |

### Visualization & Information

| Command | Alias | Description |
|---------|-------|-------------|
| `sr log` | `l` | Visualize the stack tree |
| `sr info [branch]` | | Show branch details (parent, children, commits, context) |
| `sr parent [branch]` | | Show parent branch |
| `sr children [branch]` | | Show child branches |

**`sr log` flags:**

```
-a, --all       Show all stacks, not just the current one
-l, --long      Show commits for each branch
-r, --reverse   Reverse order (trunk at bottom)
-s, --stack     Show only the current stack
```

**`sr info` flags:**

```
-d, --diff      Show full diff against parent
-s, --stat      Show diff stat against parent
```

### Branch Metadata

| Command | Alias | Description |
|---------|-------|-------------|
| `sr describe [text]` | `desc` | Set or show the branch description/objective |
| `sr context set <key> <text>` | | Add/update a context entry |
| `sr context rm <key>` | | Remove a context entry |
| `sr context list` | | List all context entries |

**Context flags:**

```
--source type:reference    Source reference (repeatable)
--ticket ID                Related ticket IDs (comma-separated or repeatable)
```

### Committing

| Command | Description |
|---------|-------------|
| `sr commit` | Commit with stackr context tracking |

**`sr commit` flags:**

```
-m, --message     Commit message (required)
-a, --all         Stage all tracked changes
-u, --untracked   Stage tracked file changes
-p, --patch       Interactive patch selection
    --desc        Update branch description
    --context     Commit context JSON blob (repeatable)
```

Context entries are structured JSON attached to individual commits:

```bash
sr commit -a -m "add validation" --context '{"key":"step-3","text":"Implementing zod validation","sources":[{"type":"file","reference":"codev/plans/1-validation.md"}]}'
```

### Stack Operations

| Command | Alias | Description |
|---------|-------|-------------|
| `sr restack` | `r` | Rebase the stack so branches are correctly ordered |
| `sr sync` | | Fetch trunk, restack, and clean merged branches |
| `sr absorb` | `ab` | Distribute staged changes to the right stack commits |
| `sr split` | `sp` | Split the current branch into multiple branches |
| `sr fold` | | Merge the current branch into its parent |
| `sr squash` | | Combine commits within a branch |
| `sr move` | | Move a branch onto a new parent |
| `sr reorder` | | Reorder branches in a stack |
| `sr pop` | | Remove current branch and move to parent |
| `sr freeze` | | Mark a branch as frozen (skip during restack) |
| `sr unfreeze` | | Unfreeze a branch (resume restacking) |

**`sr sync` flags:**

```
--restack       Restack after syncing (default: true)
-f, --force     Force sync
-a, --all       Sync all stacks
```

### Collaboration

| Command | Alias | Description |
|---------|-------|-------------|
| `sr submit` | `s` | Push branches and create/update PRs |
| `sr get [branch]` | | Fetch a branch from remote and track it |
| `sr push-meta` | | Push stackr metadata to the remote |
| `sr pull-meta` | | Pull and merge stackr metadata from the remote |

**`sr submit` flags:**

```
-d, --draft           Mark PRs as draft
-s, --stack           Push all branches in the stack
-u, --update-only     Only update already-pushed branches
-f, --force           Force push
    --dry-run         Show what would be pushed without doing it
    --title           PR title (skips interactive prompts)
    --body            PR body (used with --title)
    --body-file       Read PR body from file (used with --title)
    --ai              Launch Claude to generate and submit PR
    --aiprepare       Output PR context as JSON (for agents)
```

**Three submit modes:**

1. **Programmatic** — an agent gathers context with `sr submit --aiprepare`, then calls `sr submit --title "..." --body "..."` directly
2. **Bare interactive** — `sr submit` with no flags presents a wizard (Push only / Create PR)
3. **Agent interactive** — `sr submit --ai` spawns a Claude session that generates and submits the PR autonomously

### Address Review

| Command | Description |
|---------|-------------|
| `sr address-review` | Walk the stack and address PR review comments interactively |
| `sr address-review --aiprepare` | Output all unresolved comments as JSON (for agents) |
| `sr address-review --ai` | Launch Claude to address all comments autonomously |

**Three address-review modes** (same pattern as submit):

1. **Programmatic** — `sr address-review --aiprepare` outputs JSON, agent makes changes and resolves threads
2. **Bare interactive** — `sr address-review` walks bottom-up, presents each comment, lets you edit/reply/skip
3. **Agent interactive** — `sr address-review --ai` spawns Claude with `/goal` to address everything autonomously

### State Management

| Command | Description |
|---------|-------------|
| `sr undo` | Undo the last stack mutation |
| `sr continue` | Continue after resolving a rebase conflict |
| `sr abort` | Abort an in-progress operation |
| `sr revert` | Revert a previous operation |

### Implementing Issues (`sr implement`)

Turn a GitHub issue or Jira ticket into a working branch in one step. `sr
implement <ref>` fetches the issue host-side, creates a **new tracked branch**
named `<ref>-<title-slug>`, records the issue linkage, and drives implementation.

| Command | Description |
|---------|-------------|
| `sr implement 123` | GitHub issue → new branch; implement here or spawn Claude |
| `sr implement PROJ-456 --worktree` | Jira ticket in a worktree |
| `sr implement 123 --sandbox` | implement in a disposable sandbox (implies `--worktree`) |
| `sr implement 123 --ai` | scaffold the branch and emit `{branch, worktreePath, issueRef, prompt}` as JSON |

**Source detection** is automatic: a number (`123`, `#123`) or GitHub issue URL
is a GitHub issue; a `KEY-N` (`PROJ-456`) or browse URL is a Jira ticket. Force
it with `--source github|jira`.

**How it implements** depends on context: inside a Claude session (or with
`--ai`) it scaffolds and hands the brief back as JSON — no nested agent; a bare
terminal spawns an interactive Claude on the brief; `--sandbox` runs it in a
container (attaching in a terminal, detached + JSON when driven by a skill).

Other flags: `--branch <name>` overrides the derived name, `--parent <branch>`
changes what it stacks on (default: current branch), `--comments` folds the
issue discussion into the brief. GitHub needs `gh`; Jira needs the `jira` CLI.

### Sandboxed AI Sessions (`sr sandbox`)

Run Claude with `--dangerously-skip-permissions` **without** exposing your host:
a disposable Docker container works on the branch's worktree, while your
`~/.claude` config, skills, and session history are shared so the work is
indistinguishable from a normal local session — and resumable.

| Command | Alias | Description |
|---------|-------|-------------|
| `sr sandbox [branch] [-- "<prompt>"]` | `sb` | Launch (or reuse) a sandbox for a branch and attach |
| `sr sandbox attach [branch]` | | Reattach; no arg opens a searchable picker |
| `sr sandbox ls` | | List this repo's sandboxes with status |
| `sr sandbox watch [--notify]` | | Live dashboard, or headless desktop notifications |
| `sr sandbox stop <branch>` | | Stop the container (relaunch resumes) |
| `sr sandbox rm <branch> [--delete]` | | Remove the container (`--delete` also removes the worktree) |
| `sr sandbox config [--ai]` | | Edit sandbox config in a TUI, or let Claude help |

Highlights:

- **Session continuity** — the sandbox's Claude session appears under the same
  `~/.claude/projects` entry as the repo, so host `claude --resume` sees it.
- **Egress firewall by default** — only an allowlist (Anthropic, GitHub,
  registries) is reachable; the agent requests new domains, which you add to the
  config and relaunch. `--network full` opts out.
- **No credentials in the sandbox** — it records a proposed PR as a `pr` branch
  context entry; run `sr submit` on the host to open the PR from it.
- **Attention** — `sr sandbox ls`/`watch` show when a session is waiting on you.

Requires Docker. The base image is built once and reused; per-container
differences are just bind mounts and the launch command.

### Utilities

| Command | Description |
|---------|-------------|
| `sr rename` | Rename a branch |
| `sr modify` | Amend the current branch and restack descendants |
| `sr worktree` | Manage git worktrees |
| `sr claude install` | Install the stackr skill (with sandbox + implement lanes) for Claude Code |
| `sr shell-hook` | Print shell integration script |
| `sr completion` | Generate shell completion scripts |

## Methodology

Stackr is built around a development rhythm: plan the decomposition, build bottom-up, track decisions as you go, and submit in clean increments. This section describes how to use stackr effectively — whether you're working solo, with a team, or with AI agents.

### Think in Layers, Not Features

Before writing code, decompose your feature into layers that build on each other. Each layer becomes a branch. The bottom of the stack should be the most foundational change — the thing everything else depends on.

```
# Bad: one giant branch
feat-auth (47 files, 2000 lines)

# Good: layered stack
feat-auth-models       ← data structures, types
feat-auth-middleware   ← request validation, token parsing
feat-auth-api          ← endpoints that use the middleware
feat-auth-tests        ← integration tests
```

Each branch should be independently reviewable. A reviewer looking at `feat-auth-middleware` should only need to understand `feat-auth-models` below it — not the entire feature.

### Build Bottom-Up, Review Bottom-Up

Start at the bottom of your stack and work upward. When you create a new branch, give it a clear objective:

```bash
sr create feat-auth-models --desc "User and session types for JWT auth"
```

Commit with `sr commit` instead of `git commit` to keep the graph in sync and attach reasoning:

```bash
sr commit -a -m "add session model" --context '{"key":"design","text":"Sessions are stateless JWTs, not DB-backed"}'
```

When you're ready for review, submit the whole stack:

```bash
sr submit --stack
```

Reviewers should merge from the bottom up. As each PR merges, `sr sync` cleans it from the stack and rebases everything above.

### Track Decisions, Not Just Code

Code says *what* changed. Context says *why*. Stackr has two levels:

**Branch context** — high-level decisions that apply to the whole branch:

```bash
sr context set approach "Stateless JWTs over DB sessions — latency constraint"
sr context set tradeoff "No revocation without a blocklist; acceptable for v1"
```

**Commit context** — per-step reasoning attached to individual commits:

```bash
sr commit -a -m "add token rotation" --context '{"key":"step","text":"Refresh tokens rotate on use per OWASP guidelines","sources":[{"type":"url","reference":"https://cheatsheetseries.owasp.org/..."}]}'
```

Both feed into PR description generation (`sr submit --ai`) and are visible via `sr info`. Both are lost on squash — persist anything that should outlive the branch to a file before squashing.

### Handle Review Comments Across the Stack

When reviewers leave comments on your stack, `sr address-review` walks from the bottom up, presents each unresolved thread, and lets you edit code, reply, and resolve — then commits, restacks, and moves to the next branch.

```bash
# Interactive walkthrough
sr address-review

# Let Claude handle everything
sr address-review --ai
```

### Keep the Stack Healthy

| Situation | Command |
|-----------|---------|
| Trunk moved (someone merged) | `sr sync` |
| Reviewer requested changes mid-stack | `sr checkout <branch>`, fix, `sr restack` |
| Branch got too big | `sr split` |
| Branch is trivial, fold into parent | `sr fold` |
| Need to reorder the stack | `sr reorder` |
| Made a mistake | `sr undo` |

### Working with AI Agents

Stackr is designed for both human and AI-driven development. Every workflow command follows a three-mode pattern:

1. **Interactive** — the bare command runs a terminal walkthrough for humans
2. **Programmatic** — `--aiprepare` outputs JSON context, `--title`/`--body`/`--context` accept structured input. An agent already in a session composes these.
3. **Autonomous** — `--ai` spawns a Claude session that owns the workflow end-to-end

**The agent workflow:**
1. Agent reads the branch state: `sr info`, `sr log`
2. Agent sets context as it works: `sr context set`, `sr commit --context`
3. Agent submits when ready: `sr submit --aiprepare` → craft PR → `sr submit --title "..." --body "..."`
4. Agent addresses review comments: `sr address-review --aiprepare` → make fixes → reply and resolve

Context entries are the key integration point. They let the agent explain its reasoning in a structured way that persists across sessions and feeds into PR generation.

### Worktrees for Parallel Work

For long-running branches or when you want to keep your main working tree clean:

```bash
sr create feat-experiment --desc "Try approach B" --worktree
# Creates .worktrees/feat-experiment/ — main working tree stays on current branch
```

Add a `.stackr/hooks/post-worktree` script to automate setup (copying `.env`, running `direnv allow`, etc.):

```bash
#!/bin/bash
# .stackr/hooks/post-worktree
cp .env "$1/.env"
cd "$1" && direnv allow
```

## Workflows

### Daily Development

```bash
# Start your day — sync with trunk
sr sync

# Start a new feature stack
sr create feat-data-model -m "Add data model"
# ... make changes, commit ...

# Stack the next piece on top
sr create feat-api -m "Add API layer"
# ... make changes, commit ...

# Stack the UI on top of that
sr create feat-ui -m "Add UI components"
```

### Updating Mid-Stack

```bash
# Reviewer requested changes on feat-data-model
sr checkout feat-data-model
# ... make fixes, commit ...

# Restack everything above
sr restack
```

### Submitting for Review

```bash
# Push the entire stack and open PRs
sr submit --stack

# Or push just the current branch
sr submit

# Preview without pushing
sr submit --dry-run
```

### Handling Trunk Updates

```bash
# Trunk moved (someone merged a PR)
sr sync
# Fetches trunk, rebases your stack, cleans up merged branches
```

### Resolving Conflicts

```bash
# If a restack hits a conflict, stackr pauses
# Fix the conflict in your editor, then:
git add <resolved-files>
sr continue

# Or bail out:
sr abort
```

### Reorganizing a Stack

```bash
# Move a branch to a different parent
sr move --onto feat-auth

# Insert a branch between two existing ones
sr create feat-middleware -i

# Merge a branch into its parent (fold up)
sr fold

# Split a branch that got too big
sr split
```

## Claude Code Integration

Stackr ships a skill that teaches Claude how to use `sr` commands. Install it with:

```bash
sr claude install
```

This creates a single unified skill at `.claude/skills/stackr/` — a `SKILL.md` covering the core workflow (branch creation, navigation, context tracking, conflict recovery, PR submission) plus progressive-disclosure lane files (`sandbox.md`, `implement.md`) that Claude reads on demand. Claude then uses `sr` via Bash instead of raw git. Upgrading from an older version folds the separate `sr-sandbox` / `sr-implement` skills into this one automatically.

For programmatic workflows (agents already in a session), use the two-step submit:

```bash
# 1. Gather PR context as JSON
sr submit --aiprepare

# 2. Submit with title and body
sr submit --title "Add auth middleware" --body "## Summary\n..."
```

For AI-driven submit, `sr submit --ai` spawns a Claude session that generates and submits the PR autonomously.

For addressing review comments across the stack:

```bash
# Gather all unresolved comments as JSON
sr address-review --aiprepare

# Walk through comments interactively
sr address-review

# Let Claude handle everything
sr address-review --ai
```

## Storage

Stackr stores metadata in two tiers:

### Shared metadata — `refs/stackr/data`

The branch graph, config, and PR info are stored as git objects behind a custom ref. This means:

- Metadata can travel with `git push`/`git fetch` via refspecs
- Git's garbage collection manages cleanup
- Works correctly across worktrees (shared `.git` directory)

| Blob | Contents |
|------|----------|
| `config.json` | Trunk branch name, remote name |
| `branches.json` | Complete branch graph with metadata |
| `pr_info.json` | PR numbers, URLs, and state per branch |

### Local ephemeral data — `.git/.stackr/`

Per-machine state that shouldn't travel with push/pull:

| Path | Contents |
|------|----------|
| `undo/snapshots/` | Graph snapshots for rollback |
| `undo/events.json` | Undo event log |
| `rebase-state.json` | In-progress rebase state |

## Architecture

```
cmd/             CLI commands (Cobra)
internal/
  context/       Session initialization, repo discovery
  engine/        Core algorithms (create, restack, sync, submit, ...)
  git/           Git command wrapper
  graph/         Branch graph model and tree rendering
  store/         Storage (git-ref shared + filesystem local)
  ui/            Terminal UI (charmbracelet/bubbletea)
  errors/        Sentinel error types
pkg/
  version/       Build metadata (set via ldflags)
```

### Key Design Decisions

- **Engine layer** separates business logic from CLI concerns — each operation (create, sync, restack) lives in its own file under `internal/engine/`
- **Graph is the single source of truth** — all operations read the graph, mutate it, and write it back. Undo works by snapshotting the graph before mutations.
- **Git wrapper is explicit** — `internal/git/` exposes methods like `RebaseBranch`, `CommitsBetween`, `DiffStat` rather than raw command execution, making the engine testable.
- **Two-tier storage** — shared metadata (graph, config, PRs) lives in git objects behind `refs/stackr/data`; local ephemeral data (undo, rebase state) stays on the filesystem under `.git/.stackr/`.

## Global Flags

```
    --cwd string       Run as if started in this directory
    --debug            Print git commands as they run
    --interactive      Enable interactive prompts (default: true)
-q, --quiet            Suppress non-essential output
    --no-verify        Skip git hooks
-v, --version          Show version
```

## Development Setup

```bash
make setup      # Create .envrc from template
make build      # Build to build/sr
make link       # Symlink to $STACKR_LINK_DIR (from .envrc)
make install    # Install to $GOPATH/bin/sr
make test       # Run all tests
make clean      # Remove build artifacts
```

## License

See [LICENSE](LICENSE) for details.
