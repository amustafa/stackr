# Frozen is operation-dependent: a wall for Restack, a hole for Submit

**Frozen** was documented as "excluded from automatic operations (restack, submit)" but implemented as submit-only: `restack.go` never read the flag, so frozen branches were rebased anyway and merely skipped at push time. **Preflight** forces the question, because it restacks the full **Upstack** after every remediation and would silently move frozen branches.

The tempting fix is to pick one shape — frozen excludes only itself (a *hole*), or frozen excludes its whole upstack (a *wall*) — and apply it everywhere. Both are wrong somewhere, because the two operations relate a branch to its dependents differently.

## Decision

A frozen branch is never rebased and never pushed by an automatic operation. Whether that exclusion **spreads to its upstack** is decided per operation, by one test:

> Does this operation's effect on a dependent depend on the frozen branch having moved?

- **Restack → wall.** Restacking stops at the frozen branch. Rebasing its dependents is meaningless when the base they would rebase onto was deliberately left in place; the operation's whole purpose is to replay them onto a *new* parent tip that, by definition, will not exist.
- **Submit → hole.** Submit skips only the frozen branch. Branches above and below it are still pushed, because each carries changes of its own that are worth publishing whether or not the frozen branch moved.

Explicitly naming a frozen branch as the subject of an operation is **not** an automatic operation and is not blocked — `sr restack --branch <frozen>` and submitting while checked out on a frozen branch both work. Freezing withdraws a branch from operations that sweep over it, not from the user's direct instruction.

That exemption covers a branch's **commits**, not its **position**. `sr restack --branch <frozen>` replays commits onto the parent the branch already has; the parent pointer is untouched. An operation whose subject is a frozen branch and whose effect is to *repoint* that pointer is blocked even when named explicitly, because the parent pointer is precisely what freezing pins. So `sr move --source <frozen>` is an error, while `sr restack --branch <frozen>` is not. Being the *target* of a move is never blocked.

Freezing is an *intention*, not a *failure*, so it is always skipped and reported, never an error. This distinguishes it from `RestackOpts.SkipBlocked`, which governs genuine failures (dirty worktree, rebase conflict) and may legitimately halt when false.

## Alternatives considered

- **Hole everywhere** — what the code did for submit and, by omission, for restack. Rejected for restack: it would rebase a dependent onto a parent tip that was deliberately frozen, which either does nothing or replays commits onto a stale base.
- **Wall everywhere** — the literal reading of the old glossary text. Rejected for submit: freezing one mid-stack branch would silently withhold every PR above it, which is a large, invisible consequence for a flag whose stated meaning is only "leave this branch alone".

## Consequences

- Freezing a mid-stack branch strands its upstack on a stale parent locally (restack will not move them) while still allowing their PRs to be updated. That combination is intentional but needs saying out loud in the output, or it reads as a bug.
- This changes `sr restack` and therefore `sr sync`, not only `sr submit` — any caller of `Restack` now honours the flag.
- Submit's hole has one sharp edge: if a frozen branch has **never been pushed**, a dependent's PR base names a ref that does not exist on the remote, and PR creation against it fails. Submit must detect that case and report it against the frozen branch, rather than surfacing GitHub's error against the dependent.
- Reporting matters more than before: an operation that silently treats one branch differently from its neighbours is indistinguishable from a bug, so both the frozen branch and the shape of its exclusion must be named in the output.
- The commits/position split means any future operation must be classified before it can honour an explicit-naming exemption. The question to ask is whether the operation changes the branch's parent pointer, not whether it rewrites history — **Move** does both, and the pointer is what decides it.
