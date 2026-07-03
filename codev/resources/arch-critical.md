# arch-critical.md — Always-On System-Shape Facts (HOT tier)

<!-- HOT tier: capped facts + a bounded map of arch.md. Always injected into every porch
phase prompt and into CLAUDE.md/AGENTS.md. CAP: <=10 facts, <=12 map topics, <=35 lines.
To add a fact, DEMOTE a weaker one into arch.md (displacement). MAINTAIN polices the cap
and keeps the map in sync with arch.md's top-level sections.
STARTER: replace the examples below with YOUR project's facts and arch.md sections. -->

## Critical facts (consult before deciding)
- Shared metadata lives in `refs/stackr/data` (ADR-0001); local-only ephemeral data (rebase state, get state, undo) lives in `.git/.stackr/`.
- New persistent state requires three-layer update: `Store` (impl) → `RefStore` (delegation) → `Backend` (interface). Missing any layer causes compile errors.
- Navigation uses the `NavigateResult` + `handleNavigateResult` + `__sr_cd:` protocol — subprocess prints a marker, shell wrapper intercepts and CDs.
- `sr get` pulls from remote (no rebasing); `sr restack` does local rebasing; `sr sync` combines both. Don't conflate.

## Map of arch.md (consult when…)
- <Top-level arch.md section> — consult when <situation>.
- <Top-level arch.md section> — consult when <situation>.
- <List your arch.md's top-level sections here; keep <=12, top-level only.>
