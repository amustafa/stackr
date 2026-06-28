# Shared metadata stored in git custom refs, not filesystem

Stackr stores its shared metadata (branch graph, config, PR info) as git objects behind `refs/stackr/data` rather than in a `.stackr/` directory. This means metadata can travel with `git push`/`git fetch` via refspecs, enabling team collaboration without a separate sync mechanism. Local-only ephemeral data (undo snapshots, rebase state) stays on the filesystem under `.git/.stackr/` because it's per-machine and shouldn't be shared.

**Considered options:**
- **Filesystem only** (simpler, but local-only — collaborators can't see your stack structure)
- **Separate sync service** (powerful, but adds infrastructure and a dependency)
- **Git refs** (chosen — piggybacks on git's existing transport, merge, and GC mechanisms)

**Consequences:** The RefStore needs CAS-based writes and three-way merge logic for concurrent updates. Worth the complexity because it eliminates external dependencies entirely.
