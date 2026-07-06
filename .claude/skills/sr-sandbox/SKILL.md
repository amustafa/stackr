---
name: sr-sandbox
description: >
  Launch or manage a sandboxed Claude session — a disposable Docker container
  running Claude with skip-permissions on an isolated branch worktree. Use when
  the user wants to run an agent freely without host risk, or says "sandbox",
  "sr sandbox", "fork a session", or "/sr-sandbox".
---

# sr sandbox

Thin conversational wrapper over the `sr sandbox` command tree. The heavy lifting
(worktree creation, image build, container launch, attach) lives in Go. This
skill's job is to help the user shape the launch — pick the branch, craft the
initial prompt, decide any extra context to mount — then shell out.

See `codev/specs/5-sr-sandbox.md` for the full design, and ADR-0008 / ADR-0009
for the mount and lifecycle decisions.

## What a sandbox is

A disposable Docker container that runs `claude --dangerously-skip-permissions`
inside a **zellij** session on one branch's worktree. The host filesystem and
processes are isolated by the container; the developer's `~/.claude` (config,
skills, credentials, and session history) is bind-mounted so the sandbox's work
appears under the same project as the host — `claude --resume` sees it.

**Durable state is host-side** (the worktree + `~/.claude/projects` logs), so a
container can be destroyed and recreated without losing Claude progress.

## Launching

Before running, help the user with:

1. **Branch** — which branch to work on (one sandbox per branch). Create it first
   with `sr create` if it doesn't exist.
2. **Initial prompt** — the task for the agent. Optional; without it, plain
   interactive Claude starts.
3. **Extra context** — anything missing from the worktree the agent will need
   (a sibling repo, a docs dir, an env var). Adding context means recreating the
   container (you can't add mounts to a running one) — that's expected and cheap.

Then:

```bash
sr sandbox <branch> -- "<initial prompt>"
```

This creates/reuses the worktree, ensures the base image (and any per-project
`.stackr/sandbox/Dockerfile` layer), starts the container detached, launches
zellij → Claude, prints the identifier (the branch name), and attaches you.

## Reconnecting & listing

```bash
sr sandbox attach            # searchable TUI of active sandboxes (branch + prompt)
sr sandbox attach <branch>   # attach directly
sr sandbox ls [--all]        # list sandboxes (this repo, or all repos)
```

Detaching (zellij detach or closing the terminal) leaves the sandbox running.

## Knowing when a sandbox needs you

Skip-permissions stops permission prompts, not genuine questions. Claude Code
hooks — loaded via a sandbox-only `--settings` file (ADR-0011), never installed
into your `~/.claude` — publish each sandbox's interaction state to
`.git/.stackr/sandboxes/<branch>.status` (`working` / `awaiting-input` /
`awaiting-choice` / `exited`) with the pending question text. It's surfaced three ways:

```bash
# ambient: the shell prompt shows a count of sandboxes awaiting input
sr sandbox ls                 # status column + pending question per sandbox
sr sandbox watch              # live two-pane dashboard (awaiting on top, all below)
sr sandbox watch --notify     # headless: desktop notifications on transition to awaiting
```

In the watch dashboard: up/down or click to navigate, a hotkey jumps to the first
awaiting session, clicking a session attaches to it. Scope defaults to the current
project (config) or `--all`.

## The sandbox is firewalled

By default the sandbox runs behind an egress **allowlist** (Anthropic + GitHub +
common registries — ADR-0012). If the agent needs a blocked domain, it should
**request** it rather than treat the failure as transient. To grant it, add the
domain to the allowlist and relaunch:

```bash
sr sandbox config          # add the domain to the firewall allowlist
sr sandbox <branch>        # relaunch — resumes the session with the new allowlist
# or, for accepted-risk cases:
sr sandbox <branch> --network full
```

Adding a domain is just the "add context, then restart and resume" flow.

## Extra binaries in the sandbox

Two config-driven ways to put executables on the sandbox `PATH`:
- **Sandbox bin dir** (portable): drop binaries in `.stackr/sandbox/bin/` — prepended to `PATH`.
- **PATH mounts** (machine-specific): configure host directories to bind-mount and add to `PATH`.

Host binaries must match the container's OS/arch/libc.

## Adding missing context

If the agent is blocked on something not in the sandbox:

1. `sr sandbox stop <branch>` (or `rm`).
2. Add the context (mount a dir via config, drop a file in the worktree, etc.).
3. `sr sandbox <branch>` — reconstructs the container with the new mounts and
   resumes the session (`--continue`).

## Teardown

```bash
sr sandbox stop <branch>            # keep the container; live session resumable
sr sandbox rm <branch>              # destroy container, keep worktree (cold-resume later)
sr sandbox rm <branch> --delete     # also remove the worktree and branch
```

Destroying a container never loses Claude progress — the session lives in
`~/.claude/projects` on the host.

## Pushing & PRs (host-side)

The sandbox has **no GitHub credentials** (ADR-0010) — it cannot push or open PRs.
The agent commits to the branch (already in the shared `.git`) and records a
**PR Suggestion** as a reserved Branch Context entry:

```bash
sr context set pr "<proposed PR title/body>"   # inside the sandbox, before teardown
```

Then, on the host, `sr submit` reads the reserved `pr` entry and uses it as the
PR title/body directly (offer edit → push → create/update). Branch Context is
lost on squash, so set `pr` after any squash. Prefer this over mounting
credentials into the sandbox — direct-push is never the default.

## Configuration

```bash
sr sandbox config           # editable TUI of current settings
sr sandbox config --ai      # Claude helps manage the config
```

Config is split three ways: **portable** (shared, in the git-ref config — network
policy, base image, firewall, cache toggle), **machine-specific** (git-ignored
local file — host cache paths, extra mounts), and **auto-derived** (never stored —
the CLI computes worktree paths, HOME, UID:GID, etc.).
