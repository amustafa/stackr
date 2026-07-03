# lessons-critical.md — Always-On Engineering Wisdom (HOT tier)

<!-- HOT tier: capped lessons + a bounded map of lessons-learned.md. Always injected into
every porch phase prompt and into CLAUDE.md/AGENTS.md. CAP: <=10 lessons, <=12 map topics,
<=35 lines. To add a lesson, DEMOTE a weaker one into lessons-learned.md (displacement).
MAINTAIN polices the cap and keeps the map in sync with lessons-learned.md's sections.
STARTER: a few universal lessons are seeded; add your project's as you learn them. -->

## Critical lessons (consult before deciding)
- Check for existing work (PRs, git history) before building from scratch.
- "It compiled" / "tests pass" is not "it works" — verify the real user path before calling it done.
- When stuck (2 failed hypotheses or ~30 min), get an outside perspective instead of guessing.
- Stash operations must be paired: push before, pop after. A stash without a pop silently loses user changes.
- Go `append(returned_slice, more...)` may mutate the returned slice's backing array. Use `make` + `append` when the result outlives the call.
- Plan phases that split cmd and engine layers in Go are unrealistic — signature changes force both to update atomically.

## Map of lessons-learned.md (consult when…)
- <Top-level section> — consult when <situation>.
- <List your lessons-learned.md's top-level sections here; keep <=12, top-level only.>
