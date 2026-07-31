# Frozen is a wall, not a hole

**Frozen** was documented as "excluded from automatic operations (restack, submit)" but implemented as submit-only: `restack.go` never read the flag, so frozen branches were rebased anyway and merely skipped at push time. **Preflight** forces the question, because it restacks the full **Upstack** after every remediation and would silently move frozen branches.

The half-implemented state was also incoherent on its own terms. A frozen branch that is rebased but never pushed leaves its dependents with a PR base pointing at a remote branch stackr declined to update — and if that branch was never pushed at all, PR creation against it simply fails.

## Decision

A frozen branch is a wall. It is never rebased and never pushed, and **its entire upstack is excluded along with it** — from restack and from the push set alike.

Frozen is an *intention*, not a *failure*, so it is always skipped and reported, never an error. This distinguishes it from `RestackOpts.SkipBlocked`, which governs genuine failures (dirty worktree, rebase conflict) and may legitimately halt when false.

Mechanically this reuses the existing lineage-exclusion machinery: a frozen branch marks itself in `restackBranches`' `blocked` map and its dependents inherit the exclusion, the same rule already applied to a branch whose restack conflicts.

## Alternatives considered

- **Frozen means "don't publish"** (keep rebasing it) — what the code did. Rejected: it leaves the dangling-base problem unfixed, and it contradicts the documented meaning without saying so.
- **Frozen means "don't publish, and nothing above it publishes either"**, but still rebase it — coherent for the remote, and it keeps the local stack maintained so freezing one branch doesn't paralyse the branches above it. Rejected in favour of the literal reading: a branch the user froze should not be rewritten under them, and a partial freeze is harder to explain than a total one.

## Consequences

- Freezing a mid-stack branch strands everything above it on a stale parent locally. That is the accepted cost of the literal reading, and it makes freezing a mid-stack branch a deliberate, visible act rather than a quiet one.
- This changes `sr restack` and `sr sync`, not only `sr submit` — any caller of `Restack` now honours the flag.
- Reporting matters more than before: a frozen wall silently excluding five branches from a submit would be indistinguishable from a bug, so the excluded lineage must be named in the output.
