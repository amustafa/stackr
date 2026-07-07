# `sr implement` fetches issues host-side via existing CLIs and bakes them into a self-contained prompt

Status: accepted

`sr implement <ref>` must turn a GitHub issue number or a Jira ticket key into a working branch. Two questions have non-obvious answers: **where** the fetch happens (host vs. sandbox) and **how** each source is read (native client vs. external CLI).

## Decision

**Fetch on the host, never in the sandbox.** `sr implement` resolves the issue on the developer's machine — where credentials, `gh`, `jira`, and network already work — and bakes the resulting title/body/labels/URL into a **self-contained prompt**. When `--sandbox` is used, only that prompt text crosses into the container; the sandbox performs no issue fetch and needs no issue-source credentials or firewall allowances. This preserves ADR-0010 (no credentials in the sandbox) for free.

**Read each source by shelling to its existing CLI**, consistent with ADR-0003 (CLI over library/MCP for agent operations):

- **GitHub** — `gh issue view <n> --json title,body,labels,url` (and `--comments` when requested). Already a stackr dependency for PRs.
- **Jira** — shell to the `jira` binary (ankitpokhrel/jira-cli): `jira issue view <KEY> --raw` for the summary (branch naming) and `--plain` for the body. jira-cli owns config discovery, the keyring-stored API token, and — critically — the **ADF→plain-text rendering** that a hand-rolled REST client would have to reimplement.

Source is auto-detected by shape (`#?\d+` → GitHub, `[A-Za-z][A-Za-z0-9]+-\d+` → Jira) with issue-URL parsing and a `--source github|jira` override for ambiguity.

## Considered options

- **Fetch inside the sandbox** — rejected: the sandbox has no credentials (ADR-0010) and sits behind an egress allowlist (ADR-0012); fetching there would require punching both, re-introducing the exact exfiltration surface those ADRs remove.
- **Jira via REST + token (own Go HTTP client)** — rejected: jira-cli stores its token in the OS keyring, so we'd reimplement per-OS keyring access *and* ADF-document rendering just to avoid a binary we can shell to. High effort, fragile, asymmetric with how we already treat GitHub.
- **Jira via config-file parsing only** — rejected: `~/.config/.jira/.config.yml` yields the server + login but not the token (keyring), so it can't actually authenticate on its own.
- **Delegate all fetching to the `/sr-implement` skill (Atlassian MCP + gh)** — rejected as the *only* path: it would make `sr implement <ref>` useless as a bare CLI. The skill remains a thin driver; the Go command stays self-contained.

## Consequences

- `gh` is already required; **`jira` becomes a soft dependency** — needed only for Jira refs, checked at fetch time with a helpful install message (mirrors `ghCheckInstalled`).
- Jira support inherits whatever the user's jira-cli is configured against (Cloud or Server), with no new auth surface in stackr.
- The sandbox path is unchanged from ADR-0008/0010: it receives a prompt string and runs `SandboxRun`; it never learns the issue exists as a remote resource.
- Fetching host-side means a closed/missing issue or an auth failure surfaces immediately, before any branch is created — cheaper to recover from than a half-scaffolded sandbox.
