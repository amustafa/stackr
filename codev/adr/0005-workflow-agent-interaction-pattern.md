# Workflow-agent interaction pattern

Every workflow command in stackr follows a single interaction pattern with five layers. The workflow itself defines what to do — a sequence of steps with state transitions, conflict handling, and completion conditions. The layers determine who drives it and how.

## The five layers

**1. TUI interactive (default).** The bare command (`sr submit`, `sr address-review`) runs a terminal walkthrough. Each decision point presents options via a selector, confirm, or text input. The user drives. This is the canonical definition of the workflow — if you understand the interactive mode, you understand the operation.

**2. Fully parameterized CLI.** Every parameter the TUI collects can be passed as a flag (`--title`, `--body`, `--draft`). When all required parameters are present, the command runs without prompts. This makes the workflow scriptable and testable without any AI involvement.

**3. Context preparation (`--aiprepare`).** Outputs a JSON blob to stdout containing everything an agent needs to make decisions: the current state (branch, diff, commits, existing PR, unresolved review threads), the operation's options, and any templates. This is a read-only operation — no side effects.

**4. In-session agent skill.** A Claude Code skill (`sr claude install`) teaches agents the full command vocabulary. An agent already in a session calls `--aiprepare` to understand the situation, makes decisions using its conversation context, then calls the fully parameterized CLI to execute. The agent composes steps 2 and 3 itself. This is the highest-fidelity mode because the agent has the full conversation history, codebase understanding, and user intent.

**5. Autonomous agent (`--ai`).** Spawns a standalone Claude session with the `--aiprepare` output piped via stdin. Uses `/goal` for completion-condition-driven execution — Claude keeps working across turns until the goal is met, then exits. The spawned session uses `--bare` (no project hooks or MCP discovery), `--append-system-prompt` (workflow-specific instructions), and `--allowedTools` (scoped to `Read,Edit,Bash(sr *),Bash(git *),Bash(gh *)`). This mode is for fire-and-forget — the user runs `sr submit --ai` and walks away.

## Why `/goal` instead of `--max-turns`

`--max-turns N` is a fixed budget that doesn't adapt to the work. A 2-comment review finishes in 2 turns; a 20-comment review needs 15. With `--max-turns`, you either set it high (wasting tokens on easy tasks) or low (bailing mid-work on hard ones). `/goal` is condition-based — "all review comments are addressed and stack is restacked" — so Claude works until done regardless of how many turns it takes, then exits.

## Why five layers instead of fewer

Each layer serves a different caller:

- **Human at a terminal** → TUI interactive
- **Shell script or CI** → fully parameterized CLI
- **Agent needing context** → `--aiprepare` JSON
- **Agent already working** → skill + parameterized CLI (layers 3+2)
- **Human who wants AI to handle it** → `--ai` autonomous mode

Collapsing any two would either break a use case or force one caller to accommodate another's needs. The parameterized CLI is the linchpin — both the TUI and the AI modes ultimately call the same flags, so there's one code path for execution regardless of who's driving.

## Pattern template for new commands

When adding a workflow command:

1. Define the workflow as a TUI interactive flow with decision points
2. Add flags for every parameter the TUI collects
3. Add `--aiprepare` that outputs the operation's context as JSON
4. Add `--ai` that calls `--aiprepare` internally, builds a `/goal` string, and spawns Claude with `--bare`
5. Document the command in the Claude Code skill so in-session agents know it exists
