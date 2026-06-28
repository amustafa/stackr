package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	srerr "github.com/amustafa/stackr/internal/errors"
)

// Runner executes git commands. The zero value uses the current directory.
type Runner struct {
	Dir    string // Working directory for git commands
	Env    []string
	Debug  bool
	Verify bool // Pass --no-verify when false (for hooks)
}

// RunGit executes a git command, forwarding stdout/stderr to the terminal.
// Use this for interactive commands (rebase, add -p, etc.).
func (r *Runner) RunGit(args ...string) error {
	cmd := r.command(args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if r.Debug {
		fmt.Fprintf(os.Stderr, "[debug] git %s\n", strings.Join(args, " "))
	}
	if err := cmd.Run(); err != nil {
		return &srerr.GitError{Args: args, Err: err}
	}
	return nil
}

// RunGitCapture executes a git command and returns trimmed stdout.
// Stderr is captured for error reporting.
func (r *Runner) RunGitCapture(args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := r.command(args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if r.Debug {
		fmt.Fprintf(os.Stderr, "[debug] git %s\n", strings.Join(args, " "))
	}
	if err := cmd.Run(); err != nil {
		return "", &srerr.GitError{Args: args, Stderr: stderr.String(), Err: err}
	}
	return strings.TrimSpace(stdout.String()), nil
}

// RunGitCaptureAll executes a git command and returns stdout, stderr, and error.
// Useful when stderr contains meaningful output (e.g., push progress).
func (r *Runner) RunGitCaptureAll(args ...string) (stdout, stderr string, err error) {
	var outBuf, errBuf bytes.Buffer
	cmd := r.command(args...)
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if r.Debug {
		fmt.Fprintf(os.Stderr, "[debug] git %s\n", strings.Join(args, " "))
	}
	runErr := cmd.Run()
	return strings.TrimSpace(outBuf.String()), strings.TrimSpace(errBuf.String()), runErr
}

func (r *Runner) command(args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	if r.Dir != "" {
		cmd.Dir = r.Dir
	}
	if len(r.Env) > 0 {
		cmd.Env = append(os.Environ(), r.Env...)
	}
	return cmd
}
