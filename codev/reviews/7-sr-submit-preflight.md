# Review: sr submit preflight

- **Specification**: codev/specs/7-sr-submit-preflight.md
- **Plan**: codev/plans/7-sr-submit-preflight.md
- **ADRs produced**: 0014 (Push Record, not content inspection), 0015 (Frozen is operation-dependent)
- **Branch**: `submit-preflight`
- **Completed**: 2026-07-31

## What shipped

`sr submit` now runs a preflight across every branch it is about to publish, settles any **Content Divergence** with the user, and only then pushes — bottom-up, with each lease pinned to the exact commit that was inspected. The common case (a restacked stack nobody else touched) is silent and needs no `-f`.

All phases in the plan landed. Two extras were pulled in: `sr rollback`, because preflight printed a rollback id for a command that did not exist; and a fix to `GitCommonDir`, described below.

## Lessons

### The predicate was undecidable, and we nearly shipped the wrong one twice

The feature was framed as "check if the remote has code we don't". Two obvious readings both fail:

- **Ancestry** is true after every restack, so it would prompt on every submit of every branch.
- **Patch-id equivalence** breaks on `sr absorb`, `sr split`, and — worse — on plain restacks, because `git patch-id` hashes context lines, so a trunk edit merely *adjacent* to a branch hunk changes the id.

A third reading, whole-branch content containment via `merge-tree`, survived longer but has a fatal blind spot: a remote **revert** cancels against the commit it undoes in the net diff, so force-pushing silently resurrects reverted code.

The real lesson is that two pairs of situations are **provably indistinguishable from ref contents alone** — "I amended my own commit" vs "a colleague amended it", and "I deliberately dropped a commit" vs "a colleague pushed one". The distinguishing fact is *who wrote it*, which is not in the trees. Once that was clear the design inverted: stop inferring, start recording. Content analysis became a fallback for the case where the record is missing.

**Generalisable**: when a predicate keeps failing in new ways, check whether the question is answerable from the available evidence at all. Sometimes the fix is to record the answer at the moment you know it, not to infer it better later.

### `--force-with-lease` is not the safety net it looks like

The unqualified form compares the remote against the **remote-tracking ref**, which any fetch has just updated. Submit never fetched, which is the only reason it was safe. Adding the fetch this feature needs would have silently made the lease vacuous — verified empirically against a bare remote, where the bare lease **succeeded in destroying a collaborator's commit**. Every force push must pin `--force-with-lease=refs/heads/<B>:<the SHA you inspected>`.

**Generalisable**: a safety mechanism whose correctness depends on a property of the surrounding code (here: "we never fetch") is a trap for the next change. It was not documented as a precondition anywhere.

### Empirical verification beat reasoning, twice

Both the revert blind spot and the vacuous-lease behaviour were found by building real repositories and running real git, not by reading documentation. The adjacent-context patch-id failure would never have been guessed. Tests for this feature are correspondingly built on real repos and a real bare remote throughout — there is no value in a mocked git here, because every interesting case is a subtlety of git's actual behaviour.

### A relative path leaked into a soundness guarantee

`git rev-parse --git-common-dir` returns a **relative** `.git`, so the local store rooted itself relative to the *process* working directory. Running `sr` from a subdirectory scattered undo snapshots, rebase state — and would have scattered Push Records — into a stray `<subdir>/.git/.stackr/`. A Push Record that cannot be found silently downgrades the one sound tier of the ladder.

It surfaced as *flaky tests*: every engine test was sharing `internal/engine/.git/.stackr/`. Chasing the flake found a pre-existing production bug.

**Generalisable**: flaky tests that share state are worth chasing to the root rather than isolating, because the shared state is often shared in production too.

### Cross-model review found two spec violations of our own making

A gpt-5.5 review of the finished diff produced seven findings, all legitimate. Two mattered:

1. A Push Record **mismatch** fell through to the content tiers, which could clear it back to "force". The spec said both "a tier-0 failure still runs 1–2" and "nothing may clear a tier-0 failure back to safe" — a contradiction nobody noticed while writing it. The fix was to give tier 0 three outcomes (match / mismatch / absent) rather than two, and the spec and ADR-0014 were corrected to match.
2. `--no-force` did not stop the *lossless* force push, only the contested one. The flag is a contract about the operation, not about the risk.

**Generalisable**: a spec sentence that reads well can still be self-contradictory. An independent reader with no memory of the discussion is much more likely to notice than the author.

## Deviations from the plan

- **`sr rollback` was implemented**, though the plan scoped it out. Preflight printing "run `sr rollback <id>`" for a nonexistent command was a broken promise. Only the restore half shipped; `sr checkpoint` remains deferred.
- **Frozen's semantics changed mid-flight.** ADR-0015 originally made Frozen a wall everywhere. It became operation-dependent — a wall for Restack, a hole for Submit — decided by whether the operation's effect on a dependent depends on the frozen branch having moved. The ADR, spec, plan, and glossary were all revised.
- **`GitCommonDir` / `GitDir` now return absolute paths** — unplanned, and a prerequisite for the Push Record being findable at all.

## Known gaps, deliberately left

- A configured `merge=ours` driver can in principle make tier 1 report "safe" for a genuinely new remote hunk. It requires the user's own git config, tier 2 catches it in practice, and tier 0 makes it moot for the common case.
- Amending the same hunk a remote commit introduced is a **known false positive** — indistinguishable from a collaborator amending it. Tier 0 cures it; there is a test pinning both halves of this behaviour.
- "No partial update" is a strong default, not a guarantee. You cannot win a race against an active writer, only refuse to publish a half-state.
- The "review" affordance that would explain a tier-0 mismatch is not built.

## Verification

- 277 tests green; `go build`, `go vet` clean.
- End-to-end against a real repo with a bare remote and a second clone: restacked stack submits silently with no `-f`; a collaborator commit stops it; non-interactive fails with exit 1 and publishes nothing (verified by checking the *lower* branch's remote tip was untouched); `--force` resolves and publishes; an immediate re-submit is a no-op, proving the Push Record round-trips; frozen behaves as a hole for submit and a wall for restack, both reported by name.
