# Spec 4: sr init — git init with interactive TUI for repo setup

## Problem Statement

Currently `sr init` requires an existing git repository. If run outside a git repo, it fails with `ErrNotARepo`. Users must manually `git init`, configure git settings, add remotes, create initial files, and commit before they can use stackr. This is friction for greenfield projects.

### Current State

- `cmd/init.go` calls `srctx.Discover()` which returns `srerr.ErrNotARepo` if no `.git` directory is found
- The error propagates unhandled — no git-init flow exists
- Users must run `git init` and set up their repo manually before `sr init`

### Desired State

- `sr init` in an empty directory detects the absence of a git repo and bootstraps one
- In interactive mode: a single-screen Bubble Tea form collects git configuration (name, email, default branch, remotes, initial files) and applies them
- In non-interactive mode: silently runs `git init`, creates an empty initial commit, and proceeds with stackr initialization
- Existing behavior (inside a git repo) is unchanged

## Stakeholders

- **Primary**: Developers starting new projects who want stackr from the beginning
- **Secondary**: AI agents using `sr init` programmatically (non-interactive mode)

## Constraints

- Per ADR-0004: the three-mode pattern (programmatic / bare interactive / agent interactive) applies to AI-integrated commands. However, `sr init` is not AI-integrated — it uses the existing `--interactive` flag to distinguish interactive vs non-interactive mode. No `--ai` or `--aiprepare` modes needed here.
- Per ADR-0003: no MCP server; all agent interaction is through the CLI.
- The existing `--trunk` flag must continue to work, overriding the default branch in both modes.
- Config writes must target **repo-local** scope (`git config --local`), never global.
- The new `ui.Form` component must be reusable — it's a general-purpose multi-field form, not init-specific.

## Solution

### Approach: Single-screen Bubble Tea form

A new `ui.Form` Bubble Tea model that renders all fields on one screen with tab/arrow navigation and a single confirm action. This replaces the alternative of sequential prompts (one `ui.Input` per field), which would be slower and feel fragmented.

**Trade-offs**:
- (+) All fields visible at once — user sees full picture before confirming
- (+) Reusable for future TUI forms (worktree setup, config editing)
- (+) Single confirm action — no per-field confirmation fatigue
- (-) More complex Bubble Tea model than sequential prompts
- (-) Requires careful cursor management across heterogeneous field types (text + toggle)

### Flow

#### Interactive mode (no git repo)

1. Run `git init` (via new `Runner.Init()` method)
2. Read pre-fill values (using `git config --get` which reads all scopes — since repo-local config is empty after init, this effectively reads global/system values):
   - `git config user.name` → User name field
   - `git config user.email` → User email field
   - `git config init.defaultBranch` or `"main"` → Default branch field
   - If `--trunk` flag was provided, it overrides the default branch pre-fill (user can still edit the field)
3. Display `ui.Form` with fields:

   | Field | Type | Pre-fill | Required |
   |---|---|---|---|
   | User name | text | global `user.name` | no |
   | User email | text | global `user.email` | no |
   | Default branch | text | `init.defaultBranch` or `main` | yes |
   | Origin URL | text | empty | no |
   | Upstream URL | text | empty | no |
   | Create .gitignore | toggle | yes | — |
   | Create README.md | toggle | yes | — |

4. User navigates fields (tab/shift-tab/arrow), edits values, confirms (enter on confirm button or keybinding)
5. Apply edits:
   - Write non-empty name/email to repo-local config (`git config --local`)
   - If default branch differs from what `git init` created: `git branch -m <old> <new>`
   - If origin URL provided: `git remote add origin <url>`
   - If upstream URL provided: `git remote add upstream <url>`
   - If `.gitignore` toggled on: create file with comprehensive defaults (skip if file already exists)
   - If `README.md` toggled on: create skeleton with directory name as title (skip if file already exists)
   - Commit: if either file created, commit them; otherwise `git commit --allow-empty -m "Initial commit"`
6. Re-run `srctx.Discover()` to pick up the newly created repo
7. Proceed with normal stackr initialization (detect trunk, seed graph, write config)

#### Interactive mode (existing git repo, with commits)

Unchanged. Current `cmd/init.go` behavior — detect trunk, seed graph, write config.

#### Existing git repo with unborn HEAD (post-cancellation recovery)

If `Discover()` succeeds (`.git` exists) but HEAD is unborn (no commits, no branches — e.g., after a previous `sr init` was cancelled), treat this as equivalent to the "no git repo" case: show the TUI form (interactive) or create an empty commit (non-interactive), then proceed with stackr init. Detection: `git rev-parse HEAD` fails when HEAD is unborn.

#### Non-interactive mode (no git repo)

1. `git init`
2. If `--trunk` flag set, rename default branch to match
3. `git commit --allow-empty -m "Initial commit"`
4. Proceed with normal stackr init

No TUI, no generated files, no config edits.

### Cancellation (Esc)

If the user presses Esc during the form:
- The `git init` has already run (the `.git` directory exists)
- stackr initialization does NOT proceed
- The user is left with a bare git repo they can configure manually or re-run `sr init`
- Print a message: `"Init cancelled. Git repository created but stackr not initialized."`

This is the pragmatic choice — undoing `git init` (removing `.git/`) is more surprising than leaving it.

## New Components

### `internal/git/init.go` — `Runner.Init()`

A thin wrapper around `git init`. Takes the directory path implicitly from `Runner.Dir`.

```go
func (r *Runner) Init() error {
    return r.RunGit("init")
}
```

### `internal/ui/form.go` — `ui.Form` Bubble Tea component

A reusable multi-field form supporting:
- **Text fields**: labeled, pre-filled, editable (using `bubbles/textinput`)
- **Toggle fields**: labeled, boolean, toggled with space/enter
- **Navigation**: tab/shift-tab move between fields; up/down arrow also work
- **Confirm**: dedicated confirm button or enter-on-last-field
- **Cancel**: Esc cancels the entire form (returns `ErrCancelled`)

The form model is generic — it takes a slice of field definitions and returns a map of field values.

### `.gitignore` defaults

When toggled on, generate:

```
# OS files
.DS_Store
Thumbs.db
Desktop.ini

# Editor/IDE files
.idea/
.vscode/
*.swp
*.swo
*~
.project
.settings/

# Common build artifacts
dist/
build/
*.o
*.a
*.so
*.dylib

# Dependencies
node_modules/
vendor/
__pycache__/
*.pyc

# Environment/secrets
.env
.env.local
.env.*.local

# Stackr
.worktrees
```

### `README.md` skeleton

```markdown
# <directory-name>
```

Minimal — just the project name derived from the current directory's basename.

## Files to Modify

| File | Change |
|---|---|
| `cmd/init.go` | Catch `ErrNotARepo`, branch into git-init + TUI flow, re-`Discover()` |
| `internal/ui/form.go` (new) | `ui.Form` Bubble Tea component |
| `internal/git/init.go` (new) | `Runner.Init()` method |

No changes to existing `internal/ui/` files (`input.go`, `confirm.go`, `selector.go`).

## Success Criteria

### Functional (MUST)

- `sr init` in empty directory shows form and creates repo with chosen settings
- Form pre-fills name/email from global git config
- Editing a field writes to repo-local config (not global)
- Origin URL → `git remote add origin <url>`
- Upstream URL → `git remote add upstream <url>`
- `.gitignore` toggle on → file created with documented defaults
- `README.md` toggle on → file created with directory name as title
- Both files off → empty initial commit
- Both files on → committed together as initial commit
- Esc cancels stackr init (git repo remains)
- `sr init` inside existing git repo → unchanged behavior (no form shown)

### Functional (SHOULD)

- `--trunk` flag overrides default branch in both interactive and non-interactive modes
- Non-interactive mode: `git init` + empty commit + `sr init`, no TUI, no generated files

### Non-functional

- Form renders correctly in standard 80-column terminals
- Form is responsive — no perceptible lag on field navigation
- `ui.Form` is reusable for future stackr TUI forms

## Test Scenarios

1. **Happy path (interactive, all defaults)**: Run in empty dir, accept defaults, verify repo created with `.gitignore`, `README.md`, initial commit, and stackr initialized
2. **Happy path (interactive, custom values)**: Edit name, email, add origin URL, toggle off README, verify all applied correctly
3. **Cancel flow**: Esc during form → git repo exists but stackr not initialized
4. **Non-interactive**: `--interactive=false` in empty dir → git init + empty commit + stackr init, no form
5. **Existing repo**: `sr init` in existing git repo → no form, normal stackr init
6. **Trunk override**: `--trunk=develop` in empty dir → branch named `develop`
7. **Empty fields**: Leave name/email empty → no local config written for those fields
8. **Remote URLs**: Provide both origin and upstream → both remotes created
9. **Cancel then re-run**: Esc during form → git repo exists with unborn HEAD. Re-run `sr init` → detects unborn HEAD, re-enters the git-init flow (shows form again)
10. **`--trunk` pre-fills form**: `--trunk=develop` in interactive mode → form's "Default branch" field pre-filled with `develop` (user can still edit)
11. **File conflict**: `.gitignore` already exists in directory → toggle on but file not overwritten

## Open Questions

None — all design decisions are settled.

## Consultation Log

### Iteration 1 — Claude review

**Verdict**: COMMENT (HIGH confidence)

Three issues raised, all addressed:

1. **Post-cancellation re-run (Medium)**: After Esc, repo has unborn HEAD (no commits/branches). Re-running `sr init` would take "existing repo" path where `DefaultBranch()`/`RevParse()` fail. **Fix**: added "existing repo with unborn HEAD" flow — detect unborn HEAD via `git rev-parse HEAD` failure, re-enter git-init flow.

2. **`--trunk` flag vs form field precedence (Low)**: Ambiguous whether `--trunk` locks or pre-fills. **Fix**: `--trunk` pre-fills the "Default branch" field; user can still edit it in the form.

3. **File conflict (Low)**: What if `.gitignore`/`README.md` already exist. **Fix**: skip file creation if file already exists (don't overwrite).

Additional notes from review:
- `GetConfig()` reads all scopes (not just global) — clarified in spec language
- Existing `git.Runner` methods (`SetConfig`, `RenameBranch`, `Commit`) provide good building blocks
- `ui.Form` estimated at 150-250 lines — reasonable for a single Bubble Tea model file

Gemini: skipped (agy CLI not installed)
Codex: failed (401 Unauthorized — auth issue)
