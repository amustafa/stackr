---
name: sr-implement
description: >
  Implement a GitHub issue or Jira ticket on a fresh branch. Use when the user
  wants to turn an issue or ticket into code, or says "implement", "sr implement",
  "/sr-implement", or hands you an issue number (123, #123), a ticket key
  (PROJ-456), or an issue URL to build.
---

# sr implement

Thin driver over the `sr implement` command. It fetches the issue, creates a
**new tracked branch**, records the issue linkage, and hands you a self-contained
brief. You then implement that brief the way a careful session works: ground in
the real code, settle what's genuinely open, build in reviewable steps, verify
the real path, and leave a PR that closes the issue.

## Start — get the brief

Always call the `--ai` form. It creates the branch and returns JSON; it does NOT
spawn a nested agent, so you stay in control of the work:

    sr implement <ref> --ai                # 123 | #123 | PROJ-456 | issue URL
    sr implement <ref> --ai --worktree     # branch in a worktree (leave cwd untouched)
    sr implement <ref> --ai --comments     # fold the issue discussion into the brief
    sr implement <ref> --ai --source jira  # force the source if auto-detect is unsure

It returns `{ "branch", "worktreePath", "issueRef", "prompt" }`. Work on
`branch`; if `worktreePath` is non-empty, `cd` there first. `prompt` is your brief.

## Then — implement the brief

Work the brief in these steps (the same shape as a good design session):

1. **Ground in the code first.** Before writing anything, read the files the
   issue touches and the patterns around them. The repo is the source of truth,
   not the issue's wording — reconcile the two before you commit to an approach.
2. **Settle what's genuinely open — ask when unsure.** If the issue leaves a real
   decision (an API shape, a trade-off, an ambiguous term), don't guess: surface
   the options to the user and let them choose before you commit to an approach.
   For a substantial issue, sketch the approach and confirm it before coding.
   Record what you settle with `sr context set approach "<decision + rationale>"`.
3. **Build in reviewable steps.** Implement bottom-up and commit as you go with
   `sr commit -a -m "..."` (never `git commit`) so the branch stays coherent and
   each step stands on its own.
4. **Verify the real path.** Build, vet, and test — then exercise the actual
   behavior the issue asks for. "It compiled" / "tests pass" is not "it works."
5. **Leave a closing PR.** Record the PR so `sr submit` opens one that closes the
   issue:

       sr context set pr "<title>\n\n<summary>\n\nCloses #123"

   Then run `sr submit`, or tell the user the branch is ready.

The issue ref is already saved as a `ticket` context entry and as the branch
objective — `sr info` shows both, and PR generation picks them up.

## Sandbox lane — `--sandbox`

`--sandbox` runs the implementation in a disposable container instead of here
(it implies a worktree). You do NOT implement the brief yourself — you launch it
and keep watch:

    sr implement <ref> --sandbox --ai      # launch detached; returns attachCommand

Report the returned `attachCommand` to the user, then track it:

    sr sandbox watch                       # live status (awaiting-input on top)
    sr sandbox attach <branch>             # drop into the session

The container runs the same five steps. When it needs a decision (step 2), it
can't reach the user directly — it pauses in an `awaiting-input` state that
surfaces in `sr sandbox watch`, which is your cue to attach and answer.

The sandbox has no credentials and closes the loop the same way — it sets the
`pr` context inside the container, and host-side `sr submit` opens the PR. See
the sr-sandbox skill for attach/watch/teardown.

## Notes

- Always a NEW branch — `sr implement` never edits an existing branch in place.
- GitHub refs need `gh`; Jira refs need the `jira` CLI (it reuses your existing
  jira-cli config + keyring token).
- `--branch <name>` overrides the derived `<ref>-<title-slug>` name; `--parent
  <branch>` changes what the branch stacks on (default: the current branch).
