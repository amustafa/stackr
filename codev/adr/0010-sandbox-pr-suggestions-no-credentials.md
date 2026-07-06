# Sandboxes carry no credentials; PR suggestions bridge to host submit

`sr sandbox` (Spec 5) runs an agent with `--dangerously-skip-permissions`. Mounting the full `~/.claude` into that box is an accepted risk because those secrets are revocable and the container isolates the host. GitHub credentials are different: a leaked SSH key or long-lived token is **durable damage that outlives the disposable container**, and it's the one class of secret whose exfiltration this design most wants to prevent.

## Decision

The sandbox is given **no GitHub credentials**. It cannot `git push`, create PRs, or run any authenticated remote operation. All remote mutation happens on the **host**, through the existing `sr submit` flow.

To keep the end-to-end loop intact, the sandbox instead **deposits a PR Suggestion** — the generated PR title and body — as **Local Data** at `.git/.stackr/pr-suggestions/<branch>.json`. Because the worktree's `.git` is shared (ADR-0008), the agent's commits are already in the object store on the branch; the suggestion is the only extra artifact needed. Host-side, `sr submit` detects a deposited suggestion for the branch and offers to push the branch and create/update the PR using it — no AI required on the host, since the work is already prepared. The suggestion is cleared once the PR is created.

This reuses stackr's existing submit machinery (ADR-0004): the sandbox is effectively a persisted, credential-free `--aiprepare` producer whose output survives the container/host boundary.

## Alternatives considered

- **Mount `~/.config/gh` + gh as git credential helper** — one revocable token covers push and PR. Rejected as the default: still exposes a live token to the skip-permissions agent. Left available as an opt-in for users who want the agent to push directly.
- **SSH-agent forwarding** — keys never enter the container. Rejected as default: unnecessary for HTTPS remotes and still lets the agent push under the developer's identity.
- **Mount `~/.ssh`** — exposes durable private keys. Rejected outright.

## Consequences

- The agent cannot verify remote-side outcomes (CI, PR checks) from inside the sandbox — those are host-side follow-ups.
- `sr submit` grows a path to consume a deposited **PR Suggestion** (detect, confirm, push, create/update, clear).
- The suggestion is **Local Data** — it does not travel to the remote until `sr submit` acts on it, so nothing half-baked is pushed.
- Direct-push remains possible for users who explicitly opt into mounting credentials, but it is never the default.
