# Sandbox lane — `sr sandbox`

A sandbox is a disposable Docker container running
`claude --dangerously-skip-permissions` on **one branch's worktree**, with your
`~/.claude` mounted so config + session history carry over (resumable from the
host). Durable state is host-side, so containers are throwaway. Alias: `sr sb`.

## Launch & attach

    sr sandbox <branch> -- "<initial prompt>"   # create/reuse worktree, launch, attach
    sr sandbox <branch> --no-attach             # launch without attaching
    sr sandbox attach                           # searchable picker of active sandboxes
    sr sandbox attach <branch>                  # attach directly
    sr sandbox ls                               # list this repo's sandboxes with status

Everything after `--` is the initial prompt. Detaching (zellij detach, or closing
the terminal) leaves the sandbox running.

## Knowing when it needs you

Skip-permissions stops permission prompts, not genuine questions. Hooks publish
each sandbox's state — `working` / `awaiting-input` / `awaiting-choice` /
`exited` — with the pending question:

    sr sandbox watch            # live dashboard (awaiting on top)
    sr sandbox watch --notify   # desktop notifications on transition to awaiting
    sr sandbox awaiting         # bare count, for a shell prompt

## Firewall

The sandbox runs behind an egress allowlist by default. If it needs a blocked
domain, add it and relaunch (the session resumes):

    sr sandbox config                    # add the domain to the allowlist
    sr sandbox <branch>                  # relaunch
    sr sandbox <branch> --network full   # accepted-risk opt-out (no allowlist)

## Pushing / PRs (host-side)

The sandbox has **no GitHub credentials**. When it has a PR-worthy result it
records a suggestion as branch context, before teardown:

    sr context set pr "<title>\n\n<body>"    # run inside the sandbox

Host-side, `sr submit` reads the reserved `pr` entry and offers it as the PR
title/body. Branch context is lost on squash — set it *after* any squash.

## Teardown

    sr sandbox stop <branch>            # keep container; relaunch resumes
    sr sandbox rm <branch>              # remove container, keep worktree (cold-resume)
    sr sandbox rm <branch> --delete     # also remove worktree + branch

## Config

    sr sandbox config       # TUI: network, base image, firewall, caches, bin dir
    sr sandbox config --ai  # Claude helps manage it
