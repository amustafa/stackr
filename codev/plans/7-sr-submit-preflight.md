# Plan: sr submit preflight — settle content divergence before anything is pushed

## Metadata
- **ID**: plan-2026-07-31-sr-submit-preflight
- **Status**: draft
- **Specification**: codev/specs/7-sr-submit-preflight.md
- **ADRs**: 0014 (Push Record, not content inspection), 0015 (Frozen is a wall), 0002 (undo is graph-only — bounds the rollback token), 0003 (CLI over MCP — `gh` shell-outs)
- **Created**: 2026-07-31

## Executive Summary

Bottom-up. Every layer below the orchestration is independently testable against **real git repositories in temp dirs**, which is how the existing suite works (`internal/git/*_test.go`, `internal/engine/base_test.go`) — no mocking of git.

Phase 1 puts the two missing primitives in `internal/git`: a **pinned-lease push** and the `merge-tree` / remote-only-commit plumbing. Phase 2 adds the **Push Record** local store. Phase 3 composes them into a pure-ish **classifier** returning a disposition per branch — the heart of the spec, and the phase with the densest tests. Phase 4 restructures submit into **preflight / push / reconcile**. Phase 5 lands the **Frozen wall**, which touches `restack` and therefore `sync`. Phase 6 clears the folded-in fixes and the flag surface.

Phases 1–3 add no behaviour change to `sr submit`; the command only changes in Phase 4. That ordering means the risky, wide-blast-radius phases (4, 5) land on top of primitives that already have tests.

## Success Metrics
- [ ] All spec Success Criteria met
- [ ] No unqualified `--force-with-lease` remains in any submit path
- [ ] Classifier verified against real repos for: rebase, squash, absorb, split, reorder, adjacent-context restack, collaborator commit, collaborator revert, collaborator drop-and-force
- [ ] Preflight publishes nothing on Stop; remediations kept; rollback id printed
- [ ] Frozen branch and its upstack excluded from restack and push, and named in output
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` green

## Phases (Machine Readable)

```json
{
  "phases": [
    {"id": "phase_1", "title": "Phase 1: git primitives — pinned-lease push, merge-tree, remote-only commits"},
    {"id": "phase_2", "title": "Phase 2: Push Record local store"},
    {"id": "phase_3", "title": "Phase 3: Divergence classifier (tiers 0-2)"},
    {"id": "phase_4", "title": "Phase 4: Preflight / push / reconcile restructure"},
    {"id": "phase_5", "title": "Phase 5: Frozen is a wall"},
    {"id": "phase_6", "title": "Phase 6: Flag surface, CI policy, folded-in fixes"}
  ]
}
```

## Phase Breakdown

### Phase 1: git primitives
**Dependencies**: None

#### Objectives
Add the git operations the design needs and the current `Runner` lacks. No engine changes.

#### Deliverables
- [ ] `internal/git/remote.go`: `PushPinned(remote, branch, expectSHA string, setUpstream bool) error` emitting
      `push [-u] --force-with-lease=refs/heads/<branch>:<expectSHA> <remote> <branch>:refs/heads/<branch>`.
      Empty `expectSHA` produces the empty-expect form (asserts the ref does not exist). Never emits `--force-if-includes`.
- [ ] A typed `StaleLeaseError` so the push phase can distinguish a lease rejection from a transport failure
      (detect on stderr containing `stale info` / `fetch first` / `rejected`).
- [ ] `internal/git/mergetree.go`:
      - `MergeTreeWriteTree(ours, theirs string) (tree string, clean bool, err error)` — `merge-tree --write-tree`;
        exit 0 → clean with tree; exit 1 → not clean (first line is still a tree OID, ignore it); other/128 → err.
      - `MergeTreeOnto(mergeBase, ours, theirs string)` — same with `--merge-base=<mergeBase>`.
      - `TreeOf(rev string) (string, error)` — `rev-parse <rev>^{tree}`.
      - `RemoteOnlyCommits(local, remote string) ([]string, error)` — `rev-list --right-only --cherry-pick <local>...<remote>`,
        **no `--no-merges`**, oldest-first output.
      - `IsRootCommit(sha string) bool`, `FirstParent(sha string) (string, error)`.
- [ ] `internal/git/merge.go`: `MergeSquash(theirs string) error` — `merge --squash`, returning `MergeConflictError` on conflict;
      `MergeSquashAbort()` (`reset --merge`). Cherry-pick equivalents: `CherryPick(shas ...string) error`, `CherryPickAbort()`.
- [ ] Tests in `internal/git/mergetree_test.go` and `remote_test.go` building real repos + a bare remote:
      pinned lease accepts when the remote matches, rejects with `StaleLeaseError` when a third party pushed after
      our inspection; empty-expect succeeds on create and rejects once the ref exists; merge-tree clean/conflict/unrelated-histories.

#### Verification
- [ ] `go test ./internal/git/` green.
- [ ] A test asserts the **unqualified** lease is not used by `PushPinned` (argument-shape assertion).

### Phase 2: Push Record local store
**Dependencies**: None (parallel with Phase 1)

#### Objectives
Local-only, per-clone record of what we left on each remote branch.

#### Deliverables
- [ ] `internal/store/push_record.go`:
      ```go
      type PushRecords struct {
          Version int                                  `json:"version"`
          Remotes map[string]map[string]PushRecordEntry `json:"remotes"`
      }
      type PushRecordEntry struct {
          SHA       string `json:"sha"`
          UpdatedAt string `json:"updatedAt,omitempty"`
      }
      ```
      Stored at `<git-common-dir>/.stackr/push_records.json` via the existing local `Store` (`RefStore.local`,
      `refstore.go:38`). Nested map keeps `remote` and `branch` unambiguous — a flat `remote/branch` key is
      ambiguous for branches containing `/`.
- [ ] `RefStore` methods: `ReadPushRecords()`, `WritePushRecords()`, `PushRecordFor(remote, branch)`,
      `SetPushRecord(remote, branch, sha)`, `DeletePushRecord(remote, branch)`, `DeletePushRecordsForBranch(branch)`.
- [ ] `Store.Init` also creates `.stackr/rollback/` (currently only `.stackr`, `undo`, `undo/snapshots` — `store.go:29`).
- [ ] GC: call `DeletePushRecordsForBranch` wherever the graph drops a branch — `cleanMergedBranches` (`sync.go:169`)
      and `sr delete`. A recreated branch name must not inherit a stale claim.
- [ ] Tests: round-trip, missing-file default, branch names containing `/`, multiple remotes, GC.

#### Verification
- [ ] `go test ./internal/store/` green.
- [ ] Grep confirms nothing writes the Push Record into `refs/stackr/data` (it must never reach `PRInfo`).

### Phase 3: Divergence classifier
**Dependencies**: Phases 1, 2

#### Objectives
One function that answers, per branch, what should happen — with no side effects.

#### Deliverables
- [ ] `internal/engine/divergence.go`:
      ```go
      type Disposition int // NoPush | PushPlain | PushForce | NeedsDecision
      type Classification struct {
          Disposition Disposition
          RemoteSHA   string // lease pin; "" when the remote ref is absent
          Reason      string // populated for NeedsDecision
          RemoteOnly  []string
      }
      func ClassifyBranch(c *context.Context, remote, branch string) (Classification, error)
      ```
- [ ] Implements the spec's ladder exactly: missing remote → `PushPlain`; equal → `NoPush`; remote ancestor → `PushPlain`;
      local ancestor → `NeedsDecision("behind")`; **tier 0** Push Record match → `PushForce`; no merge base → `NeedsDecision`;
      **tier 1** whole-branch containment; **tier 2** per-commit replay with `--merge-base=X^1`, root commits and the cap
      (`maxUnmatchedCommits = 50`) both → `NeedsDecision`. Any unexpected exit → `NeedsDecision`.
- [ ] `internal/engine/divergence_test.go`, table-driven over real repos with a bare remote. One case per spec Success
      Criterion, minimally: rebase-onto-new-trunk; squash; absorb (disjoint region); split; reorder; restack where trunk
      edited a line **adjacent** to a branch hunk (the case that defeats patch-id); collaborator commit; collaborator
      revert; collaborator drop-and-force-push (must be caught by tier 0 even though the remote is an ancestor);
      first push; unrelated histories.

#### Verification
- [ ] `go test ./internal/engine/ -run Divergence` green.
- [ ] The drop-and-force case is asserted to be `NeedsDecision` **with the Push Record present** and to (incorrectly, and
      documented as such) look safe with it absent — locking in why tier 0 is primary.

### Phase 4: Preflight / push / reconcile restructure
**Dependencies**: Phase 3

#### Objectives
Rework `Submit` into three phases; publish nothing until everything is settled.

#### Deliverables
- [ ] `internal/engine/preflight.go`:
      - `buildPushSet(c, g, opts, current) ([]string, error)` — bottom-up, honouring `--stack`, `--update-only`, Frozen
        walls; recomputed each round.
      - `Preflight(c, opts, set) (PreflightResult, error)` — fetch, rollback token, per-branch classify + remediate,
        restack-and-reload after any local change, blocked-restack Stop|Skip prompt with lineage drop.
      - `remediate(...)` — the five-option flat menu; merge conflict → abort → Stop.
      - Reloads `g`/`prInfo` from the store after every restack; carries branch **names** only.
- [ ] `internal/engine/rollback_token.go` — write `.stackr/rollback/<id>.json` before the first mutation; print the id
      on Stop. (Reading it back is the deferred `sr rollback` spec; this phase only writes.)
- [ ] `submit.go` restructured: `Submit` → `buildPushSet` → `Preflight` → re-fetch + tier-0 revalidation →
      `pushPhase` (bottom-up `PushPinned`) → `reconcilePhase` (Push Record + PR base per branch; GitHub stack per segment).
- [ ] `pushBranch` loses its `opts.Force` path and takes a pinned lease SHA; PR-base retargeting moves into reconcile.
- [ ] Tests: preflight ordering (bottom-up), stop-keeps-remediations, nothing-pushed-on-stop, graph reload after restack
      (regression test for the stale-`g` bug: assert `pushBranch` receives the **post-restack** parent), re-fetch
      abandons the push phase when the remote moved.

#### Verification
- [ ] `go test ./internal/engine/` green.
- [ ] Manual E2E against a scratch GitHub repo: restacked 3-branch stack submits with no prompts and no `-f`.

### Phase 5: Frozen is a wall
**Dependencies**: None (independent; sequenced here to keep Phase 4's diff readable)

#### Objectives
Make `Frozen` mean what ADR-0015 and the glossary say.

#### Deliverables
- [ ] `restackBranches` (`restack.go:140`) skips a frozen branch and marks it in the existing `blocked` map so its
      lineage inherits the exclusion — the same mechanism already used for conflicts.
- [ ] Frozen is reported **always**, independent of `SkipBlocked`, and never returns an error: it is an intention,
      not a failure. `skippedBranch.reason` distinguishes "frozen" from "conflict".
- [ ] `buildPushSet` excludes a frozen branch and its full upstack; the excluded lineage is named in submit's output.
- [ ] Tests: mid-stack frozen branch is not rebased, its children are not rebased, both are excluded from the push set,
      and `sr sync` (which calls `Restack`) honours it.

#### Verification
- [ ] `go test ./internal/engine/ -run 'Restack|Frozen'` green.

### Phase 6: Flag surface, CI policy, folded-in fixes
**Dependencies**: Phases 4, 5

#### Objectives
Close the flag surface and the pre-existing defects the new flow would inherit.

#### Deliverables
- [ ] `cmd/submit.go`: add `--no-force`; `--force` documented as "overwrite remote on content divergence without
      prompting"; mutual exclusion enforced in `RunE`.
- [ ] `--update-only` implemented in `buildPushSet` (PR number or existing remote ref).
- [ ] `--dry-run` audited end to end: no rollback token, no remediation, no restack, no push, no PR/base/stack writes —
      and it prints what it *would* remediate.
- [ ] TTY detection in `cmd/root.go:63` so `--interactive` reflects reality; CI policy (fail on Content Divergence,
      fail on blocked restack, still force when preflight proved lossless) becomes reachable.
- [ ] `TryPushMeta` on every submit path (`submit.go:246`, `:284`, `:296`, `:372`), not only `submitStack`.
- [ ] `mergeBranchPR` (`store/merge.go:404`) → field-level merge of `Number/URL/State/Title/Draft/BaseBranch/StackNumber`.
      For `StackNumber`: prefer the side that changed from base; both changed to different non-zero → clear to 0.
- [ ] `resolveDivergedStack` (`ghstack.go:466`) implemented as rebuild: clear `prInfo.StackNumber` → `ghUnstack` →
      `ghUpdatePRBase` per PR → `ghCreateStack`. Signature widened to take `baseByPR`, `prInfo`, interactivity.
      Non-interactive warns instead of rebuilding.
- [ ] `--ai` left untouched, with a comment recording that preflight deliberately does not run in the parent.

#### Verification
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` green.
- [ ] `sr submit --dry-run` on a diverged stack mutates nothing (assert clean `git status` and unchanged branch tips).

## Risks

- **Blast radius beyond submit.** Phase 5 changes `restack`, therefore `sync`. Mitigated by landing it as its own
  phase with its own tests.
- **Test cost.** The classifier's value is entirely in its edge cases, and they can only be tested against real repos.
  Phase 3 is deliberately the heaviest test phase; skimping there removes the reason to trust any of this.
- **`merge-tree --write-tree` needs git ≥ 2.38.** Detect and degrade to `NeedsDecision` (prompt) on older git rather
  than guessing — conservative in the right direction.
- **A configured `merge=ours` driver** can in principle make tier 1 report "safe" for a genuinely new remote hunk.
  It requires the user's own git config, tier 2 catches it in practice, and tier 0 makes it moot for the common case.
  Documented, not closed.

## Out of Scope

- `sr checkpoint` / `sr rollback` as general features (this plan only *writes* the token).
- A "review" affordance explaining a tier-0 failure.
- Refactoring `sr get`'s divergence handling into a shared mechanism.
