// Package docker is a thin wrapper over the `docker` CLI, in the same
// shell-wrapper style as internal/git. It assembles argv deterministically
// (sorted map keys) so container identity and tests are stable, and splits
// interactive (Run) from captured (RunCapture) execution.
package docker

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// Runner executes docker commands. The zero value uses the current directory.
type Runner struct {
	Dir   string
	Env   []string
	Debug bool
}

// CommandError wraps a failed docker command with its stderr.
type CommandError struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *CommandError) Error() string {
	return fmt.Sprintf("docker %v failed: %s", e.Args, e.Stderr)
}

func (e *CommandError) Unwrap() error { return e.Err }

// Available reports whether the docker CLI is on PATH.
func (r *Runner) Available() bool {
	_, err := exec.LookPath("docker")
	return err == nil
}

// Run executes a docker command, forwarding stdin/stdout/stderr to the
// terminal. Use for interactive commands (exec -it, build output).
func (r *Runner) Run(args ...string) error {
	cmd := r.command(args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if r.Debug {
		fmt.Fprintf(os.Stderr, "[debug] docker %s\n", strings.Join(args, " "))
	}
	if err := cmd.Run(); err != nil {
		return &CommandError{Args: args, Err: err}
	}
	return nil
}

// RunCapture executes a docker command and returns trimmed stdout.
func (r *Runner) RunCapture(args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := r.command(args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if r.Debug {
		fmt.Fprintf(os.Stderr, "[debug] docker %s\n", strings.Join(args, " "))
	}
	if err := cmd.Run(); err != nil {
		return "", &CommandError{Args: args, Stderr: strings.TrimSpace(stderr.String()), Err: err}
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (r *Runner) command(args ...string) *exec.Cmd {
	cmd := exec.Command("docker", args...)
	if r.Dir != "" {
		cmd.Dir = r.Dir
	}
	if len(r.Env) > 0 {
		cmd.Env = append(os.Environ(), r.Env...)
	}
	return cmd
}

// Mount is a bind mount from a host source to a container target.
type Mount struct {
	Source   string
	Target   string
	ReadOnly bool
}

func (m Mount) spec() string {
	s := fmt.Sprintf("type=bind,source=%s,target=%s", m.Source, m.Target)
	if m.ReadOnly {
		s += ",readonly"
	}
	return s
}

// RunOpts describes a `docker run` invocation.
type RunOpts struct {
	Image   string
	Name    string
	Labels  map[string]string
	Env     map[string]string
	Workdir string
	User    string
	Mounts  []Mount
	Network string // "" = default, "none", or a network name
	CapAdd  []string
	Detach  bool
	TTY     bool
	Remove  bool
	Command []string
}

// args assembles the deterministic argv for `docker run`. Map-valued flags
// (labels, env) are emitted in sorted-key order so identical opts always
// produce identical argv.
func (o RunOpts) args() []string {
	a := []string{"run"}
	if o.Detach {
		a = append(a, "-d")
	}
	if o.Remove {
		a = append(a, "--rm")
	}
	if o.TTY {
		a = append(a, "-it")
	}
	if o.Name != "" {
		a = append(a, "--name", o.Name)
	}
	for _, k := range sortedKeys(o.Labels) {
		a = append(a, "--label", k+"="+o.Labels[k])
	}
	for _, k := range sortedKeys(o.Env) {
		a = append(a, "-e", k+"="+o.Env[k])
	}
	if o.Workdir != "" {
		a = append(a, "-w", o.Workdir)
	}
	if o.User != "" {
		a = append(a, "-u", o.User)
	}
	if o.Network != "" {
		a = append(a, "--network", o.Network)
	}
	for _, c := range o.CapAdd {
		a = append(a, "--cap-add", c)
	}
	for _, m := range o.Mounts {
		a = append(a, "--mount", m.spec())
	}
	a = append(a, o.Image)
	a = append(a, o.Command...)
	return a
}

// RunDetached starts a container in the background and returns its ID.
func (r *Runner) RunDetached(opts RunOpts) (string, error) {
	opts.Detach = true
	return r.RunCapture(opts.args()...)
}

// Exec runs a command inside a running container. When tty is true the
// terminal is attached interactively (for `zellij attach`).
func (r *Runner) Exec(name string, args []string, tty bool) error {
	a := []string{"exec"}
	if tty {
		a = append(a, "-it")
	}
	a = append(a, name)
	a = append(a, args...)
	return r.Run(a...)
}

// Stop stops a running container.
func (r *Runner) Stop(name string) error {
	_, err := r.RunCapture("stop", name)
	return err
}

// Rm removes a container. When force is true, a running container is killed.
func (r *Runner) Rm(name string, force bool) error {
	a := []string{"rm"}
	if force {
		a = append(a, "-f")
	}
	a = append(a, name)
	_, err := r.RunCapture(a...)
	return err
}

// PsByLabel lists container names carrying the given label (running or not).
func (r *Runner) PsByLabel(label string) ([]string, error) {
	out, err := r.RunCapture("ps", "-a", "--filter", "label="+label, "--format", "{{.Names}}")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// ImageExists reports whether an image with the given tag is present locally.
func (r *Runner) ImageExists(tag string) bool {
	_, err := r.RunCapture("image", "inspect", tag)
	return err == nil
}

// Build builds an image from contextDir using the given dockerfile, tagging it.
func (r *Runner) Build(contextDir, dockerfile, tag string) error {
	return r.Run("build", "-f", dockerfile, "-t", tag, contextDir)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
