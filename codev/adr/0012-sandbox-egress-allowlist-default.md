# Sandbox network defaults to an egress allowlist; blocked domains are requested

Status: accepted

`claude --help` recommends `--dangerously-skip-permissions` *"only for sandboxes with no internet access,"* and the sandbox deliberately mounts the developer's full `~/.claude` (credentials included, ADR-0008). Full network plus mounted credentials plus skip-permissions is the exact exfiltration scenario that warning is about.

## Decision

The sandbox defaults to a **firewall egress allowlist** (implemented via an `iptables`/`ipset` init step; needs `--cap-add=NET_ADMIN`). The allowlist ships with the domains the normal workflow needs — the Anthropic API, GitHub, and common package registries (e.g. `proxy.golang.org`, `sum.golang.org`, `registry.npmjs.org`, PyPI) — and is a **portable** config field so a repo can extend it.

The agent is told, in its initial instructions, that it runs behind an egress firewall: **if it needs a blocked domain, it must request that the domain be added to the allowlist** rather than assuming network failure is transient. That request surfaces through the normal attention channel (**Sandbox Status** → awaiting-input). The developer adds the domain to the allowlist config and the sandbox is relaunched — which is just the existing "add context, then restart and resume" flow (ADR-0009), since an allowlist change requires a fresh container anyway.

A `--network full` opt-out remains for cases where the allowlist is genuinely too restrictive and the user accepts the risk.

## Considered options

- **Full network in v1** — rejected as the default: contradicts vendor guidance and maximizes the exfil target given mounted credentials. Kept as an explicit opt-out (`--network full`).
- **`--network none`** — rejected: breaks package installs and the Anthropic API itself.

## Consequences

- Launch must add `NET_ADMIN` and run the firewall init before the agent starts; this is a mild privilege the container's network namespace contains.
- The agent's initial instructions (sandbox skill / launch system prompt) must explain the firewall and the request-to-add protocol.
- Adding a domain is a config edit + relaunch (ADR-0009), not a live change — acceptable, and consistent with the disposable model. A convenience `sr sandbox allow <domain> [branch]` (append to allowlist + offer relaunch) is a natural ergonomic follow-up.
- **Environment caveat (verified):** the firewall needs a working in-container `iptables`. On Docker Desktop (daemon in a VM) `iptables` is unavailable, so the init script warns on stderr and runs **without** enforcement rather than trapping the agent offline. Enforcement is effective on a native Linux Docker host (NET_ADMIN + iptables/ipset). The default is thus best-effort: safe where supported, non-fatal where not.
