---
name: sr-sandbox
description: >
  Launch or manage a sandboxed Claude session — a disposable Docker container
  running claude --dangerously-skip-permissions on an isolated branch worktree.
  Use when the user wants to run an agent freely without host risk, or says
  "sandbox", "sr sandbox", or "/sr-sandbox".
---

# sr sandbox

Thin wrapper over the `sr sandbox` command tree. A sandbox is a disposable
Docker container running Claude with skip-permissions on one branch's worktree,
with ~/.claude mounted so config + session history carry over (resumable from
the host). Durable state is host-side, so containers are throwaway.

## Launch & attach

    sr sandbox <branch> -- "<initial prompt>"   # create/reuse worktree, launch, attach
    sr sandbox attach                            # searchable picker of active sandboxes
    sr sandbox attach <branch>                   # attach directly
    sr sandbox ls                                # list with status

Detaching (zellij detach / close terminal) leaves it running.

## Knowing when it needs you

Hooks publish each sandbox's state (working / awaiting-input / awaiting-choice /
exited) with the pending question:

    sr sandbox watch            # live dashboard (awaiting on top)
    sr sandbox watch --notify   # desktop notifications on transition to awaiting
    sr sandbox awaiting         # count, for a shell prompt

## Firewall

The sandbox runs behind an egress allowlist by default. If it needs a blocked
domain, add it and relaunch:

    sr sandbox config           # add the domain to the allowlist
    sr sandbox <branch>         # relaunch (resumes the session)
    sr sandbox <branch> --network full   # accepted-risk opt-out

## Pushing / PRs (host-side)

The sandbox has no GitHub credentials. When it has a PR-worthy result it records
a suggestion as branch context:

    sr context set pr "<title>\n\n<body>"    # inside the sandbox, before teardown

Then on the host, `sr submit` reads the reserved `pr` entry and offers it as
the PR title/body. Branch context is lost on squash — set it after any squash.

## Teardown

    sr sandbox stop <branch>            # keep container; relaunch resumes
    sr sandbox rm <branch>              # remove container, keep worktree (cold-resume)
    sr sandbox rm <branch> --delete     # also remove worktree + branch

## Config

    sr sandbox config       # TUI: network, base image, firewall, caches, bin dir
    sr sandbox config --ai  # Claude helps manage it
