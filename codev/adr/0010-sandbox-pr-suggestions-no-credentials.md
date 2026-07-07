# Sandboxes carry no credentials; PR suggestions bridge to host submit

`sr sandbox` (Spec 5) runs an agent with `--dangerously-skip-permissions`. Mounting the full `~/.claude` into that box is an accepted risk because those secrets are revocable and the container isolates the host. GitHub credentials are different: a leaked SSH key or long-lived token is **durable damage that outlives the disposable container**, and it's the one class of secret whose exfiltration this design most wants to prevent.

## Decision

The sandbox is given **no GitHub credentials**. It cannot `git push`, create PRs, or run any authenticated remote operation. All remote mutation happens on the **host**, through the existing `sr submit` flow.

To keep the end-to-end loop intact, the sandbox instead records a **PR Suggestion** — the proposed PR title/body — as a reserved **Branch Context** entry (key `pr`), using the existing `sr context set` mechanism. Because the worktree's `.git` is shared (ADR-0008), both the agent's commits and the branch context (in `refs/stackr/data`) are already in the shared git dir; no new file or command is needed. Host-side, `sr submit` reads the reserved entry and uses it as the PR title/body directly — no AI regeneration required — offering to edit before creating/updating the PR.

This reuses stackr's existing machinery end-to-end: `sr context set` to record, and `PrepareAI`/`submit`, which already read branch **Description** + **Context** to build a PR (ADR-0004). The sandbox is effectively a credential-free PR-prep producer whose output rides the branch graph across the container/host boundary.

## Alternatives considered

- **Mount `~/.config/gh` + gh as git credential helper** — one revocable token covers push and PR. Rejected as the default: still exposes a live token to the skip-permissions agent. Left available as an opt-in for users who want the agent to push directly.
- **SSH-agent forwarding** — keys never enter the container. Rejected as default: unnecessary for HTTPS remotes and still lets the agent push under the developer's identity.
- **Mount `~/.ssh`** — exposes durable private keys. Rejected outright.

## Consequences

- The agent cannot verify remote-side outcomes (CI, PR checks) from inside the sandbox — those are host-side follow-ups.
- `sr submit` grows a path to read the reserved `pr` **Branch Context** entry and use it as the PR title/body (offer edit → push → create/update).
- The suggestion is **Branch Context** (Shared Metadata), so it is lost on squash — the sandbox must set it after any squash. The sandbox has no credentials, so it never pushes the metadata; the host does at submit time.
- A reserved context key (`pr`) is now special-cased by submit — document it so users don't collide with it.
- Direct-push remains possible for users who explicitly opt into mounting credentials, but it is never the default.
