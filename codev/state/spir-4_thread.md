# Builder spir-4 — Thread

## Project
Spec 4: `sr init` — git init with interactive TUI for repo setup

## Phase: Specify

Wrote initial spec draft. Key decisions:
- `ui.Form` is a new reusable Bubble Tea component (text inputs + toggles), not a composition of existing `Input`/`Confirm` components
- Cancellation (Esc) leaves the git repo intact but skips stackr initialization — less surprising than deleting `.git/`
- Non-interactive mode is minimal: `git init` + empty commit + stackr init, no file generation
- Config writes always target `--local` scope, never global
- `--trunk` flag override works in both interactive and non-interactive modes

Awaiting 3-way consultation feedback.
