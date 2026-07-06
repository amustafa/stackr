# Path-identical worktree mount with shared .git for sandboxes

`sr sandbox` (Spec 5) runs Claude inside a Docker container that operates on a git worktree. A git worktree is not self-contained: its working directory holds a `.git` **file** pointing at `<repo>/.git/worktrees/<name>`, and all objects live in the shared `<repo>/.git/objects`. For `git` (and `sr`) to function inside the container, both the worktree and the main `.git` must be reachable at the **same absolute paths** as on the host.

## Decision

Bind-mount the worktree and the main repo's `.git` at their **real host paths**, set the container's working directory to the worktree path, and share `.git` **read-write**.

This has a decisive second benefit. Claude Code files each session's JSONL under `~/.claude/projects/<slug>`, where `<slug>` is the **slugified absolute working-directory path** (`/`→`-`) — empirically confirmed against `~/.claude/projects`, which already contains per-worktree entries like `-home-amustafa-workspace-ftron--worktrees-am-...`. It is a path slug, **not** a content hash. Because the container's `cwd` equals the host worktree path (and `~/.claude` is mounted at the same path), the container produces the **same slug as the host** — session logs land in the shared history with zero extra machinery.

Because the slug derives from the **worktree** path, sandbox sessions are keyed to that worktree, so they are resumed from the worktree on the host (`cd .worktrees/<branch> && claude --resume`, or `sr sandbox attach`) — not from the repo root. The exact-string match means the mount path must be **canonicalized** (`filepath.EvalSymlinks`) so the container's slug matches the host's byte-for-byte.

## Alternatives considered

- **Isolated local clone** — give the container its own independent `.git` (true git isolation: it cannot touch the main repo's objects/refs). Rejected: heavier, requires reconciling commits back, and contradicts the "always a worktree" requirement. Path-hash continuity would still need path identity.
- **Mount `.git` read-only** — for extra safety. Rejected: committing must write objects, so a read-only `.git` breaks the core workflow.

## Consequences

- The sandbox's **working tree** is isolated (its own branch checkout), but the **git object store and refs are shared and writable**. A misbehaving agent could in principle write objects/refs into the main repo's `.git`. This is accepted: the sandbox boundary is the host filesystem/process space (enforced by the container) and network policy — not git internals. The threat model is "agent does something destructive to the host," which the container contains, not "agent corrupts git history."
- Mounts must use real host paths, so the container is inherently host-specific (not portable to a machine with a different repo location) — acceptable, since sandboxes are launched from the repo on the developer's machine.
