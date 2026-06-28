# Three-mode pattern for AI-integrated commands

Commands that involve AI assistance (submit, address-review) follow a three-mode pattern:

1. **Programmatic** — `--aiprepare` outputs JSON context; the caller (an agent already in session) acts on it and calls back with explicit flags (e.g., `--title`/`--body`)
2. **Bare interactive** — no flags; a terminal wizard with no AI involvement
3. **Agent interactive** — `--ai` spawns a full Claude session with `/goal` that runs autonomously and exits when done

This was chosen over a single `--ai` flag that shells out to `claude -p` (prompt mode) because agents already in a session have richer context than a blind subprocess. The two-step programmatic mode lets the calling agent use its own conversation context. The autonomous mode uses `/goal` instead of `--max-turns` so Claude keeps working until the condition is met regardless of how many turns it takes.
