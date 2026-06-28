# Undo via graph snapshots, not git history

Stackr's undo captures a JSON snapshot of the entire branch graph before each mutating operation, stored locally under `.git/.stackr/undo/snapshots/`. Undo restores the graph to the previous snapshot (LIFO stack).

This was chosen over git-based undo (e.g., reflog or reverse commits) because stackr operations are metadata mutations — they change which branches depend on which — not git commits. A graph snapshot is a few KB of JSON and captures exactly the state that matters. Git's reflog tracks commit-level changes, which is orthogonal.
