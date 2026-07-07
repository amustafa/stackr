# Spec 6: sr implement — implement a GitHub issue or Jira ticket on a new branch

## Problem Statement

Turning a tracked issue into working code is a rote sequence: read the issue, pick a branch name, create the branch, restate the issue as a task for an agent, and (increasingly) decide whether to run that agent in place, in a worktree, or in a sandbox. Today every step is manual, and the "run it safely in a sandbox" path — just built in Spec 5 — has to be wired up by hand each time.

We want a single command, `sr implement <issue>`, that fetches a GitHub issue or Jira ticket, creates a **new tracked branch** for it, and drives implementation — either handing a ready brief to the Claude session already running, spawning one, or launching the Spec 5 **Sandbox** — with the issue linkage recorded so the eventual PR closes the issue.

### Current State

- `sr create` (`internal/engine/create.go`) creates a **tracked** branch (registers it in the stack graph via `g.AddBranch`), off the current branch, with an optional `--worktree` (it already has a `Worktree bool` option) and an objective (`--desc`).
- `sr sandbox` (`internal/engine/sandbox_run.go`) launches a disposable container running Claude on a branch's worktree via `engine.SandboxRun(ctx, SandboxRunOpts{Branch, Prompt, Network, Attach})`. It creates worktrees with the *raw* `WorktreeAdd` (not graph-tracked).
- `--ai` commands (`sr submit --ai`) shell to `claude --bare -p <goal> --allowedTools ... --append-system-prompt <sys>` with JSON context on stdin (`internal/engine/submit.go:96`, ADR-0004).
- GitHub is reached by shelling to `gh` (`internal/engine/github.go`, `ghCheckInstalled`). There is no Jira integration.
- `sr claude install` (`cmd/claude.go`) writes skills (`installSandboxSkill` pattern) — the shipping mechanism for a `/sr-implement` skill.
- There is no `sr implement`.

### Desired State

- `sr implement <ref>` fetches the issue **host-side**, creates a new tracked branch named `<ref>-<title-slug>` off the current branch, records the issue as linkage, and drives implementation per flags/context.
- GitHub issues (`123`, `#123`, issue URL) and Jira tickets (`PROJ-456`, browse URL) both work; source is auto-detected with a `--source` override.
- Flags: `--worktree` (branch lives in a worktree), `--sandbox` (worktree + implement in a container; implies `--worktree`), `--ai` (emit the brief as JSON and exit), `--parent <b>`, `--branch <name>`, `--comments`, `--source github|jira`, `--network` (passthrough to sandbox).
- A thin `/sr-implement` skill installed alongside the others invokes the `--ai` path and implements from the returned brief.

## Stakeholders

- **Primary**: The developer (often already inside a Claude session) who wants to go from an issue ID to an in-progress implementation on a clean branch in one step.
- **Secondary**: The agent that implements — the current session, a spawned `claude`, or a sandboxed one — which receives a self-contained brief and the stackr skill.

## Constraints

- Per **ADR-0003**: no MCP server in the Go path; issue sources are read by shelling to their CLIs (`gh`, `jira`).
- Per **ADR-0004**: the spawn path follows the agent-interactive three-mode pattern (`claude` with a scoped tool allowlist + appended system prompt).
- Per **ADR-0010**: the sandbox carries no credentials — so the issue is fetched on the host and only its resolved text crosses into the container. The agent is asked to record a **PR Suggestion** (reserved `pr` context) as usual.
- Per **ADR-0013** (new): issues are fetched host-side via existing CLIs and baked into a self-contained **Prompt**; Jira is read by shelling to `jira`, not a bespoke REST client.
- Reuses Spec 5 as-is: `engine.SandboxRun` / `SandboxRunOpts` are called, not modified or forked.
- The new branch is always **new** and always **tracked** (`engine.Create`), never an in-place edit of an existing branch.
- `gh` is required for GitHub refs; `jira` is required only for Jira refs — both checked at fetch time with a helpful error.

## Solution

### Behavior matrix

`sr implement <ref>` always: detect source → fetch issue → derive branch name → `engine.Create` (tracked, off `--parent`/current, objective `<ref>: <title>`, `Worktree` if `--worktree`/`--sandbox`) → record `ticket` context → build **Prompt**. Then it diverges:

| Invocation | Behavior |
|---|---|
| `--ai` (any context) | Print JSON `{branch, worktreePath, issueRef, prompt}` to stdout and exit. Caller implements. |
| no `--ai`, inside a Claude session (`CLAUDECODE=1`) | **Hand-off**: same as `--ai` (emit JSON) — never spawn a nested Claude. |
| no `--ai`, bare terminal | Spawn `claude` seeded with the **Prompt** (submitAI pattern), cwd = branch/worktree. |
| `--sandbox`, bare terminal | `SandboxRun{Branch, Prompt, Network, Attach: true}` — launch container and attach. |
| `--sandbox`, inside Claude session or `--ai` | `SandboxRun{... Attach: false}` — launch detached; emit JSON incl. `attachCommand: sr sandbox attach <branch>`. |

`--worktree` is orthogonal: it only decides whether the tracked branch (and any spawned agent's cwd) lives in `.worktrees/<branch>` vs. the current checkout. `--sandbox` implies `--worktree`.

The `CLAUDECODE=1` environment variable (set by Claude Code for every command it runs — verified) is the session-detection signal.

### Issue fetch (host-side, ADR-0013)

- **Source detection**: `#?\d+` → GitHub; `[A-Za-z][A-Za-z0-9]+-\d+` (normalized upper) → Jira; `github.com/.../issues/N` → GitHub; `*.atlassian.net/browse/KEY` → Jira; else require `--source`. `--source github|jira` always overrides.
- **GitHub**: `gh issue view <n> --json title,body,labels,url` (+ `--comments` when `--comments`). Closed/missing → clear error before any branch is created.
- **Jira**: shell to `jira` — `jira issue view <KEY> --raw` (JSON, for the summary → branch name) and `jira issue view <KEY> --plain` (rendered body, ADF already flattened by jira-cli). jira-cli supplies config + keyring token.
- A common `Issue` struct (`ref`, `title`, `body`, `labels`, `url`, optional `comments`) is the fetch output, source-agnostic downstream.

### Branch naming

`<ref>-<slug(title)>`: ref is the number (`123`) or lowercased key (`proj-456`); slug = lowercased title, `[^a-z0-9]+` → `-`, trimmed, capped ~50 chars. Collisions (branch exists) → error suggesting `--branch`. `--branch <name>` overrides entirely. Names stay flat (no `/`) to avoid the sandbox `%2F` worktree-encoding path.

### Prompt building

A fixed preamble + issue content:

```
You are implementing <ref> ("<title>") on branch <branch>.
Implement it fully, commit with `sr commit`, and end your PR with "Closes #<n>"
(record the PR via `sr context set pr` before finishing).

# <title>   [labels: a, b]
<body>

<url>
```

(comments appended under a `--- Discussion ---` header when `--comments`). The same **Prompt** string is what `--ai` returns, what a spawned `claude` receives on stdin, and what `SandboxRun.Prompt` carries into the container — deliberately self-contained so no downstream fetch is needed.

### Issue linkage

- Branch **Description** = `<ref>: <title>` (via `engine.Create` `Desc`).
- `sr context set ticket <url>` on the new branch (source-typed `ticket`), so it shows in `sr info` and flows to PR generation.
- The prompt instructs closing the issue; the resulting PR body (and the reserved `pr` **PR Suggestion**) is the agent's to write.

### CLI surface

```
sr implement <ref> [flags]
  --source github|jira   force the issue source (default: auto-detect)
  --branch <name>        override the derived branch name
  --parent <branch>      parent to stack on (default: current branch)
  --worktree             create the branch in a worktree
  --sandbox              implement in a sandbox (implies --worktree)
  --ai                   emit JSON {branch, worktreePath, issueRef, prompt} and exit
  --comments             include the issue discussion in the prompt
  --network full|allowlist   passthrough to the sandbox (with --sandbox)
```

### The `/sr-implement` skill

A thin skill (installed by `sr claude install`, mirroring `installSandboxSkill`) that drives the `--ai` path and then implements the returned brief. Its value is not the invocation — it's encoding *how* to implement well, distilled from the design-session method:

1. **Ground in the code first** — read the files the issue touches before choosing an approach; the repo is the source of truth, not the issue's wording.
2. **Settle what's genuinely open — ask when unsure** — surface real decisions (API shape, trade-offs, ambiguous terms) to the user rather than guessing; sketch and confirm the approach for a substantial issue; record with `sr context set approach`.
3. **Build in reviewable steps** — implement bottom-up, `sr commit` as you go.
4. **Verify the real path** — build/vet/test, then exercise the actual behavior ("it compiled" ≠ "it works").
5. **Leave a closing PR** — `sr context set pr "…Closes #N"` so host-side `sr submit` opens a PR that closes the issue.

Two lanes: the default/`--worktree` lane where the reading agent implements directly, and the `--sandbox` lane where it launches the container and switches to `sr sandbox watch`/attach. In the sandbox lane, step 2's "ask when unsure" surfaces as an `awaiting-input` **Sandbox Status** the developer sees in Watch (Spec 5) — the ask channel works in both lanes.

### Files

- `cmd/implement.go` — cobra command + flag wiring (mirrors `cmd/sandbox.go`).
- `internal/engine/implement.go` — `Implement(c, ImplementOpts)`: orchestrates detect → fetch → create → link → prompt → drive.
- `internal/engine/issue.go` — `Issue` struct + `fetchGitHubIssue` / `fetchJiraIssue` (shell-outs) + `detectSource` + `deriveBranchName` + `buildPrompt` (pure, unit-tested).
- `cmd/implement_skill.go` — embedded `/sr-implement` SKILL.md + `installImplementSkill`, called from `sr claude install`.

## Success Criteria

- `sr implement 123` on a repo with a GitHub issue #123 creates tracked branch `123-<slug>` off the current branch, with objective and `ticket` context set, and (bare terminal) spawns Claude with a self-contained brief; verified against a real issue via `gh`.
- `sr implement PROJ-456` fetches via `jira` and behaves identically.
- `sr implement 123 --ai` prints valid JSON with a non-empty `prompt` and creates the branch, exiting without spawning.
- `sr implement 123 --sandbox` (from inside a Claude session) launches a detached sandbox on the worktree with the issue as its prompt and prints the attach command; the container's Claude sees the brief. Verified E2E against real Docker + a real issue.
- Auto-detection routes digits→GitHub and `KEY-N`→Jira; `--source` overrides; ambiguous refs error helpfully.
- `go build ./...`, `go vet ./...`, `go test ./...` green; pure helpers (`detectSource`, `deriveBranchName`, `buildPrompt`, URL parsing) unit-tested; fetch/docker tests skip when the tool is unavailable.

## Out of Scope

- Editing an existing branch in place (always a new branch).
- Modifying Spec 5 sandbox internals (reused via its public API).
- A Jira REST client / keyring handling (delegated to jira-cli, ADR-0013).
- Multi-issue / batch implement, and auto-opening the PR (`sr submit` still owns PR creation).
- Pre-writing the PR body (the implementing agent owns it).
