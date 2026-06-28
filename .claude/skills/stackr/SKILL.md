---
name: stackr
description: >
  Stacked branch workflow manager. Use when working with git branches, creating
  commits, pushing code, or managing PRs. Proactively track design decisions and
  context with sr context while working on any branch.
---

# Stackr (sr)

This repo uses stackr for stacked branch management. Use sr commands instead of
raw git for branch operations.

## How to Work

Decompose features into layered branches that build on each other. Each branch
should be independently reviewable. Build bottom-up: foundational changes first,
dependent changes stacked on top.

When creating a branch, always set an objective:

    sr create feat-auth-models --desc "User and session types for JWT auth"

Use sr commit (not git commit) to keep the graph in sync:

    sr commit -a -m "add session model"

Track decisions as you go — this is what separates good stacks from chaotic ones.

## Context Tracking — Do This As You Work

Two levels of context exist. Use both proactively.

**Branch context** — high-level decisions for the whole branch:

    sr context set approach "Stateless JWTs — no DB sessions"
    sr context set tradeoff "No revocation without blocklist; ok for v1"
    sr context set design "Split handler into middleware chain" --source file:internal/api/handler.go

**Commit context** — per-step reasoning (JSON blob):

    sr commit -a -m "add rotation" --context '{"key":"step","text":"Refresh tokens rotate per OWASP","sources":[{"type":"url","reference":"https://..."}]}'

Both feed into PR generation and are visible via sr info.
Both are lost on squash — persist important context to files first.

### What to Track

- Design choice: key "design", "approach", "tradeoff"
- Plan reference: key "step", with --source file:path/to/plan.md
- Related files: key "related-files", with --source flag
- Tickets: use --ticket flag
- Rationale: key "rationale" — why this approach over alternatives
- Followup: key "followup" — things to revisit later

Remove stale entries with sr context rm.

## Core Workflow

    sr create <name> [-m "commit message"] [-a]    # New stacked branch
    sr create <name> --desc "objective" --worktree  # Create with worktree
    sr commit -a -m "message"                      # Commit with stackr tracking
    sr commit -a -m "msg" --context '{"key":"k","text":"t"}'  # Commit with context
    sr modify [-m "message"] [-a] [-c]             # Amend and restack
    sr submit [--ai] [-d] [-s] [-f]                # Push to remote
    sr sync                                        # Fetch trunk, restack, clean merged

Use sr commit instead of git commit to track commit context and keep the graph
in sync. Context entries are JSON blobs with key, text, sources, and tickets.

## Submit (3 modes)

**Programmatic (you're already in a session):**

    sr submit --aiprepare                          # Output PR context as JSON
    sr submit --title "..." --body "..."           # Create PR directly
    sr submit --title "..." --body-file /tmp/pr.md # Body from file

**Interactive:** sr submit (wizard: Push only / Create PR)

**AI-driven:** sr submit --ai (Claude generates and submits autonomously)

## Review (3 modes)

Walk the stack bottom-to-top addressing PR review comments.

**Programmatic:**

    sr address-review --aiprepare                          # Output all unresolved comments as JSON

**Interactive:** sr address-review (edit/reply/skip per comment, commit, restack, move up)

**AI-driven:** sr address-review --ai (Claude addresses all comments autonomously)

## Navigation

    sr up [n]            # Move up the stack (away from trunk)
    sr down [n]          # Move down the stack (toward trunk)
    sr top               # Jump to top of stack
    sr bottom            # Jump to bottom of stack
    sr checkout <branch> # Switch to a tracked branch
    sr trunk             # Switch to trunk

## Inspection

    sr info [branch]     # Branch details, objective, context, commits
    sr info -s           # Include diff stat
    sr info -d           # Include full diff
    sr log               # Visualize stack tree
    sr log -a            # Show all stacks
    sr log -l            # Show commits per branch

## Other Commands

    sr rename <new>          # Rename current branch
    sr delete <branch>       # Delete and reparent children
    sr move -p <new-parent>  # Reparent a branch
    sr restack               # Rebase all branches onto their parents
    sr fold                  # Fold branch into parent
    sr squash                # Squash commits on current branch
    sr split                 # Split current branch
    sr undo                  # Undo last operation
    sr push-meta             # Push stackr metadata to remote
    sr pull-meta             # Pull stackr metadata from remote
