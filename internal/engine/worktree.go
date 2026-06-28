package engine

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/amustafa/stackr/internal/context"
)

// WorktreeAddOpts holds options for adding a worktree.
type WorktreeAddOpts struct {
	Name string
}

// WorktreeRemoveOpts holds options for removing a worktree.
type WorktreeRemoveOpts struct {
	Name   string
	Delete bool // also delete the branch
}

// WorktreeAdd creates a worktree for the named branch under .worktrees/.
func WorktreeAdd(c *context.Context, opts WorktreeAddOpts) error {
	root := c.Git.Dir
	wtDir := filepath.Join(root, ".worktrees")

	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		return fmt.Errorf("creating .worktrees dir: %w", err)
	}

	if err := ensureGitExclude(c); err != nil {
		return fmt.Errorf("updating .git/info/exclude: %w", err)
	}

	exists, err := c.Git.BranchExists(opts.Name)
	if err != nil {
		return err
	}
	if !exists {
		if err := c.Git.CreateBranch(opts.Name, ""); err != nil {
			return err
		}
	}

	wtPath := filepath.Join(".worktrees", opts.Name)
	if err := c.Git.WorktreeAdd(wtPath, opts.Name); err != nil {
		return err
	}

	absWtPath := filepath.Join(root, wtPath)

	if err := runPostWorktreeHook(c, absWtPath); err != nil && !c.Quiet {
		fmt.Printf("Warning: post-worktree hook failed: %v\n", err)
	}

	if !c.Quiet {
		fmt.Printf("Created worktree for %q at %s\n", opts.Name, absWtPath)
	}
	return nil
}

// WorktreeRemove removes a worktree and optionally deletes the branch.
func WorktreeRemove(c *context.Context, opts WorktreeRemoveOpts) error {
	wtPath := filepath.Join(".worktrees", opts.Name)
	if err := c.Git.WorktreeRemove(wtPath); err != nil {
		return err
	}

	if opts.Delete {
		if err := c.Git.DeleteBranch(opts.Name, false); err != nil {
			return fmt.Errorf("worktree removed but branch delete failed: %w", err)
		}
	}

	if !c.Quiet {
		if opts.Delete {
			fmt.Printf("Removed worktree and deleted branch %q\n", opts.Name)
		} else {
			fmt.Printf("Removed worktree for %q\n", opts.Name)
		}
	}
	return nil
}

// ensureGitExclude adds ".worktrees" to .git/info/exclude if not already present.
func ensureGitExclude(c *context.Context) error {
	gitDir, err := c.Git.GitDir()
	if err != nil {
		return err
	}

	infoDir := filepath.Join(gitDir, "info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		return err
	}

	excludePath := filepath.Join(infoDir, "exclude")
	const entry = ".worktrees"

	// Check if entry already exists.
	if f, err := os.Open(excludePath); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			if strings.TrimSpace(scanner.Text()) == entry {
				f.Close()
				return nil
			}
		}
		f.Close()
	}

	// Append the entry.
	f, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = fmt.Fprintln(f, entry)
	return err
}

// runPostWorktreeHook runs .stackr/hooks/post-worktree if it exists.
func runPostWorktreeHook(c *context.Context, wtPath string) error {
	hookPath := filepath.Join(c.Git.Dir, ".stackr", "hooks", "post-worktree")
	info, err := os.Stat(hookPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&0o111 == 0 {
		return nil
	}

	cmd := exec.Command(hookPath, wtPath)
	cmd.Dir = c.Git.Dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
