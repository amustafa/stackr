# Spec 5: sr sandbox — sandboxed Claude sessions on isolated worktrees

## Problem Statement

Running Claude Code with `--dangerously-skip-permissions` removes the constant permission interruptions but hands an agent unrestricted access to the host: it can run any command, touch any file, and mutate the working tree in place. Today the only way to get uninterrupted agent work is to accept that host-level risk.

We want the productivity of skip-permissions **without** exposing the host: run Claude inside a disposable Docker container that operates on an isolated git worktree, while preserving the developer's normal Claude experience (global + project config, skills, credentials, and — critically — session history) so that work started in the sandbox is indistinguishable from a normal local session and can be resumed at will.

### Current State

- `sr` manages worktrees under `<repo>/.worktrees/<name>` (`internal/engine/worktree.go`), git-excluded, with a `post-worktree` hook.
- `sr claude install` (`cmd/claude.go`) writes a `.claude/skills/stackr/SKILL.md` — the existing pattern for shipping a skill.
- `--ai` commands (`sr submit --ai`, `sr address-review --ai`) shell out to `claude` with a scoped tool allowlist and an appended system prompt (`internal/engine/submit.go:96`, ADR-0004).
- There is no sandboxing, no container integration, and no way to run skip-permissions safely.

### Desired State

- `sr sandbox [branch]` creates-or-reuses a worktree for a branch, launches a Docker container that runs Claude with `--dangerously-skip-permissions` inside a **zellij** session, and attaches the developer to it.
- The container is **disposable**; all durable state (the worktree, `~/.claude/projects` session logs) lives on the host, so a container can be destroyed and recreated without losing Claude progress.
- The sandbox's Claude sessions appear under the **same** `~/.claude/projects` entry as the real repo, so host `claude --resume`/history see them seamlessly.
- `sr sandbox attach [branch]` reconnects; the bare form shows a searchable TUI of active sandboxes (branch + initial prompt).
- `sr sandbox config [--ai]` manages sandbox settings via TUI or an AI-assisted session.

## Stakeholders

- **Primary**: The developer who wants uninterrupted agent work without risking their host machine.
- **Secondary**: Agents launched inside the sandbox (they inherit the developer's full skill/config environment and operate on the worktree).

## Constraints

- Per ADR-0003: no MCP server; all agent interaction is via the CLI.
- Per ADR-0004: `sr sandbox config --ai` follows the three-mode pattern's agent-interactive mode (spawns a Claude session with a scoped tool allowlist + system prompt).
- Per ADR-0006: worktree creation continues to fire the `post-worktree` hook — the sandbox launches *after* the hook has set up the worktree.
- Per **ADR-0008**: the worktree and main `.git` are mounted at their real host paths (path-identical), and `.git` is shared read-write. Session continuity depends on this.
- Per **ADR-0009**: the container is disposable; durable state is host-side. In-container installs are ephemeral by design.
- Per **ADR-0010**: the sandbox carries **no GitHub credentials** and performs no authenticated remote operations. The sandbox records a **PR Suggestion** as a reserved `pr` **Branch Context** entry; host-side `sr submit` reads it to open the PR. Guiding principle: prefer host-side revocable operations over durable secrets in a skip-permissions box.
- Per **ADR-0011**: attention hooks are provided via `claude --settings <file>` (additive over the mounted `~/.claude`), never installed into the developer's global `~/.claude/settings.json`. The sandbox's main session must not use `--bare` (which skips hooks).
- Per **ADR-0012**: the sandbox defaults to an **egress allowlist firewall**; blocked domains are requested by the agent and added to the allowlist config + relaunched. `--network full` is an explicit opt-out.
- Requires Docker on the host. The feature is a no-op / clear error where Docker is unavailable.
- The base image and all mounts are **bind mounts**; no per-sandbox image builds and no named-volume copies (efficiency requirement — see Solution).
- One sandbox per branch (git allows a branch in only one worktree at a time).

## Solution

### Architecture overview

```
host                                             container (disposable, run -d)
────                                             ──────────────────────────────
~/.claude/            ──bind rw (same path)──▶   ~/.claude/           (config, skills, creds, projects/)
<repo>/.git/          ──bind rw (same path)──▶   <repo>/.git/         (shared object store + refs)
<repo>/.worktrees/B/  ──bind rw (same path)──▶   <repo>/.worktrees/B/ (cwd; isolated working tree)
~/go/pkg/mod, caches  ──bind rw (auto)───────▶   same paths           (warm deps)

                                                 zellij session "B"
                                                   └─ claude --dangerously-skip-permissions "<prompt>"

manifest: .git/.stackr/sandboxes/<B>.json  (mounts, initial command, session id)
```

Because the container's `cwd` equals the host worktree path and `~/.claude` is mounted at the same path, Claude computes the **same project hash** as the host — session logs land in the shared `~/.claude/projects/<hash>` and host `--resume`/history pick them up for free (ADR-0008).

### Efficiency model (single base image + bind mounts)

- **One base image** `stackr-sandbox:base`, built once and cached by Docker layers. Rebuilt only when its Dockerfile changes.
- **Optional per-project layer**: a repo may ship `.stackr/sandbox/Dockerfile` (`FROM stackr-sandbox:base`) that adds its toolchain (e.g. Go for this repo). Built and cached **once per project**, shared by all its sandboxes.
- **Per-container differences are only**: the bind mounts and the launch command. Nothing is copied; "volume creation" is effectively free.

### Base image contents

Shared base: `git`, `gh`, `curl`/ca-certificates, the `claude` CLI, `zellij`, and the `sr` binary. Project-specific toolchains come from the optional per-project `Dockerfile`.

### Container identity & process model

- **Identity** = branch name. `sr sandbox feat-x` → worktree/container for `feat-x`.
- **Process user** = host `UID:GID` with `HOME` set to the real host home and `~/.claude` mounted there, so all writes to the worktree / `.git` / `~/.claude` are owned by the developer (no root-owned litter). Git identity inside the container is supplied via `GIT_AUTHOR_*`/`GIT_COMMITTER_*` (or a synthesized passwd entry) since the UID may not exist in the image.
- **Network** = **egress allowlist by default** (ADR-0012), enforced via an `iptables`/`ipset` init step (`--cap-add=NET_ADMIN`). The allowlist ships with Anthropic API + GitHub + common package registries and is a portable config field. The agent is told it is firewalled and must **request** any blocked domain; the developer adds it to the allowlist and relaunches (the ADR-0009 add-context-and-restart flow). A `--network full` opt-out remains for accepted-risk cases.

### Lifecycle

1. `sr sandbox [branch] [-- "<prompt>"]`
   - Create-or-reuse the worktree (fires `post-worktree` hook).
   - Ensure base image (and per-project layer) exist; build if missing.
   - `docker run -d` the container with the bind mounts above, running `zellij` with a session named after the branch, whose command is `claude --dangerously-skip-permissions "<prompt>"` (plain interactive Claude if no prompt).
   - Write the manifest to `.git/.stackr/sandboxes/<branch>.json`.
   - Print the identifier (branch name) and **attach** (same path as `attach`).
2. Detaching (zellij detach / closing the terminal) leaves the container running and Claude alive.
3. `sr sandbox attach [branch]` — `docker exec -it <container> zellij attach <branch>`. Bare `sr sandbox attach` opens the searchable selector.
4. `sr sandbox stop <branch>` — `docker stop` (keep the container; `docker start` + attach resumes the **live** zellij session).
5. `sr sandbox rm <branch> [--delete]` — remove the container. Worktree/branch are kept by default (a later `sr sandbox` cold-resumes via `claude --continue`, since session logs are host-side). `--delete` also removes the worktree and branch (mirrors `sr worktree remove --delete`).

**Invariant:** destroying a container never loses Claude progress. `stop` preserves the in-memory zellij session; `rm` drops to cold `--continue` resume.

### Attach & discovery TUI

- `sr sandbox ls` and bare `sr sandbox attach` enumerate active sandboxes by listing containers labeled `stackr.sandbox` (Docker is the source of truth for "running"), joined with each manifest for the **initial prompt** display.
- The selector is a **searchable** variant of `internal/ui/selector.go`: a `bubbles/textinput` filter on top of the current list model, matching the "searchable like the other tuis" requirement. Each row shows the branch and a truncated initial prompt.
- Scope: current repo by default; `--all` lists sandboxes across repos.

### Human interaction & attention

Skip-permissions removes *permission* prompts but not the legitimate need for human input — a clarifying question or an `AskUserQuestion` choice. A detached sandbox must therefore publish an "it's your turn" signal, with enough detail to know *which* session and *why*.

#### Detection: Claude Code hooks → status file

A sandbox-scoped hook set — provided via a **per-invocation `--settings` file** (ADR-0011), so `~/.claude` is never mutated and the hooks exist only for sandbox sessions — publishes state to `.git/.stackr/sandboxes/<branch>.status` (Local Data). `SR_SANDBOX=<branch>` tells the hook which sandbox it is:

| Hook | Transition | Status |
|---|---|---|
| `PreToolUse` on `AskUserQuestion` | agent presents options | `awaiting-choice` + options text |
| `Stop` | turn ended (question or completion) | `awaiting-input` + tail of last message |
| `Notification` | idle nudge | `awaiting-input` |
| `UserPromptSubmit` | you replied | `working` |
| `SessionEnd` | session exited | `exited` |

The status file carries `{ state, reason, updated_at }` where `reason` is the pending question / options / summary — the same text surfaced in `sr sandbox ls`, the attach selector, and the watch app.

#### Surfacing (layered)

1. **Ambient prompt indicator** — the shell hook / Prompt Cache (already used for `SR_BRANCH`) is extended to expose a count of sandboxes awaiting input (e.g. `SR_SANDBOX_AWAITING`), so the developer's prompt ambiently shows "2 awaiting" with no running process.
2. **On-demand** — `sr sandbox ls` and the attach TUI show a status column + pending-question text.
3. **`sr sandbox watch`** — see below.

#### `sr sandbox watch`

Two modes:

- **`--notify`** — headless background process. Watches status files and fires **desktop notifications** on transition into an awaiting state. No UI.
- **default (no flag)** — opens a live full-screen TUI **dashboard** application:
  - **Left panel, two sections**: a **top** section listing sessions **currently awaiting input**, and a **bottom** section listing **all** included sessions. Live-updates as status files change.
  - **Right panel**: detail/preview of the selected session — branch, state, the pending question/options, and the initial prompt. *(Inferred; the user specified the left panel — the right is the natural companion. Adjust if not wanted.)*
  - **Navigation**: up/down moves through the lists; **mouse click** on any item in either list jumps directly to it (attach — suspends the dashboard, `docker exec` + `zellij attach`, returns to the dashboard on detach); a **hotkey** jumps to the first item in the awaiting-input section.
  - **Scope**: controlled by a config option (default: current project); `--all` watches sessions across all projects.

### Configuration (three-way split)

Config holds **only what needs a human decision**. Everything the CLI can compute, it computes.

| Tier | Storage | Examples |
|---|---|---|
| **Portable** | git-ref `config.json` (shared/mergeable, via existing `store.Config`) | network policy, base-image name, per-project Dockerfile path, firewall allowlist, cache on/off, default prompt template, **sandbox bin dir**, **watch default scope** (project \| all) |
| **Machine-specific** | git-ignored `.git/.stackr/sandbox.local.json` | absolute host cache paths, extra host dirs to mount, non-standard docker socket, **extra PATH mounts** |
| **Auto-derived** (never stored) | computed at runtime | worktree paths, repo root, `.git` path, HOME, UID:GID, project/session hash |

- `sr sandbox config` → editable TUI (reuse `internal/ui/form.go`). This is stackr's first real config TUI (top-level `sr config` only prints).
- `sr sandbox config --ai` → mirrors `submitAI` (`internal/engine/submit.go:96`): feeds current config as JSON context on stdin, scopes tools to `Read,Edit,Bash(sr sandbox config *)`, appends a purpose-built system prompt (author: user). Follows ADR-0004's agent-interactive mode.

### Caching

On by default. The CLI auto-detects standard cache dirs (`~/go/pkg/mod`, `~/.cache/go-build`, `~/.npm`, …) and bind-mounts them at the same paths so disposable runs stay warm. Overridable in machine-specific config.

### Binaries & PATH

Two ways to make extra executables available inside the sandbox, both assembled into the container's `PATH` at launch:

- **Sandbox bin dir** (portable): a repo-local directory, default `.stackr/sandbox/bin/`, bind-mounted into the container and **prepended** to `PATH`. Drop a binary in it (or commit one for the whole team) and it's on `PATH` in every sandbox for that repo.
- **Extra PATH mounts** (machine-specific): a configured list of **host** directories (e.g. `/home/amustafa/tools/bin`). Each is bind-mounted at its real host path and added to `PATH`, so an existing host tool location is directly usable in the sandbox.

`sr sandbox config` exposes both (add/remove entries). The CLI composes the final `PATH` as: sandbox bin dir → extra PATH mounts → image defaults.

**Caveat (documented, not enforced):** host binaries must be compatible with the container's OS/arch and libc. Static binaries and matching-libc dynamic binaries work; a binary linked against a different libc than the base image may fail. This is the user's responsibility, surfaced in the config help text.

### Remote operations & PR suggestions (credential-free)

The sandbox has no GitHub credentials (ADR-0010), so it never pushes or opens PRs. It reuses the existing **Branch Context** mechanism instead of any new file or command:

1. The agent commits to the branch — those commits are already in the shared `.git` (ADR-0008).
2. When it has a PR-worthy result, the agent records a **PR Suggestion** via `sr context set pr "<proposed title/body>"` — a reserved Branch Context entry that lives in the shared `refs/stackr/data`.
3. Host-side, `sr submit` reads the reserved `pr` entry (if present) and uses it as the PR title/body directly — no AI regeneration — offering to edit before it pushes and creates/updates the PR. If absent, submit falls back to its existing generation from Description + Context + commits.

`PrepareAI`/`submit` already read branch **Description** + **Context** to build a PR (`internal/engine/prepare.go`); this only adds special-casing of the reserved `pr` key. Direct-push remains available only if the user explicitly opts into mounting credentials; it is never the default.

## New Components

| Component | Purpose |
|---|---|
| `cmd/sandbox.go` | `sr sandbox` command tree (`run` (default), `attach`, `ls`, `stop`, `rm`, `config`, `watch [--notify] [--all]`); alias `sb`. |
| `internal/engine/sandbox.go` | Orchestration: worktree ensure, image ensure/build, `docker run/exec/stop/rm`, manifest read/write. |
| `internal/engine/sandbox_docker.go` | Thin wrappers over the `docker` CLI (run, exec, stop, rm, ps-by-label, image build/exists). |
| `internal/store/sandbox_config.go` | Portable `Sandbox` section on `store.Config` + machine-specific local-file loader. |
| `internal/ui/filter_selector.go` | Searchable selector (textinput filter over the existing selector model). |
| `internal/sandbox/hooks.go` | Builds the sandbox-only `--settings` JSON with the attention hooks (ADR-0011); the embedded hook script writes the status file. |
| `internal/sandbox/status.go` | Status type (`state`, `reason`, `updated_at`) + read/write/watch of `.git/.stackr/sandboxes/<branch>.status`. |
| `internal/ui/watch.go` | `sr sandbox watch` two-pane live dashboard (awaiting / all lists, detail pane, click-to-attach, jump-to-first-awaiting hotkey). |
| `internal/engine/sandbox_watch.go` | Watch orchestration + `--notify` headless notifier (desktop notifications on transition). |
| `internal/sandbox/manifest.go` | Manifest type + read/write to `.git/.stackr/sandboxes/<branch>.json`. |
| `internal/engine/submit.go` / `prepare.go` (modify) | Special-case the reserved `pr` Branch Context entry: if present, use it as the PR title/body directly instead of AI regeneration. |
| `assets/Dockerfile.base` (embedded via `go:embed`) | The shared base image definition, built on first use. |
| `.claude/skills/sr-sandbox/SKILL.md` | The `/sr-sandbox` skill (thin conversational wrapper). |

## Files to Modify

| File | Change |
|---|---|
| `internal/store/config.go` | Add `Sandbox` sub-struct to `Config`. |
| `cmd/claude.go` (or new) | Optionally teach `sr claude install` to also install the sandbox skill. |
| `README.md` | Add a `sr sandbox` section (at implementation time). |
| `.git/info/exclude` handling | Ensure `.git/.stackr/sandbox.local.json` and sandboxes dir are ignored. |

## Success Criteria

### Functional (MUST)

- `sr sandbox feat-x` creates/reuses the `feat-x` worktree, starts a container, launches zellij→Claude with skip-permissions, and attaches.
- Sessions started in the sandbox appear under the host's project in `~/.claude/projects` (host `claude --resume` sees them).
- Files written by the sandbox to the worktree/`.git`/`~/.claude` are owned by the host user (no root-owned files).
- Detaching leaves the container running; `sr sandbox attach feat-x` reconnects to the live session.
- `sr sandbox attach` (no arg) shows a searchable TUI listing active sandboxes with their initial prompts.
- `sr sandbox stop feat-x` stops without losing the live session; `docker start` + attach resumes it.
- `sr sandbox rm feat-x` removes the container but keeps the worktree; a subsequent `sr sandbox feat-x` cold-resumes via `--continue`.
- `sr sandbox rm feat-x --delete` also removes the worktree and branch.
- `sr sandbox config` opens an editable TUI; `--ai` launches a scoped Claude session.
- Only one image build per project; subsequent sandboxes reuse cached images and only differ by mounts + command.
- The sandbox has **no** GitHub credentials — no ssh keys, no gh token, no credential helper; authenticated remote ops fail inside the container.
- The sandbox records a PR Suggestion via `sr context set pr …`; host-side `sr submit` reads the reserved entry and uses it as the PR title/body (offer edit → push → create/update).
- The sandbox runs behind the egress allowlist by default: allowlisted hosts reachable, others blocked; the agent's initial instructions describe the firewall + request-to-add protocol.
- Adding a domain to the allowlist config + relaunch makes it reachable; `--network full` disables the firewall.
- When a sandbox session ends its turn / asks a question / presents options, its status file flips to an awaiting state with the pending text; replying flips it back to `working`.
- The attention hooks are inert during normal host Claude sessions (not gated `SR_SANDBOX`).
- `sr sandbox ls` / attach TUI show a status column + pending-question text; the shell prompt shows a count of awaiting sandboxes.
- `sr sandbox watch` opens a two-pane dashboard (awaiting section + all section, detail pane); up/down + click navigate, a hotkey jumps to the first awaiting session, clicking attaches.
- `sr sandbox watch --notify` fires a desktop notification on transition into an awaiting state.
- Watch scope defaults per config (project); `--all` watches across projects.

### Functional (SHOULD)

- Caches are mounted by default and auto-detected.
- `sr sandbox ls --all` lists sandboxes across repos.
- Portable config lives in the git-ref store; machine-specific config in the git-ignored local file.

### Non-functional

- Second and later `sr sandbox` invocations start in seconds (no image rebuild, no dep re-download).
- The searchable selector matches the feel of existing stackr TUIs.
- Clear, actionable error when Docker is missing.

## Test Scenarios

1. **Happy path**: `sr sandbox feat-x -- "add tests"` → container up, Claude running with the prompt, attached; worktree present.
2. **Session continuity**: run a sandbox, detach, then on the host run `claude --resume` in the repo → the sandbox session is listed.
3. **Ownership**: create/edit files in the sandbox → host `ls -l` shows they're owned by the developer, not root.
4. **Detach/reattach live**: detach, `sr sandbox attach feat-x` → same live zellij session (in-memory state intact).
5. **Searchable TUI**: two sandboxes running, `sr sandbox attach`, type to filter by branch/prompt, select → attaches.
6. **Cold resume**: `sr sandbox rm feat-x` (container gone, worktree kept), `sr sandbox feat-x` → Claude `--continue` resumes the prior session from host logs.
7. **Full teardown**: `sr sandbox rm feat-x --delete` → container, worktree, and branch removed.
8. **Efficiency**: launch a second sandbox on another branch → no image rebuild, deps already warm from cache mounts.
9. **Per-project layer**: repo with `.stackr/sandbox/Dockerfile` → toolchain present in container; repo without it → runs on base.
10. **Config TUI**: `sr sandbox config`, edit network policy + cache toggle, confirm → portable value written to git-ref config.
11. **Config --ai**: `sr sandbox config --ai` → Claude session with scoped tools edits config per instruction.
12. **No Docker**: on a host without Docker → clear error, no partial state.
13. **post-worktree hook**: repo with a `post-worktree` hook → hook runs before the container launches (e.g. `.env` copied into the worktree).
14. **Attention — question**: sandbox agent asks a clarifying question and ends its turn → status flips to `awaiting-input` with the question text; `sr sandbox ls` shows it; reply → back to `working`.
15. **Attention — choice**: agent uses `AskUserQuestion` → status `awaiting-choice` with options; watch dashboard lists it in the awaiting section.
16. **Host session unaffected**: run normal Claude on the host (no `SR_SANDBOX`) → attention hooks do not fire, no status files written.
17. **Watch dashboard**: two sandboxes, one awaiting → watch app shows it in the top section; hotkey jumps to it; click attaches; detach returns to the dashboard.
18. **Notify mode**: `sr sandbox watch --notify`, trigger an awaiting transition → desktop notification fires; no UI shown.
19. **Binaries on PATH**: drop a binary in `.stackr/sandbox/bin/` and add a host dir via config → both are on `PATH` inside the sandbox.
20. **Firewall default**: sandbox reaches `api.anthropic.com` and GitHub but a non-allowlisted host is blocked; agent's initial instructions mention requesting a domain.
21. **Allowlist add**: add a domain to the allowlist config, relaunch → host now reachable. `--network full` → firewall disabled.

## Open Questions

- **PR Suggestion — reserved key & submit UX** (mechanism settled — ADR-0010: a reserved `pr` Branch Context entry): confirm the key name (`pr`) and how title vs. body are split (single entry vs. `pr`/branch Description), and how the host consume path presents (auto vs. confirm-edit).
- **Base image provisioning** — build-on-first-use from the embedded Dockerfile vs. an optional prebuilt registry image; how the image is versioned/invalidated.
- **Cold-restart zellij mapping** — exact `--continue` vs `--resume <id>` selection when a container is recreated.

## Consultation Log

_Pending — to be filled during implementation review._
