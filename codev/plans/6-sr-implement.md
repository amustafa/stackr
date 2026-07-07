# Plan: sr implement — implement a GitHub issue or Jira ticket on a new branch

## Metadata
- **ID**: plan-2026-07-06-sr-implement
- **Status**: draft
- **Specification**: codev/specs/6-sr-implement.md
- **ADRs**: 0003 (CLI over MCP), 0004 (three-mode AI pattern), 0010 (no creds in sandbox), 0013 (host-side issue fetch via CLIs)
- **Depends on**: Spec 5 (`sr sandbox`) — branch `spec-5-sr-sandbox`; reused via `engine.SandboxRun`, not modified.
- **Created**: 2026-07-06

## Executive Summary

Bottom-up build. First the **pure, source-agnostic core** — issue model, source detection, branch-name derivation, prompt building — all unit-testable with no I/O. Then the **fetchers** (`gh` and `jira` shell-outs) behind a common `Issue` type. Then the **orchestration engine** (`Implement`) that composes detect → fetch → `engine.Create` (tracked) → link → build prompt → drive, where "drive" branches on `--ai` / `CLAUDECODE` / `--sandbox`. Finally **command wiring** and the **`/sr-implement` skill**.

The only genuinely new integration is Jira via jira-cli; everything else reuses existing engine primitives (`engine.Create` with its `Worktree` option, `engine.SandboxRun`, the submitAI spawn shape, `gh` shell-outs, `sr context set`). The design keeps the sandbox untouched — the issue is fetched host-side and passed in as a plain prompt string (ADR-0013).

## Success Metrics
- [ ] All spec Success Criteria met
- [ ] Branch is tracked (appears in `sr log`), off the correct parent, with objective + `ticket` context set
- [ ] GitHub path verified against a real issue via `gh`; Jira path via `jira`
- [ ] `--ai` emits valid JSON `{branch, worktreePath, issueRef, prompt}` and does not spawn
- [ ] `CLAUDECODE=1` hand-off never spawns a nested Claude; bare terminal does spawn
- [ ] `--sandbox` E2E: detached launch on the worktree with the issue as prompt, attach command printed; container Claude sees the brief
- [ ] Pure helpers unit-tested; fetch/docker tests skip when the tool is unavailable
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` green

## Phases (Machine Readable)

```json
{
  "phases": [
    {"id": "phase_1", "title": "Phase 1: Pure core — Issue type, source detection, branch naming, prompt building"},
    {"id": "phase_2", "title": "Phase 2: Fetchers — gh + jira shell-outs behind a common Issue"},
    {"id": "phase_3", "title": "Phase 3: Orchestration engine — Implement() (create tracked branch, link, drive)"},
    {"id": "phase_4", "title": "Phase 4: Command wiring — cmd/implement.go + flags"},
    {"id": "phase_5", "title": "Phase 5: /sr-implement skill install + README"}
  ]
}
```

## Phase Breakdown

### Phase 1: Pure core — Issue type, source detection, branch naming, prompt building
**Dependencies**: None

#### Objectives
- All the source-agnostic logic, no I/O, fully unit-testable.

#### Deliverables
- [ ] `internal/engine/issue.go`: `Issue{Ref, Source, Title, Body, Labels []string, URL string, Comments string}` and `Source` (`github`|`jira`).
- [ ] `detectSource(ref, override string) (Source, normalizedRef, error)` — regex families + issue-URL parsing (`github.com/.../issues/N`, `*.atlassian.net/browse/KEY`); `--source` override; ambiguous → error.
- [ ] `deriveBranchName(iss Issue) string` — `<ref>-<slug(title)>`, lowercase, `[^a-z0-9]+`→`-`, trim, cap ~50; ref = number or lowercased key.
- [ ] `buildPrompt(iss Issue, withComments bool) string` — preamble (branch filled by caller via a small template) + title/labels/body/URL (+ discussion).
- [ ] `internal/engine/issue_test.go`: table tests for detection (digits, `#123`, `PROJ-456`, both URL forms, ambiguous, override), naming (slug edge cases, cap, collisions of chars), prompt shape (comments on/off).

#### Verification
- [ ] `go test ./internal/engine/ -run 'Issue|Source|Branch|Prompt'` green.

### Phase 2: Fetchers — gh + jira shell-outs behind a common Issue
**Dependencies**: Phase 1

#### Objectives
- Populate `Issue` from either source; fail early and clearly.

#### Deliverables
- [ ] `fetchGitHubIssue(n, withComments) (Issue, error)` — `gh issue view <n> --json title,body,labels,url` (+`--comments`); parse JSON; map labels; `ghCheckInstalled` first. Non-existent/closed → actionable error.
- [ ] `jiraCheckInstalled()` (mirror `ghCheckInstalled`) + `fetchJiraIssue(key, withComments) (Issue, error)` — `jira issue view <KEY> --raw` (summary) + `--plain` (body); parse `--raw` JSON for the summary; body from `--plain`.
- [ ] `fetchIssue(source, ref, withComments)` dispatcher.
- [ ] Tests: JSON-parse tests with captured `gh --json` / `jira --raw` fixtures (pure parse funcs); live fetch tests gated behind tool presence (skip otherwise), consistent with the sandbox docker-gated tests.

#### Verification
- [ ] Parsing unit tests green; a manual `gh`-backed fetch returns a populated `Issue`.

### Phase 3: Orchestration engine — Implement()
**Dependencies**: Phases 1–2

#### Objectives
- Compose the whole flow and the drive-mode decision.

#### Deliverables
- [ ] `ImplementOpts{Ref, Source, Branch, Parent, Worktree, Sandbox, AI, Comments, Network string}`.
- [ ] `Implement(c, opts)`:
  1. `detectSource` → `fetchIssue`.
  2. branch name (`--branch` or `deriveBranchName`); error if exists.
  3. `engine.Create(c, CreateOpts{Name, Desc: "<ref>: <title>", Worktree: opts.Worktree || opts.Sandbox, Parent handling})` — parent = current or `--parent` (switch/track as needed).
  4. `sr context set ticket <url>` on the new branch (reuse the context engine API).
  5. `prompt := buildPrompt(...)` with branch filled in; compute `worktreePath` (via the same `.worktrees/<branch>` convention `WorktreeAdd` uses).
  6. Drive:
     - `opts.Sandbox` → `SandboxRun{Branch, Prompt, Network, Attach: bareTerminal()}`; when detached, return/emit JSON incl. `attachCommand`.
     - else `opts.AI || insideClaude()` → emit JSON `{branch, worktreePath, issueRef, prompt}`.
     - else → spawn `claude` (submitAI shape: `--allowedTools`, `--append-system-prompt`, prompt on stdin), cwd = worktree or repo.
- [ ] `insideClaude()` = `os.Getenv("CLAUDECODE") == "1"`; `bareTerminal()` = interactive && !insideClaude && !AI.
- [ ] JSON result struct + marshal helper.
- [ ] Tests: a fake/interface seam for create+fetch+spawn so drive-mode selection is unit-tested (AI vs CLAUDECODE vs bare vs sandbox) without real Docker/Claude.

#### Verification
- [ ] Drive-mode selection tests green; a real `sr implement <gh-issue> --ai` prints correct JSON and creates a tracked branch (`sr log` shows it).

### Phase 4: Command wiring — cmd/implement.go
**Dependencies**: Phase 3

#### Deliverables
- [ ] `cmd/implement.go` — `implementCmd` (`Use: "implement <ref>"`), `ctx.RequireInit()`, flags: `--source --branch --parent --worktree --sandbox --ai --comments --network`; `--sandbox` sets worktree; call `engine.Implement`. Register on `rootCmd`.
- [ ] Arg validation (exactly one ref) with usage examples in `Long`.

#### Verification
- [ ] `sr implement --help` renders; end-to-end GitHub + Jira + `--sandbox` smoke per Success Criteria.

### Phase 5: /sr-implement skill install + README
**Dependencies**: Phase 4

#### Deliverables
- [ ] `cmd/implement_skill.go` — `srImplementSkillContent` (raw-string; escape the backticks in the `sr context set pr "…"` example with the `+ "`" +` trick used by `sandboxSkillContent`) + `installImplementSkill(repoRoot)`. Content per the spec's "The `/sr-implement` skill" section:
  - Front-matter: name `sr-implement`, trigger description (issue/ticket → code; "implement", "sr implement", "/sr-implement", a number/key/URL).
  - **Start**: always the `--ai` form; consume JSON `{branch, worktreePath, issueRef, prompt}`; `cd` into `worktreePath` when set.
  - **Implement the brief** — the five steps: (1) ground in the code, (2) settle-what's-open / **ask when unsure** + `sr context set approach`, (3) build in reviewable steps with `sr commit`, (4) verify the real path, (5) leave a closing PR via `sr context set pr "…Closes #N"`.
  - **Sandbox lane**: `--sandbox --ai` launches detached; report `attachCommand`, use `sr sandbox watch`/`attach`; note the container runs the same five steps and its step-2 asks surface as `awaiting-input` in Watch.
  - **Notes**: always a new branch; `gh`/`jira` requirements; `--branch`/`--parent`.
  - Draft lives at (scratchpad) `sr-implement-SKILL.md` — port verbatim, applying the backtick escaping.
- [ ] Wire `installImplementSkill` into `claudeInstallCmd` (`cmd/claude.go`), alongside `installSandboxSkill`.
- [ ] README section for `sr implement`.

#### Verification
- [ ] `sr claude install` writes `.claude/skills/sr-implement/SKILL.md`; skill drives `--ai` and implements from the JSON.

## Testing Strategy
- **Unit** (no I/O): source detection, branch naming, prompt building, JSON parsing of `gh`/`jira` output, drive-mode selection (behind seams).
- **Gated integration**: real `gh`/`jira` fetch and real `--sandbox` launch skip when the tool/Docker is absent (sandbox precedent).
- **E2E** (manual, per Spec 5 bar): a real GitHub issue and a real Jira ticket → tracked branch + brief; `--sandbox` container sees the prompt.

## Commit Convention
`[Spec 6][Phase N] ...`, explicit `git add`, `sr create`/`sr commit`, with the required Co-Authored-By + Claude-Session trailers. Stack the work on `spec-5-sr-sandbox`.

## Risks
- **jira-cli output drift** — `--plain`/`--raw` formats vary by version; isolate parsing in one function with fixtures.
- **Parent switching** — creating off a non-current `--parent` must not leave the user on the wrong branch or dirty the tree; reuse `engine.Create`'s existing worktree/checkout handling and cover the in-place dirty-tree case with a pre-check.
- **Worktree path alignment** — the tracked-Create worktree and `SandboxRun`'s `ensureWorktree` must resolve to the same path; verified by reusing the `.worktrees/<branch>` convention and letting `ensureWorktree` no-op on an existing dir.
