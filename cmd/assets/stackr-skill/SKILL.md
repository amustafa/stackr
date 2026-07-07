---
name: stackr
description: >
  Stacked-branch workflow for git via the `sr` CLI. Use whenever working with
  branches, commits, or PRs in a repo that uses stackr — creating and navigating
  stacked branches, committing with tracked design context, submitting and
  addressing review on PRs, running work in a disposable sandbox, or implementing
  a GitHub issue or Jira ticket. Track design decisions with `sr context` as you go.
---

# Stackr (sr)

This repo uses stackr. Use `sr` for branch operations, not raw git.

## The one rule

**Use `sr commit`, not `git commit`; use `sr` for branch/stack moves, not raw
git.** Stackr keeps a graph of branches and their parents; raw git mutations
desync it. When in doubt, there is almost always an `sr` verb for what you want —
check the command map below or run `sr --help`.

## How to work in a stack

Decompose a change into layered branches that build on each other, each
independently reviewable. Build bottom-up: foundational changes first, dependent
changes stacked on top. Give every branch an objective, and record the decisions
behind it as you go — that context is what turns a pile of commits into a
reviewable stack (and it writes your PR for you).

## The hot path

    sr create <name> --desc "objective"     # new branch stacked on current, with an objective
    sr commit -a -m "message"                # stage + commit, keeping the graph in sync
    sr modify -a                             # amend current branch and restack descendants
    sr describe "objective"                  # set/replace the current branch's objective
    sr sync                                  # fetch trunk, restack, drop merged branches
    sr submit                                # push and open/update PRs

`sr create <name> --worktree` makes the branch in a worktree instead of checking
it out here. Run `sr <cmd> --help` for the full flag set of any command.

## Context tracking — do this as you work

Stackr records structured context that feeds `sr info` and PR generation. Two
levels, both worth using proactively:

**Branch context** — decisions that hold for the whole branch:

    sr context set approach "Stateless JWTs — no DB sessions"
    sr context set design "Split handler into a middleware chain" --source file:internal/api/handler.go
    sr context set risk "No revocation without a blocklist; acceptable for v1" --ticket PROJ-123

**Commit context** — per-step reasoning, attached at commit time as a JSON blob:

    sr commit -a -m "add rotation" --context '{"key":"step","text":"Refresh tokens rotate per OWASP","sources":[{"type":"url","reference":"https://..."}]}'

What earns a context entry: the *approach* and *why it beat the alternative*, a
*trade-off* you accepted, a *plan/issue reference* (`--source file:...`), a
*follow-up* to revisit. Prune stale ones with `sr context rm <key>`.

> Context is **lost on squash**. Persist anything that must survive to a file
> (or the PR body) before squashing a branch.

## Recovering from conflicts

`restack`, `sync`, and `get` replay branches onto their parents and can stop on a
merge conflict. The operation is *paused*, not failed:

1. Resolve the conflicts in the working tree (edit, then `git add`).
2. `sr continue` — resumes the paused operation from where it stopped.

To bail out instead, `sr abort` returns to the pre-operation state. **Never**
finish a stackr rebase with raw `git rebase --continue` — always `sr continue`,
or the graph desyncs.

## Shipping — the three-mode pattern

`submit` and `address-review` each work three ways. Pick by situation:

- **Programmatic** (you're already in a session — the default for an agent):
  `--aiprepare` emits structured JSON to reason over; direct flags act without
  prompts.

      sr submit --aiprepare                    # PR context as JSON
      sr submit --title "..." --body-file /tmp/pr.md
      sr address-review --aiprepare            # all unresolved comments as JSON

- **Interactive**: bare `sr submit` / `sr address-review` runs a wizard.
- **AI-driven**: `--ai` hands the whole task to a fresh Claude session.

`address-review` walks the stack bottom-to-top; address comments on a branch,
commit, and it restacks and moves up.

## Command map

Run `sr <cmd> --help` for flags. Grouped by what you're doing:

- **Start**: `create` · `implement <issue>` · `track` · `checkout`
- **Make progress**: `commit` · `describe` · `context` · `absorb` (spread staged
  changes into the right stack commits) · `modify`
- **Keep the stack healthy**: `restack` · `sync` · `get <branch|PR#>` ·
  `continue` · `abort`
- **Navigate**: `up` · `down` · `top` · `bottom` · `trunk` · `checkout`
- **Inspect**: `log` (`-a` all stacks, `-l` commits) · `info` (`-s` stat, `-d`
  diff) · `children` · `parent`
- **Reshape**: `split` · `fold` · `squash` · `move -o <parent>` · `reorder` ·
  `delete` · `pop` · `revert` · `rename` · `freeze`/`unfreeze`
- **Ship**: `submit` · `address-review`
- **Meta**: `init` · `config` · `push-meta`/`pull-meta` · `untrack` · `worktree`

Global flags worth knowing: `--quiet` (suppress non-essential output, cleaner for
scripting), `--cwd <dir>` (run as if from another directory), `--no-verify` (skip
git hooks).

## Specialized lanes

- **Running work in a disposable sandbox** (Docker container, skip-permissions) —
  read `sandbox.md` in this skill directory.
- **Implementing a GitHub issue or Jira ticket** — read `implement.md` in this
  skill directory.
