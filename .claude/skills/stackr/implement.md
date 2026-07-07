# Implement lane — `sr implement`

Turns a GitHub issue or Jira ticket into code on a **new tracked branch**.
`sr implement` fetches the issue, creates the branch, records the issue linkage
(as a `ticket` context entry and the branch objective), and hands you a
self-contained brief. You implement the brief. It never edits an existing branch
in place.

The ref is auto-detected: a number (`123`, `#123`) or GitHub URL → GitHub; a
`KEY-N` (`PROJ-456`) or browse URL → Jira. Force it with `--source github|jira`.

## Start — get the brief

Call the `--ai` form. It scaffolds the branch and returns JSON; it does **not**
spawn a nested agent, so you stay in control:

    sr implement <ref> --ai                # 123 | #123 | PROJ-456 | issue URL
    sr implement <ref> --ai --worktree     # branch in a worktree (leaves cwd untouched)
    sr implement <ref> --ai --comments     # fold the issue discussion into the brief
    sr implement <ref> --ai --source jira  # force the source if auto-detect is unsure
    sr implement <ref> --ai --branch <name> --parent <branch>   # override name / stack point

It returns `{ "branch", "worktreePath", "issueRef", "prompt" }`. Work on
`branch`; if `worktreePath` is non-empty, `cd` there first. `prompt` is your brief.

## Then — implement the brief

Work it in these steps (the shape of a good design session):

1. **Ground in the code first.** Read the files the issue touches and the
   patterns around them before writing anything. The repo is the source of truth,
   not the issue's wording — reconcile the two before committing to an approach.
2. **Settle what's genuinely open — ask when unsure.** If the issue leaves a real
   decision (an API shape, a trade-off, an ambiguous term), surface the options
   to the user rather than guessing. For a substantial issue, sketch the approach
   and confirm it before coding. Record what you settle:
   `sr context set approach "<decision + rationale>"`.
3. **Build in reviewable steps.** Implement bottom-up, committing as you go with
   `sr commit -a -m "..."` (never `git commit`) so each step stands on its own.
4. **Verify the real path.** Build, vet, and test — then exercise the actual
   behavior the issue asks for. "It compiled" / "tests pass" is not "it works."
5. **Leave a closing PR.** Record it so `sr submit` opens one that closes the issue:

       sr context set pr "<title>\n\n<summary>\n\nCloses #123"

   Then run `sr submit`, or tell the user the branch is ready.

## Sandbox lane — `--sandbox`

`--sandbox` runs the implementation in a disposable container instead of here
(it implies a worktree). You do **not** implement the brief yourself — you launch
it and keep watch:

    sr implement <ref> --sandbox --ai      # launch detached; returns an attach command

Report the returned attach command to the user, then track it with
`sr sandbox watch` (awaiting-input on top) and `sr sandbox attach <branch>`. The
container runs the same five steps; when it needs a decision (step 2) it can't
reach the user directly, so it pauses in `awaiting-input` — your cue to attach and
answer. It closes the loop the same way: sets the `pr` context inside the
container, and host-side `sr submit` opens the PR. See `sandbox.md` for
attach/watch/teardown.

## Notes

- Always a **new** branch; default name `<ref>-<title-slug>`, default parent the
  current branch (override with `--branch` / `--parent`).
- GitHub refs need the `gh` CLI; Jira refs need the `jira` CLI (it reuses your
  existing jira-cli config + keyring token).
- Outside a Claude session and without `--ai`, a bare terminal invocation spawns
  an interactive `claude` seeded with the brief instead of emitting JSON.
