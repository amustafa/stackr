# stackr

This repo uses stackr (`sr`) for stacked-branch development. Prefer `sr` over
raw git for branch, commit, and PR operations — raw git desyncs the stack graph.

**Stack sequential work.** When a change builds on earlier work, don't pile it
all into one branch — stack it: put each reviewable step on its own branch on top
of the previous one, so every branch builds on its parent and reviews on its own.

**Travel the stack** with `sr up` and `sr down` to move between branches
(`sr top` / `sr bottom` jump to the ends); `sr log` shows the tree.

See the `stackr` skill for the full command set and workflow.
