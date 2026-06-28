package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

const skillDir = ".claude/skills/stackr"
const skillFile = "SKILL.md"

var claudeCmd = &cobra.Command{
	Use:   "claude",
	Short: "Claude Code integration",
}

var claudeInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the stackr skill for Claude Code",
	Long:  "Creates a Claude Code skill at .claude/skills/stackr/SKILL.md that teaches Claude how to use sr commands.",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := ctx.Git.RepoRoot()
		if err != nil {
			return fmt.Errorf("could not find repo root: %w", err)
		}

		dir := filepath.Join(repoRoot, skillDir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("could not create skill directory: %w", err)
		}

		path := filepath.Join(dir, skillFile)
		if err := os.WriteFile(path, []byte(stackrSkillContent), 0o644); err != nil {
			return fmt.Errorf("could not write skill: %w", err)
		}

		fmt.Printf("Installed stackr skill to %s\n", filepath.Join(skillDir, skillFile))
		return nil
	},
}

var claudeUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove the stackr skill from Claude Code",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := ctx.Git.RepoRoot()
		if err != nil {
			return fmt.Errorf("could not find repo root: %w", err)
		}

		dir := filepath.Join(repoRoot, skillDir)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			fmt.Println("No stackr skill found")
			return nil
		}

		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("could not remove skill: %w", err)
		}
		fmt.Println("Removed stackr skill")
		return nil
	},
}

func init() {
	claudeCmd.AddCommand(claudeInstallCmd)
	claudeCmd.AddCommand(claudeUninstallCmd)
	rootCmd.AddCommand(claudeCmd)
}

const stackrSkillContent = `---
name: stackr
description: >
  Stacked branch workflow manager. Use when working with git branches, creating
  commits, pushing code, or managing PRs. Proactively track design decisions and
  context with sr context while working on any branch.
---

# Stackr (sr)

This repo uses stackr for stacked branch management. Use sr commands instead of
raw git for branch operations.

## Context Tracking — Do This As You Work

When working on a branch, proactively track decisions and context. This metadata
persists across sessions and feeds into PR description generation.

**Add context when you make a decision or discover something relevant:**

    sr context set <key> <text> [--source type:reference] [--ticket PROJ-123]

Examples:

    sr context set approach "Using optimistic locking to avoid DB contention"
    sr context set design "Split handler into middleware chain" --source file:internal/api/handler.go
    sr context set scope "Auth refresh + token rotation" --ticket AUTH-456,AUTH-789

**Manage context:**

    sr context list          # Show all context entries
    sr context rm <key>      # Remove a stale entry

**Set the branch objective:**

    sr describe "Add JWT refresh token rotation"
    sr describe                                    # Show current objective

### When to Set Context

- Non-obvious design choice: key "design", "approach", "tradeoff"
- Relevant files or dependencies: key "related-files", with --source flag
- Linked tickets or issues: use --ticket flag
- Notes for future work: key "note", "followup"
- Rationale for choosing one approach over another: key "rationale"

Remove entries when they become stale or the branch objective changes.

## Core Workflow

    sr create <name> [-m "commit message"] [-a]    # New stacked branch
    sr modify [-m "message"] [-a] [-c]             # Amend and restack
    sr submit [--ai] [-d] [-s] [-f]                # Push to remote
    sr sync                                        # Fetch trunk, restack, clean merged

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
`
