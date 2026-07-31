# Force-push safety by record-keeping, not content inspection

Because **Restack** rewrites SHAs, nearly every **Submit** of a stacked branch needs a force push. Blind forcing destroys a collaborator's work; refusing to force makes the tool unusable. So submit has to answer, per branch: *would force-pushing local over the remote lose anything?*

The obvious framing is to answer it by **inspecting the two branches** — compare commits, or compare content. Both fail, and the second failure is the one that matters.

## Decision

Answer it primarily from a **Push Record**: the SHA *this clone* last left on the branch's remote ref. If the remote still equals that SHA, everything on it is ours by construction, and force-pushing is lossless — no inference required. Content inspection is retained only as a *fallback* for when the record is absent or stale (fresh clone, another machine, someone else pushed).

The Push Record is **local-only**, stored beside rebase/undo state rather than in the shared metadata at `refs/stackr/data`. Its meaning is "*we* put that there", so a record travelling between clones would let one developer's push authorise another developer's force push over it — converting the only sound check into the most dangerous one.

Fallback tiers, in order, each failing conservative (any unexpected exit, unrelated histories, or missing merge base prompts):

1. **Push Record matches** — provably ours, force.
2. **Whole-branch containment** — `git merge-tree --write-tree B origin/B`; if the merged tree equals local's tree, the remote contributes nothing.
3. **Per-commit replay** — for each remote commit with no patch-equivalent locally, replay it alone onto local; a no-op means it is already contained.

A "review" affordance may explain a Push Record mismatch but may never clear it back to safe.

## Alternatives considered

- **Ancestry (`HasDiverged`)** — true after every restack, so it would prompt on every submit of every branch. It measures **Ref Divergence**, which carries no information here.
- **Patch-id equivalence (`git cherry`)** — false-positives on `sr absorb`, `sr split`, and even plain restacks: `git patch-id` hashes context lines, so a trunk edit merely *adjacent* to a branch hunk changes the id. It also goes blind whenever N commits collapse into 1, which is why `SquashMergedUpstream` had to exist alongside `AllCommitsUpstream`.
- **Content inspection as the primary check** — rejected because two pairs of situations are provably indistinguishable from ref contents alone: "I amended my own commit" vs "a colleague amended it", and "I deliberately dropped a commit" vs "a colleague pushed one". The distinguishing fact is *who wrote it*, which is not in the trees. The collaborator-deleted-a-commit case is the sharpest: the remote becomes a strict ancestor of local, so every content test says "safe" and git does not even require a force — the branch is silently resurrected. Only the Push Record catches it.

## Consequences

- A clone that has never pushed a branch falls through to heuristics, which is correct: it has no grounds to claim the remote is its own work.
- The record must be garbage-collected wherever the graph drops a branch, or a recreated branch name inherits a stale claim.
- Every operation that settles local against remote writes it — including "overwrite local with remote", which is not a push.
- Restacking cannot perturb tier 1, since the record concerns the remote and rewriting local touches neither side of the comparison. This is what lets **Preflight** restack freely mid-loop.
- `--force-with-lease` must be pinned to the inspected SHA (`--force-with-lease=refs/heads/<B>:<sha>`). The unqualified form compares against the remote-tracking ref, which the preflight fetch has just updated, making the lease vacuous — verified empirically to destroy a collaborator's commit.
