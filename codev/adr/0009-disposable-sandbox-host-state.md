# Disposable sandbox containers with host-side durable state

`sr sandbox` (Spec 5) needs two things that seem to pull in opposite directions: the developer must be able to **add missing context and restart** without losing Claude's progress, and the containers must be **cheap** ("the only difference between containers is the context volumes and the initial commands").

## Decision

Treat the container as **fully disposable**. All durable state lives on the **host** via bind mounts — the worktree, and the Claude session logs under `~/.claude/projects`. The container holds no state worth keeping.

- Restart/resume = throw the container away, start a fresh one from the same base image + mounts, and run `claude --continue` — which resumes the exact session because the JSONL is host-side and the project hash matches (ADR-0008).
- "Add context as needed" is a first-class consequence: you **cannot add a bind mount to a running container**, so adding context (a sibling repo, a docs dir, an env var) inherently requires recreating it — and a disposable model makes that a non-event.
- `sr sandbox` persists a small **manifest** (`.git/.stackr/sandboxes/<branch>.json`: mounts, launch command, session id) so a restart reconstructs the container identically, plus whatever was just added, and auto-resumes.
- In-container tool installs are **ephemeral by design**: the toolchain belongs in the base image (or the per-project layer), project dependencies live in the mounted worktree, and package caches are bind-mounted to stay warm.

Two levels of teardown reflect this: `sr sandbox stop` keeps the container so `docker start` resumes the **live** in-memory zellij session; `sr sandbox rm` destroys the container and drops to cold `--continue` resume. Either way, **Claude progress is never lost.**

## Alternatives considered

- **Long-lived `docker create` + start/stop container** — lets in-container installs persist. Rejected: you still cannot add new mounts without recreating it, in-container state drifts from the "only volumes differ" model, and it invites treating the container as a pet.

## Consequences

- The base image (and optional per-project layer) must carry the real toolchain; a minimal base that forces per-run installs would re-download on every disposable start.
- Anything the agent installs ad hoc inside the container is lost on `rm` — acceptable and intentional; durable needs are declared in the image or mounted from the host.
- Manifest + host-side logs are the single source of truth for reconstructing a sandbox, so they must stay outside the agent's easily-mutated worktree (hence `.git/.stackr/`).
