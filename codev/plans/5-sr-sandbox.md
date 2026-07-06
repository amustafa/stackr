# Plan: sr sandbox — sandboxed Claude sessions on isolated worktrees

## Metadata
- **ID**: plan-2026-07-06-sr-sandbox
- **Status**: draft
- **Specification**: codev/specs/5-sr-sandbox.md
- **ADRs**: 0008 (path-identical mount), 0009 (disposable + host state), 0010 (no creds / PR suggestions)
- **Created**: 2026-07-06

## Executive Summary

Bottom-up build following the codebase's layering: a thin Docker CLI wrapper and base-image provisioning first, then the Local-Data types (manifest / status / suggestion) and sandbox config, then the core launch/attach/lifecycle engine, then the attention hooks, then the surfacing layer (searchable `ls`/attach, watch dashboard, prompt-cache count), then submit integration for credential-free PR suggestions, and finally command wiring + skill install.

The two hardest pieces are **mount assembly** (path-identical worktree + shared `.git` + `~/.claude` + caches + PATH, all at real host paths, running as host UID:GID — ADR-0008) and the **env-gated attention hooks** (installed into the mounted `~/.claude` but inert outside a sandbox). Everything else composes around Docker CLI calls and files under `.git/.stackr/`.

## Success Metrics
- [ ] All spec MUST criteria met
- [ ] All spec SHOULD criteria met
- [ ] Session continuity verified: a sandbox session appears in host `claude --resume`
- [ ] No root-owned files created on the host by a sandbox
- [ ] Second sandbox launch does no image rebuild and no dep re-download
- [ ] Attention hooks inert during normal host Claude sessions
- [ ] Sandbox has zero GitHub credentials; host `sr submit` consumes a deposited PR Suggestion
- [ ] All tests pass (existing + new)

## Phases (Machine Readable)

```json
{
  "phases": [
    {"id": "phase_1", "title": "Phase 1: Docker CLI wrapper + availability"},
    {"id": "phase_2", "title": "Phase 2: Base image provisioning (embedded Dockerfile + per-project layer)"},
    {"id": "phase_3", "title": "Phase 3: Local-Data types — manifest, status, suggestion"},
    {"id": "phase_4", "title": "Phase 4: Sandbox config (three-tier) + auto-derivation"},
    {"id": "phase_5", "title": "Phase 5: Core engine — mount assembly, launch, attach, stop, rm"},
    {"id": "phase_6", "title": "Phase 6: Attention hooks (env-gated) + status publishing"},
    {"id": "phase_7", "title": "Phase 7: Searchable selector, sr sandbox ls, attach TUI"},
    {"id": "phase_8", "title": "Phase 8: Watch dashboard + --notify + prompt-cache count"},
    {"id": "phase_9", "title": "Phase 9: sr sandbox config TUI + --ai"},
    {"id": "phase_10", "title": "Phase 10: PR Suggestion deposit + host submit consume"},
    {"id": "phase_11", "title": "Phase 11: Command wiring, skill install, README"}
  ]
}
```

## Phase Breakdown

### Phase 1: Docker CLI wrapper + availability
**Dependencies**: None

#### Objectives
- A thin, testable wrapper over the `docker` CLI (no SDK dependency — matches the codebase's `git.Runner` shell-wrapper style).
- Clear, actionable error when Docker is missing.

#### Deliverables
- [ ] `internal/docker/runner.go` — `Runner` with `Run`, `RunCapture`, `LookPath`/`Available()`.
- [ ] Methods: `RunDetached(opts)`, `Exec(name, args, tty)`, `Stop(name)`, `Rm(name)`, `PsByLabel(label)`, `ImageExists(tag)`, `Build(dir, dockerfile, tag)`.
- [ ] `RunOpts` struct: image, name, labels, env, workdir, user, mounts (`[]Mount{Source,Target,ReadOnly}`), network, command.
- [ ] Unit tests with a fake exec runner (assert argv assembly), gated integration tests behind a `docker` presence check.

#### Acceptance Criteria
- [ ] `RunOpts` → correct `docker run -d --name … --label … -e … -w … -u … --mount … <image> <cmd>` argv.
- [ ] `Available()` false → callers surface "Docker not found" without partial state.

---

### Phase 2: Base image provisioning
**Dependencies**: Phase 1

#### Objectives
- One cached base image built on first use; optional per-project layer.

#### Deliverables
- [ ] `assets/Dockerfile.base` embedded via `go:embed`; installs `git`, `gh`, `curl`, ca-certs, `zellij`, the `claude` CLI, and `sr`.
- [ ] `internal/sandbox/image.go` — `EnsureImage(cfg)`: build base if absent (tag `stackr-fork:base`); if `.stackr/fork/Dockerfile` exists, build/refresh the per-project layer `FROM` base, tag `stackr-fork:<repo-hash>`.
- [ ] Content-hash tagging so an unchanged Dockerfile is a cache hit; rebuild only on change.

#### Acceptance Criteria
- [ ] First call builds; subsequent calls no-op (image exists, hash unchanged).
- [ ] Repo with `.stackr/fork/Dockerfile` → project image derived from base; repo without → base used directly.

---

### Phase 3: Local-Data types — manifest, status, suggestion
**Dependencies**: None (can run parallel to 1–2)

#### Objectives
- Typed read/write for the three per-branch artifacts under `.git/.stackr/`.

#### Deliverables
- [ ] `internal/sandbox/manifest.go` — `Manifest{Branch, Image, Mounts, Command, SessionID}` ↔ `.git/.stackr/forks/<branch>.json`.
- [ ] `internal/sandbox/status.go` — `Status{State, Reason, UpdatedAt}` (states: `working`, `awaiting-input`, `awaiting-choice`, `exited`) ↔ `.git/.stackr/forks/<branch>.status`; plus a `Watch(dir)` helper (fsnotify or poll) emitting change events.
- [ ] `internal/sandbox/suggestion.go` — `Suggestion{Title, Body, CreatedAt}` ↔ `.git/.stackr/pr-suggestions/<branch>.json`; `Read`, `Write`, `Clear`.
- [ ] Ensure `.git/.stackr/` paths are Local Data (never in refs/stackr/data); confirm they're outside the worktree.
- [ ] Unit tests: round-trip each type; `Watch` fires on write.

#### Acceptance Criteria
- [ ] Round-trip fidelity for all three types.
- [ ] Files land under the main repo's `.git/.stackr/`, not the worktree.

---

### Phase 4: Sandbox config (three-tier) + auto-derivation
**Dependencies**: None (parallel to 1–3)

#### Objectives
- Portable + machine-specific config, with everything else auto-derived.

#### Deliverables
- [ ] `internal/store/config.go` — add `Sandbox` sub-struct (portable): `Network`, `BaseImage`, `DockerfilePath`, `FirewallAllowlist`, `Caches bool`, `PromptTemplate`, `BinDir`, `WatchScope`.
- [ ] `internal/sandbox/localconfig.go` — machine-specific loader for `.git/.stackr/sandbox.local.json`: `CachePaths`, `ExtraMounts`, `PathMounts`, `DockerSocket`.
- [ ] `internal/sandbox/derive.go` — auto-derivations: worktree path for a branch, repo root, `.git` dir, HOME, `UID:GID`, project hash.
- [ ] Config precedence resolver → an effective `LaunchConfig` consumed by Phase 5.
- [ ] Ensure `.git/.stackr/sandbox.local.json` is git-ignored.
- [ ] Unit tests for merge/precedence and derivation.

#### Acceptance Criteria
- [ ] Portable values persist to the git-ref config; machine-specific to the local file.
- [ ] Auto-derived values are never written to either store.

---

### Phase 5: Core engine — mount assembly, launch, attach, stop, rm
**Dependencies**: Phases 1, 2, 3, 4

#### Objectives
- The heart: assemble path-identical mounts, launch zellij→Claude detached, attach, and tear down. Implements ADR-0008 and ADR-0009.

#### Implementation Details

**Mount assembly** (`internal/engine/sandbox.go`) — all at real host paths:
- worktree `<repo>/.worktrees/<branch>` → same path (cwd)
- `<repo>/.git` → same path (rw, shared — ADR-0008)
- `~/.claude` → same path (rw)
- caches (auto-derived, if `Caches`) → same paths
- bin dir `.stackr/sandbox/bin` + machine `PathMounts` → same paths, composed into `PATH`
- machine `ExtraMounts` → as configured

**Launch**:
- `docker run -d` as host `UID:GID`, `HOME=<host home>`, `-e SR_SANDBOX=<branch>`, `-e GIT_AUTHOR_*/GIT_COMMITTER_*`, `-e PATH=<composed>`, label `stackr.sandbox=<repo-hash>`, workdir = worktree path.
- Command: `zellij attach --create <branch>` running `claude --dangerously-skip-permissions "<prompt>"` (or plain `claude` if no prompt). Cold-resume path uses `claude --continue`.
- Write the manifest.

**Attach**: `docker exec -it <container> zellij attach <branch>` (shared by `sandbox` auto-attach and `attach`).
**Stop**: `docker stop`. **Rm**: `docker rm -f`; `--delete` also `WorktreeRemove(--delete)`.

#### Deliverables
- [ ] `internal/engine/sandbox.go` — `SandboxRun`, `SandboxAttach`, `SandboxStop`, `SandboxRm`, `SandboxList`.
- [ ] Worktree ensure reuses `engine.WorktreeAdd` (fires `post-worktree` hook — ADR-0006).
- [ ] Git identity env injected so commits work despite a UID absent from image `/etc/passwd`.
- [ ] Integration test (docker-gated): launch → exec `id -u` matches host, `pwd` = worktree path, `git status` works, a written file is host-owned.

#### Acceptance Criteria
- [ ] Session continuity: sandbox session JSONL lands in host `~/.claude/projects/<same-hash>`.
- [ ] No root-owned files on host.
- [ ] `rm` keeps worktree by default; `--delete` removes it.
- [ ] Relaunch after `rm` cold-resumes via `--continue`.

---

### Phase 6: Attention hooks (env-gated) + status publishing
**Dependencies**: Phase 3 (status), Phase 5 (SR_SANDBOX env)

#### Objectives
- Publish Sandbox Status from Claude Code hooks, inert on the host.

#### Implementation Details
- Install hook entries into the mounted `~/.claude` settings (idempotent, marked/owned block) mapping: `PreToolUse:AskUserQuestion`→`awaiting-choice`, `Stop`→`awaiting-input`, `Notification`→`awaiting-input`, `UserPromptSubmit`→`working`, `SessionEnd`→`exited`.
- Hook command is a small script that **no-ops unless `$SR_SANDBOX` is set**, then writes `.git/.stackr/forks/$SR_SANDBOX.status` with state + a `reason` extracted from the hook payload (last message / tool input).

#### Deliverables
- [ ] `internal/sandbox/hooks.go` — install/uninstall the gated hook block; the status-writer script (embedded).
- [ ] Idempotent install (safe across many sandboxes sharing one `~/.claude`).
- [ ] Tests: script writes correct state given a sample payload; no-op when `SR_SANDBOX` unset.

#### Acceptance Criteria
- [ ] Awaiting transitions produce the right state + reason.
- [ ] Normal host session (no `SR_SANDBOX`) writes nothing.

---

### Phase 7: Searchable selector, sr sandbox ls, attach TUI
**Dependencies**: Phases 3, 5

#### Objectives
- Discovery surface with status.

#### Deliverables
- [ ] `internal/ui/filter_selector.go` — searchable selector (a `bubbles/textinput` filter over the existing `selector.go` model); rows show branch + truncated prompt/status.
- [ ] `SandboxList` joins `PsByLabel` (running truth) with manifests + status files.
- [ ] `sr sandbox ls [--all]` — table: branch, state, reason, container status.
- [ ] Bare `sr sandbox attach` → filter_selector over active sandboxes; direct `attach <branch>` skips it.

#### Acceptance Criteria
- [ ] Typing filters the list; enter attaches.
- [ ] `ls` shows status column + pending text; `--all` crosses repos.

---

### Phase 8: Watch dashboard + --notify + prompt-cache count
**Dependencies**: Phases 3, 5, 7

#### Objectives
- The full attention application + ambient signal.

#### Deliverables
- [ ] `internal/ui/watch.go` — two-pane Bubble Tea app: left = awaiting section over all section (live via `status.Watch`); right = selected session detail (branch, state, pending question, initial prompt). Up/down + mouse click navigate; click attaches (suspend program → `docker exec` zellij attach → resume); hotkey → first awaiting.
- [ ] `internal/engine/sandbox_watch.go` — `--notify` headless notifier (desktop notification on transition into awaiting; `notify-send`/OS-appropriate); scope from config/`--all`.
- [ ] Prompt Cache extension: write an awaiting-count so the shell hook can expose `SR_SANDBOX_AWAITING`.
- [ ] Tests: reducer transitions (list membership on status change), notifier fires once per transition.

#### Acceptance Criteria
- [ ] Dashboard reflects live status changes; hotkey + click work; detach returns to it.
- [ ] `--notify` fires on transition to awaiting; no UI.
- [ ] Prompt shows awaiting count with no running process.

---

### Phase 9: sr sandbox config TUI + --ai
**Dependencies**: Phase 4

#### Objectives
- Human + AI config editing.

#### Deliverables
- [ ] `sr sandbox config` → editable form via `ui.Form` (reuse) covering portable + machine-specific fields; writes to the right tier.
- [ ] `sr sandbox config --ai` → mirror `submitAI` (`internal/engine/submit.go:96`): current config as JSON on stdin, `--allowedTools Read,Edit,Bash(sr sandbox config *)`, `--append-system-prompt <prompt>` (prompt authored by user; placeholder committed).

#### Acceptance Criteria
- [ ] Edits persist to the correct tier.
- [ ] `--ai` launches a scoped Claude session that can read + modify config.

---

### Phase 10: PR Suggestion deposit + host submit consume
**Dependencies**: Phase 3 (suggestion)

#### Objectives
- Credential-free PR flow (ADR-0010).

#### Deliverables
- [ ] Deposit mode (e.g. `sr submit --deposit`) — generate/accept title+body and persist a Suggestion instead of pushing.
- [ ] `internal/engine/submit.go` — on host `sr submit`, detect a Suggestion for the branch → confirm → push branch → create/update PR with it → `Clear()`.
- [ ] Tests: deposit round-trips; consume path calls push + PR-create with suggestion fields and clears on success.

#### Acceptance Criteria
- [ ] Sandbox (no creds) deposits; host `sr submit` turns it into a pushed branch + PR; suggestion cleared.

---

### Phase 11: Command wiring, skill install, README
**Dependencies**: All

#### Deliverables
- [ ] `cmd/sandbox.go` — cobra tree: default `run`, `attach`, `ls`, `stop`, `rm [--delete]`, `config [--ai]`, `watch [--notify] [--all]`; alias `sb`.
- [ ] Flags: `sr sandbox [branch] [-- "<prompt>"]`.
- [ ] Install the `sr-sandbox` skill (extend `sr claude install` or a dedicated installer).
- [ ] README `sr sandbox` section.
- [ ] Shell-completion entries.

#### Acceptance Criteria
- [ ] Full command family works end-to-end; `sb` alias resolves.

## Testing Strategy
- **Unit**: docker argv assembly (fake exec), config precedence/derivation, Local-Data round-trips, hook script state mapping, watch reducer, suggestion consume.
- **Integration (docker-gated)**: launch → ownership + cwd + git works + session continuity; stop/rm/relaunch cold-resume; attention transition writes status.
- **Manual smoke**: attach UX (zellij detach/reattach), watch dashboard navigation + click-attach, `--notify`, prompt count.
- Gate docker-dependent tests behind an availability check so CI without Docker still passes.

## Risks & Mitigations
- **UID not in image `/etc/passwd`** → git/tools unhappy. Mitigate: inject `GIT_*` identity env; optionally synthesize a passwd entry at entrypoint.
- **Session-hash assumption** (hash derived purely from cwd path) → verify against the installed Claude Code version before relying on it; fall back to explicit `--resume <id>` from the manifest if the hash scheme differs.
- **Shared `~/.claude` hook install races** across concurrent sandboxes → idempotent, marked block; write-with-rename.
- **`zellij`/`claude` version drift in the base image** → pin versions in `Dockerfile.base`; content-hash tag forces rebuild on bump.
- **fsnotify on `.git`** may be noisy → watch only `.git/.stackr/forks/` and debounce.

## Open Questions (from spec, to resolve during implementation)
- Base-image provisioning: build-on-first-use vs optional prebuilt registry image.
- Cold-restart zellij mapping: `--continue` vs `--resume <id>` selection.
- PR Suggestion JSON shape + deposit flag name + host consume UX (auto vs confirm).
- Watch right-pane content (detail/preview assumed; confirm with user).

## Consultation Log
_Pending._
