# Plan: sr init — git init with interactive TUI for repo setup

## Metadata
- **ID**: plan-2026-07-03-sr-init-git-init
- **Status**: draft
- **Specification**: codev/specs/4-sr-init-git-init-with-interact.md
- **Created**: 2026-07-03

## Executive Summary

Three-phase bottom-up implementation following the codebase's existing layering: low-level git wrapper → reusable UI component → command wiring. Phase 1 adds `Runner.Init()` (trivial). Phase 2 builds the reusable `ui.Form` Bubble Tea component (the bulk of new code). Phase 3 modifies `cmd/init.go` to detect no-repo/unborn-HEAD conditions, orchestrate the git-init + TUI flow, and proceed with stackr initialization.

## Success Metrics
- [ ] All spec MUST criteria met (11 items)
- [ ] All spec SHOULD criteria met (2 items)
- [ ] `ui.Form` is reusable (not init-specific)
- [ ] Existing `sr init` behavior unchanged inside existing repos
- [ ] All tests pass (existing + new)

## Phases (Machine Readable)

```json
{
  "phases": [
    {"id": "phase_1", "title": "Phase 1: Runner.Init() — git init wrapper"},
    {"id": "phase_2", "title": "Phase 2: ui.Form — reusable Bubble Tea form component"},
    {"id": "phase_3", "title": "Phase 3: cmd/init.go — git-init flow integration"}
  ]
}
```

## Phase Breakdown

### Phase 1: Runner.Init() — git init wrapper
**Dependencies**: None

#### Objectives
- Add `Init()` method to `git.Runner` that wraps `git init`
- Add `IsHeadUnborn()` method to detect repos with no commits (for post-cancellation recovery)
- Add `AddRemote()` method to `git.Runner` for adding remotes (origin, upstream)

#### Deliverables
- [ ] `internal/git/init.go` — `Runner.Init()` and `Runner.IsHeadUnborn()` methods
- [ ] `Runner.AddRemote()` method added to `internal/git/remote.go`
- [ ] Unit test for `Init()` — verify `.git` directory created
- [ ] Unit test for `IsHeadUnborn()` — verify true on fresh repo, false after commit
- [ ] Unit test for `AddRemote()` — verify remote appears in `git remote -v`

#### Implementation Details

**File: `internal/git/init.go`** (new)

```go
// Init initializes a new git repository in the runner's directory.
func (r *Runner) Init() error {
    return r.RunGit("init")
}

// IsHeadUnborn returns true if HEAD exists but points to a branch with no commits.
func (r *Runner) IsHeadUnborn() bool {
    _, err := r.RunGitCapture("rev-parse", "HEAD")
    return err != nil
}
```

**File: `internal/git/remote.go`** (modified — add method)

```go
// AddRemote adds a named remote with the given URL.
func (r *Runner) AddRemote(name, url string) error {
    return r.RunGit("remote", "add", name, url)
}
```

All methods follow the existing `Runner` pattern — thin wrappers around `RunGit`/`RunGitCapture`.

#### Acceptance Criteria
- [ ] `Runner.Init()` creates a `.git` directory in `Runner.Dir`
- [ ] `Runner.IsHeadUnborn()` returns `true` on a freshly-inited repo (no commits)
- [ ] `Runner.IsHeadUnborn()` returns `false` on a repo with at least one commit
- [ ] `Runner.AddRemote()` adds a remote visible in `ListRemotes()`
- [ ] Existing tests pass

#### Test Plan
- **Unit Tests**: Create temp dir, call `Init()`, verify `.git` exists. Call `IsHeadUnborn()` before/after a commit.

#### Rollback Strategy
Delete `internal/git/init.go` — no other files modified.

#### Risks
- **Risk**: None significant — trivial wrappers over well-tested git commands.

---

### Phase 2: ui.Form — reusable Bubble Tea form component
**Dependencies**: None (parallel with Phase 1 in theory, but sequential in practice per SPIR)

#### Objectives
- Build a generic multi-field Bubble Tea form supporting text inputs and boolean toggles
- Follow the existing `internal/ui/` patterns (one component per file, public function wrapper)

#### Deliverables
- [ ] `internal/ui/form.go` — `FormField` type, `formModel` Bubble Tea model, public `Form()` function
- [ ] Unit tests for form field navigation, toggle behavior, value collection

#### Implementation Details

**File: `internal/ui/form.go`** (new)

**Types:**

```go
type FieldKind int

const (
    FieldText   FieldKind = iota
    FieldToggle
)

type FormField struct {
    Key      string    // unique identifier for result map
    Label    string    // display label
    Kind     FieldKind
    Value    string    // pre-fill for text fields
    Toggle   bool      // initial state for toggle fields
    Required bool      // if true, text field must be non-empty to confirm
}

type FormResult struct {
    Values  map[string]string // text field values by key
    Toggles map[string]bool   // toggle field values by key
}
```

**Public API:**

```go
func Form(title string, fields []FormField) (*FormResult, error)
```

Returns `*FormResult` on confirm, `nil, ErrCancelled` on Esc.

**Bubble Tea model behavior:**
- Renders all fields vertically with labels
- Text fields use `bubbles/textinput` (only the focused one is active)
- Toggle fields render as `[yes]` / `[no]`, toggled with space or enter
- Navigation: tab/shift-tab/up/down move focus between fields
- Confirm: ctrl+s or a `[ Confirm ]` button at the bottom (enter when focused on it)
- Cancel: Esc at any point returns `ErrCancelled`
- Reuses existing styles from `selector.go` (`titleStyle`, `normalStyle`, `selectedStyle` — same package, no export needed)

**Design choices:**
- `FormResult` uses separate maps for text and toggles (type-safe, no strconv needed by callers)
- Fields are ordered by the input slice — the form renders them top to bottom
- The confirm button is an extra "field" at the bottom, not a separate concept — simplifies cursor management
- Required field validation: if a required text field is empty when confirm is pressed, focus jumps to that field (no dialog/toast)

#### Acceptance Criteria
- [ ] Text fields accept input and return values
- [ ] Toggle fields toggle between true/false on space/enter
- [ ] Tab/shift-tab/arrows navigate between fields
- [ ] Esc returns `ErrCancelled`
- [ ] Confirm collects all values into `FormResult`
- [ ] Required empty text field prevents confirmation and focuses the field
- [ ] Renders within 80 columns

#### Test Plan
- **Unit Tests**: Test `formModel` directly via Bubble Tea's `Update()` with synthetic `tea.KeyMsg`. Verify:
  - Field navigation (cursor moves correctly)
  - Toggle behavior (space toggles value)
  - Value collection on confirm
  - Cancel returns error
  - Required field validation
- **Manual Testing**: Visual confirmation of layout, focus indicators, and style consistency with existing UI components

#### Rollback Strategy
Delete `internal/ui/form.go` — no other files modified.

#### Risks
- **Risk**: Cursor management across mixed field types (text input has its own focus state)
  - **Mitigation**: Only the currently focused text input is in `Focus()` state; all others are `Blur()`ed. Toggle fields have no sub-state to manage.

---

### Phase 3: cmd/init.go — git-init flow integration
**Dependencies**: Phase 1, Phase 2

#### Objectives
- Modify `runInit()` to catch `ErrNotARepo` and branch into git-init flow
- Detect unborn HEAD for post-cancellation recovery
- Implement interactive flow (form → apply → commit → stackr init)
- Implement non-interactive flow (git init → empty commit → stackr init)
- Handle `--trunk` flag in both flows

#### Deliverables
- [ ] Modified `cmd/init.go` — new git-init flow with form integration
- [ ] `.gitignore` template content (as a string constant or embedded)
- [ ] Integration tests covering all entry paths
- [ ] `README.md` skeleton generation

#### Implementation Details

**Modified flow in `runInit()`:**

```
1. Try Discover(cwd)
2. If err == ErrNotARepo:
   a. Create Runner with Dir=cwd
   b. runner.Init()
   c. If interactive:
      - Read pre-fill values (GetConfig for name/email/defaultBranch, --trunk override)
      - Show ui.Form()
      - If cancelled: print message, return nil (no stackr init)
      - Apply form results: SetConfig, branch rename, remote add, file creation, commit
   d. If non-interactive:
      - If --trunk set: rename branch
      - git commit --allow-empty
   e. Re-Discover(cwd) to get full context
   f. Fall through to stackr init
3. If err == nil AND runner.IsHeadUnborn():
   - Same as step 2c/2d (skip git init since repo exists)
4. If err == nil AND HEAD exists:
   - Existing behavior (detect trunk, seed graph, write config)
```

**File creation functions** (private, in `cmd/init.go`):

- `writeGitignore(dir string) error` — writes the default `.gitignore` if file doesn't exist
- `writeReadme(dir string) error` — writes `# <basename>` if file doesn't exist

**`.gitignore` content**: stored as a `const defaultGitignore` string in `cmd/init.go` (not a separate file — it's small and init-specific).

**Commit logic**:
- `git add .gitignore README.md` (only files that were created)
- `git commit -m "Initial commit"` (or `--allow-empty` if no files)
- Use `Runner.Add()` and `Runner.Commit()` which already exist

#### Acceptance Criteria
- [ ] `sr init` in empty dir (interactive): shows form, creates repo, initializes stackr
- [ ] `sr init` in empty dir (non-interactive): creates repo + empty commit, initializes stackr
- [ ] `sr init` in repo with unborn HEAD: re-enters git-init flow (shows form or empty commit)
- [ ] `sr init` in existing repo with commits: unchanged behavior
- [ ] `--trunk=develop` pre-fills form field / renames branch in non-interactive
- [ ] Esc cancels stackr init, prints message, leaves git repo intact
- [ ] `.gitignore`/`README.md` not overwritten if they exist
- [ ] All existing tests pass

#### Test Plan
- **Unit Tests**: Test the flow decision logic (ErrNotARepo → init flow, unborn HEAD → init flow, existing repo → normal flow)
- **Integration Tests**: In temp directories:
  1. Empty dir + interactive=false → verify repo, commit, stackr config
  2. Empty dir + cancel → verify repo exists, no stackr config
  3. Existing repo → verify no git-init behavior
  4. Unborn HEAD repo → verify re-enters init flow
  5. `--trunk` flag → verify branch name
  6. File creation with pre-existing files → verify no overwrite
- **Manual Testing**: Interactive form in real terminal

#### Rollback Strategy
Revert changes to `cmd/init.go` — the file is in version control. Phases 1 and 2 (new files) can be removed independently.

#### Risks
- **Risk**: `Discover()` behavior change could affect other commands
  - **Mitigation**: We don't change `Discover()` — we only handle its error differently in `cmd/init.go`. All other commands still go through `PersistentPreRunE` in `root.go` which skips `init`.
- **Risk**: Commit in newly initialized repo needs `user.name`/`user.email` set
  - **Mitigation**: In interactive mode, the form collects these. In non-interactive mode, git will use global config or fail with a clear error (standard git behavior — not ours to solve).

---

## Dependency Map
```
Phase 1 (Runner.Init) ──┐
                         ├──→ Phase 3 (cmd/init.go integration)
Phase 2 (ui.Form)    ───┘
```

## Resource Requirements
### Development Resources
- Go 1.25.5 (already configured)
- `charmbracelet/bubbles` and `charmbracelet/bubbletea` (already in `go.mod`)

### Infrastructure
- No new dependencies, services, or configuration changes

## Risk Analysis
### Technical Risks
| Risk | Probability | Impact | Mitigation |
|------|------------|--------|------------|
| Cursor management in mixed-field form | Medium | Low | Only one text input focused at a time; toggles have no sub-state |
| Unborn HEAD edge case missed | Low | Medium | Explicit `IsHeadUnborn()` check with unit tests |

## Validation Checkpoints
1. **After Phase 1**: `Runner.Init()` and `IsHeadUnborn()` work in unit tests
2. **After Phase 2**: `ui.Form` renders correctly, handles all input modes, returns correct values
3. **After Phase 3**: Full integration — `sr init` works in all scenarios from spec test matrix

## Documentation Updates Required
- [ ] No external docs needed — `sr init --help` already exists, behavior extends naturally

## Expert Review

### Iteration 1 — Claude review
**Verdict**: COMMENT (HIGH confidence)

Two issues raised:
1. **(Medium) Missing `AddRemote` method**: `git.Runner` has `Push`, `Fetch`, `ListRemotes`, `RemoteBranchExists` but no `AddRemote`. Plan updated to include `Runner.AddRemote()` in Phase 1 deliverables (added to `internal/git/remote.go`).
2. **(Low) `RunGit("init")` stdout noise**: `RunGit` forwards stdout to terminal, so `git init`'s message will print. Acceptable — the message is informative, not harmful.

Gemini: skipped (agy CLI not installed)
Codex: failed (401 Unauthorized — auth issue)

## Change Log
| Date | Change | Reason | Author |
|------|--------|--------|--------|
| 2026-07-03 | Initial plan | Spec approved | Builder spir-4 |
| 2026-07-03 | Added AddRemote deliverable | Claude review: missing git method | Builder spir-4 |
