# Sandbox attention hooks via --settings, not a global install

Status: accepted

The sandbox publishes its interaction state (**Sandbox Status**) from Claude Code hooks. Those hooks must exist for sandbox sessions but not perturb the developer's normal host sessions — and the developer's full `~/.claude` is bind-mounted read-write into the container (ADR-0008), so any global mutation is durable and shared.

## Decision

Do **not** install hooks into the mounted `~/.claude/settings.json`. Instead, generate a sandbox-only settings JSON containing the attention hooks and pass it at launch with `claude --settings <file>`. Because `--settings` layers *additively* over the auto-discovered `~/.claude` config, the developer's own hooks/settings still load, while the attention hooks exist only for sandbox invocations. `SR_SANDBOX=<branch>` is still exported so the hook script knows which branch's status file to write, but it is a hint, not an inertness gate.

## Considered options

- **Env-gated global install** into `~/.claude/settings.json` — rejected: mutates shared global settings, risks colliding with the user's hooks, needs idempotent install/uninstall, and relies on gating to stay inert on the host.
- **Project `.claude/settings.json` in the worktree** — rejected: the worktree is a checkout of the repo, so hooks would be committable and would also affect host sessions in that repo.

## Consequences

- The sandbox's main Claude session must **not** run with `--bare` (which skips hooks); `--bare` remains fine for credential-free PR-suggestion generation, which needs no attention hooks.
- Nothing to uninstall; removing a sandbox leaves no trace in `~/.claude`.
- The hook script locates the status file via the shared git dir (`git rev-parse --git-common-dir`) so it resolves to `<main .git>/.stackr/`, which is mounted and visible to the host.
