package engine

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/sandbox"
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

// worktreesRoot returns the absolute path of the .worktrees directory that holds
// all of this repo's worktrees. It is anchored on the MAIN repo root (the
// directory containing the shared .git), NOT the current checkout — so the
// location is identical whether sr runs from the main checkout or from inside
// another worktree. This keeps it in lockstep with the sandbox's canonical
// worktree path (ADR-0008) and avoids nesting a .worktrees pyramid when a
// worktree is created from within a worktree.
func worktreesRoot(c *context.Context) (string, error) {
	gitCommon, err := absGitCommonDir(c)
	if err != nil {
		return "", err
	}
	return filepath.Join(sandbox.MainRoot(gitCommon), ".worktrees"), nil
}

// WorktreeAdd creates a worktree for the named branch under the main repo's
// .worktrees/ directory.
func WorktreeAdd(c *context.Context, opts WorktreeAddOpts) error {
	wtDir, err := worktreesRoot(c)
	if err != nil {
		return err
	}

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

	// Use an absolute path so `git worktree add` lands under the main root
	// regardless of the current working directory.
	wtPath := filepath.Join(wtDir, opts.Name)
	if err := c.Git.WorktreeAdd(wtPath, opts.Name); err != nil {
		return err
	}

	if err := runPostWorktreeHook(c, wtPath); err != nil && !c.Quiet {
		fmt.Printf("Warning: post-worktree hook failed: %v\n", err)
	}

	if !c.Quiet {
		fmt.Printf("Created worktree for %q at %s\n", opts.Name, wtPath)
	}
	return nil
}

// WorktreeRemove removes a worktree and optionally deletes the branch.
func WorktreeRemove(c *context.Context, opts WorktreeRemoveOpts) error {
	wtDir, err := worktreesRoot(c)
	if err != nil {
		return err
	}
	wtPath := filepath.Join(wtDir, opts.Name)
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

// ensureGitExclude adds ".worktrees" to the shared .git/info/exclude if not
// already present. It targets the common git dir (not the per-worktree gitdir)
// so the exclude covers the main repo, where the .worktrees directory lives.
func ensureGitExclude(c *context.Context) error {
	gitDir, err := absGitCommonDir(c)
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
