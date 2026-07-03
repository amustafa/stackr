# Review: sr init — git init with interactive TUI for repo setup

## Summary

Implemented `sr init` git repository bootstrapping with an interactive TUI form. When `sr init` is run outside a git repo, it now creates one and presents a Bubble Tea form for configuring git settings (user name/email, default branch, remotes, initial files). Non-interactive mode silently creates a repo with an empty commit.

Three new/modified files:
- `internal/git/init.go` — `Runner.Init()`, `IsHeadUnborn()` methods
- `internal/git/remote.go` — `Runner.AddRemote()` method
- `internal/ui/form.go` — Reusable `ui.Form` Bubble Tea component (246 lines)
- `cmd/init.go` — Git-init flow integration with form, file generation, and stackr init

Total: ~1030 lines of implementation + ~590 lines of tests.

## Spec Compliance

- [x] `sr init` in empty dir shows form and creates repo with chosen settings
- [x] Form pre-fills name/email from global git config
- [x] Editing a field writes to repo-local config (not global)
- [x] Origin URL → `git remote add origin <url>`
- [x] Upstream URL → `git remote add upstream <url>`
- [x] `.gitignore` toggle on → file created with documented defaults
- [x] `README.md` toggle on → file created with directory name as title
- [x] Both files off → empty initial commit
- [x] Both files on → committed together as initial commit
- [x] Esc cancels stackr init (git repo remains)
- [x] `sr init` inside existing git repo → unchanged behavior (no form shown)
- [x] `--trunk` flag overrides default branch in both modes
- [x] Non-interactive mode: `git init` + empty commit + `sr init`
- [x] Unborn HEAD detection for post-cancellation recovery
- [x] File conflict: `.gitignore`/`README.md` not overwritten if existing

## Deviations from Plan

- **Phase 1**: Added `AddRemote()` to `internal/git/remote.go` (not originally planned — caught by Claude review)
- **Phase 3**: Added `initFlagTrunk` auto-detection from `CurrentBranch()` after bootstrap (not in original plan — caught by Claude review in iteration 1). Also replaced `os.Exit(0)` on cancellation with sentinel error `errInitCancelled`.

## Lessons Learned

### What Went Well
- Bottom-up phase ordering (git wrappers → UI component → integration) kept each phase independently testable
- `ui.Form` component is cleanly reusable — no init-specific logic leaked in
- Claude review caught two real bugs: missing `AddRemote` method and the custom branch name detection issue

### Challenges Encountered
- **Consultation infrastructure**: Gemini (agy CLI not installed) and Codex (401 Unauthorized) were unavailable throughout. Claude was the only functioning reviewer. Architect approved proceeding with single-reviewer consultation.
- **Pre-existing `go vet` issue**: `cmd/shell_hook.go` has a false-positive `printf` directive warning that prevents `go test ./cmd/...` from running with default vet. Worked around with `-vet=off` for cmd package tests; internal tests unaffected.
- **Unborn HEAD edge case**: The branch rename logic needed two approaches — `git branch -m` for repos with commits, `git symbolic-ref HEAD` for unborn HEAD. Caught during spec review by Claude.

### What Would Be Done Differently
- Would have included `AddRemote` in the plan from the start — easy to miss when surveying existing `Runner` methods
- Would run `go vet ./cmd/...` early to discover the pre-existing shell_hook.go issue

## Technical Debt

- `cmd/shell_hook.go:56`: Pre-existing `go vet` false positive — `fmt.Print` with a shell script containing `%s`. Not introduced by this PR but blocks `go test ./cmd/...` with default vet settings.

## Consultation Feedback

### Specify Phase (Round 1)

#### Claude
- **Concern**: Post-cancellation re-run — unborn HEAD causes `DefaultBranch()`/`RevParse()` failure
  - **Addressed**: Added unborn HEAD detection flow to spec
- **Concern**: `--trunk` flag vs form field precedence ambiguous
  - **Addressed**: Clarified `--trunk` pre-fills (user can override)
- **Concern**: File conflict when `.gitignore`/`README.md` already exist
  - **Addressed**: Skip file creation if file exists

#### Gemini
- Skipped (agy CLI not installed)

#### Codex
- Failed (401 Unauthorized)

### Plan Phase (Round 1)

#### Claude
- **Concern**: Missing `AddRemote` method on `git.Runner`
  - **Addressed**: Added to Phase 1 deliverables
- **Concern**: `RunGit("init")` stdout noise in non-interactive mode
  - **Rebutted**: Git init message is informative, not harmful

### Implement Phase 1 (Round 1)
- Claude: APPROVE, no concerns

### Implement Phase 2 (Round 1)
- Claude: APPROVE, no concerns

### Implement Phase 3 (Round 1)

#### Claude
- **Concern**: Custom branch name via form without `--trunk` causes `DefaultBranch()` failure
  - **Addressed**: After bootstrap, set `initFlagTrunk` from `CurrentBranch()`
- **Concern**: `os.Exit(0)` on cancellation not idiomatic
  - **Addressed**: Replaced with sentinel error `errInitCancelled`

### Implement Phase 3 (Round 2)
- Claude: APPROVE, confirmed fixes resolve both issues

## Flaky Tests

No flaky tests encountered.

## Architecture Updates

No architecture updates needed. This feature adds a new entry path to an existing command (`sr init`) without changing any core invariants, data structures, or inter-component contracts.

## Lessons Learned Updates

No lessons learned updates to hot tier needed. The lessons from this project (bottom-up phase ordering works well, Claude catches real bugs in review, verify branch detection after rename) are spec-narrow and don't rise to the level of always-on guidance.

## Follow-up Items

- Fix pre-existing `go vet` issue in `cmd/shell_hook.go:56` (separate PR)
- Consider adding `ui.Form` documentation/examples if other commands adopt it
