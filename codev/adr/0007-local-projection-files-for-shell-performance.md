# Local projection files over subprocesses for shell-hot paths

When the shell integration needs data from stackr on every directory change or prompt render, spawn a subprocess to read from git refs. Instead, project the needed data into a flat file under `.git/.stackr/` at write time, and read it with pure shell builtins at query time.

**The rule:** if data is read on a shell-hot path (chpwd, precmd, prompt), it must be readable without spawning a process. Write a local projection file when the source of truth changes; read the projection in the hook.

**First application:** the prompt cache (`.git/.stackr/prompt-cache`) projects `SR_BRANCH` and `SR_STACK_DEPTH` from the graph. The graph (in `refs/stackr/data`) is the source of truth. The cache is written on every graph mutation. The chpwd hook reads two lines from a flat file — no `sr`, no `git cat-file`, no JSON parsing.

**Considered alternatives:**
- **Subprocess on every cd** (`sr prompt-info`) — correct but adds 30-50ms to every prompt. Unacceptable for interactive shells.
- **Pure shell git-object parsing** (`git cat-file` + grep) — still spawns git, still parses JSON. ~10-15ms, still noticeable in prompt latency budgets.
- **No prompt integration** — avoids the problem but users lose stack awareness in their prompt.

**Constraints:**
- Projection files are **Local Data** — never shared, never pushed. They live in `.git/.stackr/` alongside undo snapshots.
- The graph is always the source of truth. A missing or stale projection file is a degraded experience (empty prompt vars), never incorrect behavior.
- Projection files must be updated atomically (write-tmp + rename) to avoid partial reads from concurrent shell hooks.

**Consequences:** any future shell-visible data follows this pattern — add a field to the projection file, update it at write time, read it in the hook. Don't add new subprocesses to the prompt path.
