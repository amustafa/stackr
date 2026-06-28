# Commit-level context and post-worktree hooks

With the MCP server removed (ADR-0003), agents interact with stackr exclusively through the CLI. Two gaps emerged: agents need a way to attach structured reasoning to individual commits (not just the branch), and worktrees need project-specific post-creation setup.

## Commit context

Context now exists at two levels: **branch context** (high-level decisions via `sr context set`) and **commit context** (per-step reasoning via `sr commit --context`). Commit context is a JSON blob — structured information like links to plan steps, ADRs, tickets, or agent reasoning explaining why a specific change was made. Both levels live in the stackr graph, not in git history, so they don't pollute commit messages.

Both are lost on squash. Anything that should outlive the branch must be persisted to a file by the user or agent before squashing. Stackr does not manage context archival — that's outside its domain.

## Post-worktree hook

A user-defined script at `.stackr/hooks/post-worktree` runs after worktree creation (from `sr create --worktree` or `sr worktree add`). It receives the worktree path as `$1` and handles project-specific setup: copying `.env`, `.envrc`, symlinks, editor config — anything the project needs that isn't committed.

## sr commit

A new `sr commit` command wraps `git commit` with stackr integration: staging flags (`-a`, `-u`, `-p`), commit message (`-m`), branch description update (`--desc`), and commit context attachment (`--context`). This replaces the pattern of running `git commit` + `sr context set` as separate steps.
