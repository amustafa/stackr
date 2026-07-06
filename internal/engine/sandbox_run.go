package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/docker"
	"github.com/amustafa/stackr/internal/sandbox"
	"github.com/amustafa/stackr/internal/store"
)

// SandboxRunOpts holds options for launching/attaching a sandbox.
type SandboxRunOpts struct {
	Branch  string
	Prompt  string
	Network string // "" = config default; "full" | "allowlist" override
	Attach  bool   // attach after launch (default true for `sr sandbox`)
}

// SandboxRun launches (or reuses) the sandbox for a branch and attaches to it.
func SandboxRun(c *context.Context, opts SandboxRunOpts) error {
	dr := &docker.Runner{}
	if !dr.Available() {
		return fmt.Errorf("docker not found — install Docker to use sr sandbox")
	}
	if opts.Branch == "" {
		return fmt.Errorf("a branch name is required")
	}

	spec, err := resolveLaunchSpec(c, opts.Branch, opts.Network, opts.Prompt)
	if err != nil {
		return err
	}
	name := sandboxContainerName(opts.Branch)

	// Reuse a running container if present.
	running, err := containerRunning(dr, name)
	if err != nil {
		return err
	}
	if !running {
		if err := ensureWorktree(c, spec.WorktreePath, opts.Branch); err != nil {
			return err
		}
		// Recompute the worktree path now that it exists (canonicalizes).
		spec.WorktreePath = sandbox.WorktreePath(filepath.Dir(spec.GitCommonDir), opts.Branch)

		if err := ensureImages(c, dr, &spec); err != nil {
			return err
		}
		if err := launchContainer(c, dr, spec, name); err != nil {
			return err
		}
		if !c.Quiet {
			fmt.Printf("Launched sandbox %q (container %s)\n", opts.Branch, name)
		}
	} else if !c.Quiet {
		fmt.Printf("Reusing running sandbox %q\n", opts.Branch)
	}

	if opts.Attach {
		return sandboxAttach(dr, name, opts.Branch)
	}
	if !c.Quiet {
		fmt.Printf("Attach with: sr sandbox attach %s\n", opts.Branch)
	}
	return nil
}

// SandboxAttach connects to a running sandbox's zellij session.
func SandboxAttach(c *context.Context, branch string) error {
	dr := &docker.Runner{}
	if !dr.Available() {
		return fmt.Errorf("docker not found")
	}
	return sandboxAttach(dr, sandboxContainerName(branch), branch)
}

// SandboxStop stops a sandbox container but keeps it (live session resumable).
func SandboxStop(c *context.Context, branch string) error {
	dr := &docker.Runner{}
	name := sandboxContainerName(branch)
	if err := dr.Stop(name); err != nil {
		return err
	}
	if !c.Quiet {
		fmt.Printf("Stopped sandbox %q (docker start to resume the live session)\n", branch)
	}
	return nil
}

// SandboxRm removes a sandbox container. The worktree/branch are kept unless
// delete is true.
func SandboxRm(c *context.Context, branch string, delete bool) error {
	dr := &docker.Runner{}
	name := sandboxContainerName(branch)
	if err := dr.Rm(name, true); err != nil {
		return err
	}
	_ = sandbox.RemoveManifest(sandboxesDir(c), branch)
	_ = sandbox.RemoveStatus(sandboxesDir(c), branch)
	if delete {
		if err := WorktreeRemove(c, WorktreeRemoveOpts{Name: branch, Delete: true}); err != nil {
			return fmt.Errorf("container removed but worktree delete failed: %w", err)
		}
	}
	if !c.Quiet {
		fmt.Printf("Removed sandbox %q\n", branch)
	}
	return nil
}

// SandboxInfo pairs a manifest with its live status for listing.
type SandboxInfo struct {
	Branch  string
	Running bool
	Status  *sandbox.Status
}

// SandboxList reports the sandboxes for this repo (containers labeled for it).
func SandboxList(c *context.Context) ([]SandboxInfo, error) {
	dr := &docker.Runner{}
	if !dr.Available() {
		return nil, fmt.Errorf("docker not found")
	}
	gitCommon, err := absGitCommonDir(c)
	if err != nil {
		return nil, err
	}
	rh := repoHash(sandbox.MainRoot(gitCommon))

	names, err := dr.PsByLabel(sandboxLabel + "=" + rh)
	if err != nil {
		return nil, err
	}
	statuses, _ := sandbox.ListStatuses(sandboxesDir(c))
	byBranch := make(map[string]*sandbox.Status, len(statuses))
	for _, s := range statuses {
		byBranch[s.Branch] = s
	}

	var out []SandboxInfo
	for _, n := range names {
		branch := containerBranch(dr, n)
		out = append(out, SandboxInfo{Branch: branch, Running: true, Status: byBranch[branch]})
	}
	return out, nil
}

// --- helpers ---

func sandboxesDir(c *context.Context) string {
	return filepath.Join(c.Store.Root(), "sandboxes")
}

func ensureWorktree(c *context.Context, worktreePath, branch string) error {
	if dirExists(worktreePath) {
		return nil
	}
	return WorktreeAdd(c, WorktreeAddOpts{Name: branch})
}

func ensureImages(c *context.Context, dr *docker.Runner, spec *launchSpec) error {
	base, err := sandbox.EnsureBaseImage(dr)
	if err != nil {
		return fmt.Errorf("building base image: %w", err)
	}
	mainRoot := sandbox.MainRoot(spec.GitCommonDir)
	cfg, _ := c.Store.ReadConfig()
	img, err := sandbox.EnsureProjectImage(dr, mainRoot, cfg.Sandbox.Resolved().DockerfilePath, base)
	if err != nil {
		return fmt.Errorf("building project image: %w", err)
	}
	spec.Image = img
	return nil
}

func launchContainer(c *context.Context, dr *docker.Runner, spec launchSpec, name string) error {
	sbDir := filepath.Join(spec.GitCommonDir, ".stackr", "sandboxes")
	if err := os.MkdirAll(sbDir, 0o755); err != nil {
		return err
	}
	// Layout + firewall script live under the shared .git (mounted at the same
	// path), so the container reads them at their host paths.
	layoutPath := filepath.Join(sbDir, sandbox.EncodeBranch(spec.Branch)+".layout.kdl")
	if err := os.WriteFile(layoutPath, []byte(buildLayout(spec)), 0o644); err != nil {
		return err
	}

	// Inner command: ensure writable XDG dirs exist, then exec zellij with the
	// claude-launching layout. Run via `sh -c` so the mkdir happens in-container.
	inner := fmt.Sprintf("%s; exec zellij -s %s -n %s",
		xdgMkdirs(), shellQuote(spec.Branch), shellQuote(layoutPath))
	command := []string{"sh", "-c", inner}
	var capAdd []string
	env := buildEnv(spec)

	// Mount the current sr binary read-only rather than baking it into the
	// image, so the sandbox always runs this exact sr version.
	mounts := buildMounts(spec)
	if srBin, err := os.Executable(); err == nil {
		if resolved, rerr := filepath.EvalSymlinks(srBin); rerr == nil {
			srBin = resolved
		}
		mounts = append(mounts, docker.Mount{Source: srBin, Target: "/usr/local/bin/sr", ReadOnly: true})
	}
	network := "" // docker default (full)
	if spec.Network == store.SandboxNetworkAllowlist {
		fwPath := filepath.Join(sbDir, "firewall-init.sh")
		if err := os.WriteFile(fwPath, sandbox.FirewallScript(), 0o755); err != nil {
			return err
		}
		env["SR_ALLOWLIST"] = strings.Join(spec.Allowlist, ",")
		capAdd = []string{"NET_ADMIN"}
		command = append([]string{"sh", fwPath}, command...)
	}

	runOpts := docker.RunOpts{
		Image:   spec.Image,
		Name:    name,
		Labels:  map[string]string{sandboxLabel: spec.RepoHash, sandboxBranchLabel: spec.Branch},
		Env:     env,
		Workdir: spec.WorktreePath,
		User:    spec.User,
		Mounts:  mounts,
		Network: network,
		CapAdd:  capAdd,
		TTY:     true, // zellij PID1 needs a TTY; combined with -d => -dit
		Command: command,
	}
	id, err := dr.RunDetached(runOpts)
	if err != nil {
		return err
	}

	manifestMounts := make([]sandbox.Mount, 0, len(runOpts.Mounts))
	for _, m := range runOpts.Mounts {
		manifestMounts = append(manifestMounts, sandbox.Mount{Source: m.Source, Target: m.Target, ReadOnly: m.ReadOnly})
	}
	return sandbox.WriteManifest(sandboxesDir(c), &sandbox.Manifest{
		Branch:    spec.Branch,
		Image:     spec.Image,
		Container: id,
		Mounts:    manifestMounts,
		Command:   command,
	})
}

func sandboxAttach(dr *docker.Runner, name, branch string) error {
	running, err := containerRunning(dr, name)
	if err != nil {
		return err
	}
	if !running {
		// Try starting a stopped container before giving up.
		if _, startErr := dr.RunCapture("start", name); startErr != nil {
			return fmt.Errorf("sandbox %q is not running (launch it with: sr sandbox %s)", branch, branch)
		}
	}
	return dr.Exec(name, []string{"zellij", "attach", "--create", branch}, true)
}

func containerRunning(dr *docker.Runner, name string) (bool, error) {
	out, err := dr.RunCapture("ps", "--filter", "name=^"+name+"$", "--filter", "status=running", "--format", "{{.Names}}")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == name, nil
}

func containerBranch(dr *docker.Runner, name string) string {
	out, err := dr.RunCapture("inspect", "-f", "{{ index .Config.Labels \""+sandboxBranchLabel+"\" }}", name)
	if err != nil {
		return name
	}
	return strings.TrimSpace(out)
}
