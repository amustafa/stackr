# CLI via Bash for agent operations, not MCP

AI agents interact with stackr through the CLI (`sr` commands via Bash), not through an MCP server. An MCP server was built and removed — it duplicated the CLI's functionality without adding value, since agents already run shell commands for everything else (git, gh, file edits).

A Claude Code skill (`sr claude install`) teaches agents the command set. For programmatic workflows, `--aiprepare` outputs structured JSON and `--title`/`--body` flags accept direct input. For autonomous workflows, `--ai` spawns a Claude session with `/goal` that runs CLI commands itself.

This keeps the tool surface to one layer (CLI) instead of two (CLI + MCP).
