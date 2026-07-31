# Spec 7: sr submit preflight — settle content divergence before anything is pushed

## Problem Statement

**Restack** rewrites SHAs, so nearly every **Submit** of a stacked branch needs a force push. That leaves two bad options: force blindly and risk destroying a collaborator's work, or refuse to force and make the tool unusable on its own core workflow. Today `sr submit` takes the first, gated behind `-f`, and the user carries the risk in their head.

Worse, the safety net isn't one. `git.PushWithUpstream` uses **unqualified** `--force-with-lease` (`internal/git/remote.go:15`), which compares the server against the local **remote-tracking ref**. That happens to be safe today only because submit never fetches. The moment submit fetches — which every check in this spec requires — the lease compares against the commit the fetch just wrote and passes by construction. This was verified empirically: after a fetch, an unqualified `--force-with-lease` push **succeeded in destroying a collaborator's commit**.

We want `sr submit` to decide, per branch, whether a force push would lose anything; to settle everything that would with the user *before* publishing anything; and to force push safely once nothing will be lost.

### Current State

- `Submit` (`internal/engine/submit.go:32`) reads the graph, PR info and config, then dispatches to `submitStack` or `submitSingle`. **It never fetches.**
- `pushBranch` (`submit.go:445`) pushes with `opts.Force` passed straight to `PushWithUpstream`, then best-effort retargets the PR base (`submit.go:474`) — best-effort because GitHub refuses to change a base while the PR is grouped in a stack.
- `pushDownstack` / `pushUpstack` (`submit.go:376`, `:400`) compute their branch lists **once** from a graph read at `submit.go:56`.
- `sr get` already has a divergence menu (`get.go:328` `handleDivergence`): *Replace with remote* / *Keep local* / *Merge remote into local*, with non-interactive meaning "skip".
- `syncGitHubStacks` (`ghstack.go:245`) segments the pushed set into linear runs and registers them; `resolveDivergedStack` (`ghstack.go:466`) is an **unimplemented hard error** swallowed as a warning.
- Bugs this work must not inherit: `--update-only` is declared and wired but **read nowhere**; `TryPushMeta` fires only in `submitStack` (`submit.go:163`); `mergeBranchPR` (`store/merge.go:404`) picks a whole `BranchPR` by state rank so two clones clobber each other's `BaseBranch`/`StackNumber`; `--interactive` defaults `true` with no TTY detection (`cmd/root.go:63`); `restack.go` never reads `Frozen`.

### Desired State

- `sr submit` runs a **Preflight** that fetches, classifies every branch in the push set, resolves **Content Divergence** with the user, and restacks after any local change — **publishing nothing**.
- Only once the whole push set is settled does anything get pushed, bottom-up, with a lease **pinned to the SHA that was actually inspected**.
- The common case — a restacked stack that nobody else touched — is silent and needs no `-f`.
- **Frozen** means what the glossary says, per operation (ADR-0015): restack stops at it, submit steps over it.
- Force-push safety comes from a **Push Record**, not from inspecting content (ADR-0014).

## Stakeholders

- **Primary**: the developer running `sr submit` on a restacked stack, who should never have to reason about whether `-f` is safe today.
- **Secondary**: a collaborator or bot writing to the same PR branch (GitHub's "Update branch", suggested-change commits), whose work must not be silently destroyed.
- **Tertiary**: CI invoking submit non-interactively, which must fail loudly rather than guess.

## Constraints

- Per **ADR-0014**: the Push Record is the primary check and is **local-only**; content inspection is a fallback. A shared record would let one developer's push authorise another's force push over it.
- Per **ADR-0015**: Frozen is operation-dependent — never rebased and never pushed by an automatic operation, a **wall** for Restack (dependents are not rebased) and a **hole** for Submit (dependents are still pushed). Naming a frozen branch explicitly is not blocked.
- Per **ADR-0002**: `sr undo` restores the graph only. The rollback token introduced here is a **separate, local** artifact; extending undo to restore refs is explicitly out of scope (deferred to a `sr checkpoint` / `sr rollback` spec).
- Per **ADR-0003**: GitHub is reached by shelling to `gh`; no REST client.
- The local graph remains the source of truth (`restack.go:160`), which is what settles the diverged-GitHub-stack policy.
- Requires git ≥ 2.38 for `merge-tree --write-tree`. Development target is 2.48.
- `sr get`'s divergence handling is **not** refactored into a shared mechanism by this spec. Get mutates locally; submit publishes. Their non-interactive policies deliberately differ (see below) and unifying them would force one to adopt the other's blast radius.

## Solution

### Vocabulary

Defined in `codev/UBIQUITOUS_LANGUAGE.md`: **Ref Divergence**, **Content Divergence**, **Push Record**, **Preflight**, and the revised **Frozen** and **Submit**.

The distinction the whole design rests on: every restack produces **Ref Divergence** and it means nothing. Only **Content Divergence** — the remote holding changes local does not contain — justifies stopping.

### Classification (per branch, cheapest and most authoritative first)

Returns one of `NoPush` / `PushPlain` / `PushForce(leaseSHA)` / `Prompt(reason)`.

```
if remote ref missing            -> PushPlain (first push, empty-expect lease)
if remote == local               -> NoPush
if remote is ancestor of local   -> PushPlain      (plain fast-forward)
if local is ancestor of remote   -> Prompt("behind")
--- refs diverged ---
if PushRecord[remote/B] == remoteSHA -> PushForce(remoteSHA)          # Tier 0, SOUND
if no merge base                     -> Prompt("unrelated histories")
Tier 1: merge-tree --write-tree B remote/B
        nonzero exit                 -> Prompt("conflict")
        merged tree != B^{tree}      -> Prompt("remote has content you lack")
Tier 2: for each X in rev-list --right-only --cherry-pick B...remote/B    (merges INCLUDED)
        X is a root commit           -> Prompt("cannot replay root commit")
        count > cap                  -> Prompt("too much unmatched history")
        merge-tree --write-tree --merge-base=X^1 B X
        nonzero or tree != B^{tree}  -> Prompt("remote commit not contained: X")
-> PushForce(remoteSHA)
```

**Tier 0 is the only sound tier.** Content analysis is structurally blind to deletions: if a collaborator drops a commit and force-pushes, the remote becomes a strict *ancestor* of local, every content test says "safe", and git does not even require a force — the dropped commit is silently resurrected. Only "the remote moved off the SHA we left there" catches it.

Tiers 1–2 exist because a tier-0 miss is not evidence of conflict — GitHub itself writes to PR branches. A tier-0 failure therefore still runs 1–2. **Nothing may clear a tier-0 failure back to safe.**

`--no-merges` is deliberately **absent** from tier 2: a merge commit can carry content (an "evil merge" resolution). Merge commits are replayed against their first parent (`X^1`).

### Preflight

Publishes nothing. Order is bottom-up so a branch is classified in its final shape.

1. Fetch the remote; write a rollback token at `.stackr/rollback/<id>.json` (`{branch: pre-remediation SHA}`).
2. For each branch in the push set:
   - classify; on `Prompt`, present a flat menu:

     | option | effect |
     |---|---|
     | **Stop** | handle manually; nothing is pushed |
     | **Merge — squash** | `git merge --squash <remote>/B` + commit: their changes as one new commit. Linear, one conflict resolution. |
     | **Merge — cherry-pick** | replay their commits individually; preserves granularity and authorship |
     | **Overwrite local** | reset to `<remote>/B`, discarding our commits on this branch |
     | **Overwrite remote** | push ours anyway; their commits are lost |

   - if local changed, restack the branch **and its full upstack immediately** — including branches outside the push set — then reload the graph. The restack stops at any frozen branch (ADR-0015), leaving that lineage unrebased.
   - if a restack is blocked (dirty worktree, conflict), prompt **Stop | Skip**; Skip drops that branch **and its entire lineage** from the push set.
3. A conflict during either merge remediation is `--abort`ed and degrades into **Stop** — no conflict-resumption machinery is needed.
4. On Stop: nothing is pushed, remediations already made are **kept**, and the rollback id is printed. Re-running `sr submit` is idempotent because classification is re-derived, never remembered.

Merge-squash and cherry-pick were chosen over `get`'s merge commit because `rebase --onto` is stackr's entire maintenance model (`restack.go:229`); a merge commit on a stacked branch is dropped and linearised by the next restack.

### Push

1. Re-fetch and re-check tier 0 for every branch. If anything moved, **abandon the push phase** and return to preflight for the affected branches — human decision time sits between the first fetch and the push, and that window is unbounded.
2. Push bottom-up: `git push --force-with-lease=refs/heads/<B>:<inspected SHA> <remote> <B>:refs/heads/<B>`. First push uses the empty-expect form `refs/heads/<B>:`, which also closes the create race. `--force-if-includes` is a documented no-op alongside an explicit expect value and is omitted.
3. On lease rejection mid-way: stop, and report exactly what was pushed and what was not. **"No partial update" is a strong default, not a guarantee** — you cannot win a race against an active writer, only refuse to publish a half-state.

### Reconcile

Record-keeping fires when the thing it records happens.

- **Per branch, at push time**: update the Push Record; retarget the PR base. Bottom-up order guarantees a parent exists on the remote before its child's base is retargeted.
- **Per segment, once fully pushed**: register or extend the GitHub stack. A stack needs ≥2 PRs and the whole linear segment, which is why this one cannot be per-branch.
- **Diverged GitHub stack**: rebuild to mirror local, because the local graph is the source of truth everywhere else. Guards: clear `prInfo.StackNumber` **before** `ghUnstack` so a mid-failure leaves a recoverable ungrouped state rather than a pointer to a dead stack; gate the destructive rebuild on `c.Interactive` so CI warns instead of regrouping unattended.

### Flags

| flag | meaning |
|---|---|
| `--force`, `-f` | choose **Overwrite remote** on Content Divergence without prompting. Still uses the pinned lease. **Never skips preflight.** |
| `--no-force` | never force-push; fail when a non-fast-forward push would be required. Mutually exclusive with `--force`. |
| `--dry-run` | classify and report only. No rollback token, no remediation, no restack, no push, no PR/base/stack writes. |
| `--update-only`, `-u` | *(currently dead)* restrict the push set to branches with a PR number or an existing remote ref. |
| `--ai` | unchanged, and does **not** preflight in the parent: the spawned agent calls plain `sr submit`, which preflights. One code path. |

Unchanged: `--draft`, `--stack`, `--title`, `--body`, `--body-file`, `--aiprepare`.

### Non-interactive / CI

- Content Divergence → **fail before pushing anything**. This deliberately differs from `get`, which skips: `get` mutates only locally, whereas a skipped mid-stack branch in submit strands every PR above it on a base that was never updated.
- Blocked restack → **fail before pushing anything**.
- A non-interactive submit **may** force-push when preflight proved it lossless — that is the entire point of the feature.
- Requires real TTY detection, since `--interactive` currently defaults `true` without one.

### Folded-in fixes

Pre-existing defects that this flow would otherwise inherit:

- `TryPushMeta` on every submit path, not only `submitStack`.
- `mergeBranchPR` becomes **field-level** so concurrent clones stop dropping each other's `BaseBranch` / `StackNumber`.
- `resolveDivergedStack` implemented; signature widened to take `baseByPR`, `prInfo` and interactivity so it can clear `StackNumber` and retarget bases.
- `--update-only` implemented.
- Frozen honoured in `restack.go` as a **wall** (ADR-0015), which also changes `sr restack` and `sr sync`. Submit keeps treating it as a **hole**, with one addition: a frozen branch that was never pushed leaves its dependent's PR base pointing at a ref that does not exist, so submit reports that against the frozen branch instead of letting GitHub reject the dependent.

### Stale-state rule

`Restack` reads and writes its **own** graph instance (`restack.go:32`, `:268`), so a caller-held `g` goes stale the moment preflight restacks. A stale `g` would feed `pushBranch` an old parent (`submit.go:392`, `:414`), corrupting both PR base retargeting (`submit.go:474`) and stack segmentation (`ghstack.go:186`).

Rule: preflight owns one graph and **reloads `g` and `prInfo` after every restack**. Branch **names** are carried across iterations; SHAs are not, except the inspected remote SHA used for the lease — which is re-validated after the second fetch. Push-set membership is recomputed each round, since remediation can change graph shape and frozen state.

## Success Criteria

- [ ] Submitting a freshly restacked stack that nobody else touched pushes silently, with no `-f` and no prompts.
- [ ] A collaborator's new commit on a submitted branch is detected and prompts; choosing **Merge — squash** produces one new commit and a clean force push.
- [ ] A collaborator's **revert** on a submitted branch is detected (tier 2), not silently resurrected.
- [ ] A collaborator **dropping a commit** and force-pushing is detected (tier 0), despite the remote being a strict ancestor of local.
- [ ] `sr absorb`, `sr split`, `sr squash`, `sr reorder`, and a restack over a trunk edit *adjacent* to a branch hunk all classify as lossless and push without prompting.
- [ ] An unqualified `--force-with-lease` no longer appears in any submit path; every force push pins the inspected SHA.
- [ ] A push rejected by a stale lease stops the run and reports exactly what was and wasn't pushed.
- [ ] Stopping mid-preflight publishes nothing, keeps prior remediations, and prints a rollback id; re-running submit resumes cleanly.
- [ ] A frozen mid-stack branch is neither rebased nor pushed; `sr restack` stops at it (dependents untouched) while `sr submit` steps over it (dependents still pushed), and both name the exclusion in their output.
- [ ] Naming a frozen branch explicitly (`sr restack --branch <frozen>`, submitting while on it) is not blocked.
- [ ] A dependent of a never-pushed frozen branch reports the missing base against the frozen branch, not as a raw GitHub error.
- [ ] Non-interactive submit fails on Content Divergence and on a blocked restack, and still force-pushes when preflight proved it lossless.
- [ ] `--dry-run` mutates nothing — no rollback token, no remediation, no restack, no writes.
- [ ] `--update-only` restricts the push set instead of being ignored.
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` green.
