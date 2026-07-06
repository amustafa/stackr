package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/amustafa/stackr/internal/context"
)

// SandboxConfigAI spawns a scoped Claude session to help manage the sandbox
// config, mirroring the submit --ai pattern (ADR-0004): current config is fed
// as JSON context, tools are limited to reading/editing config.
func SandboxConfigAI(c *context.Context) error {
	if _, err := exec.LookPath("claude"); err != nil {
		return fmt.Errorf("claude CLI not found — install it from https://claude.ai/code")
	}
	cfg, err := c.Store.ReadConfig()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg.Sandbox.Resolved(), "", "  ")
	if err != nil {
		return err
	}

	goal := "/goal the sandbox configuration reflects the user's stated intent. " +
		"Read the current config from stdin, discuss changes, and apply them with " +
		"`sr sandbox config` or by editing config values."
	args := []string{
		"--bare",
		"-p", goal,
		"--allowedTools", "Read,Edit,Bash(sr sandbox config *)",
		"--append-system-prompt", sandboxConfigSystemPrompt(),
	}
	cmd := exec.Command("claude", args...)
	cmd.Stdin = strings.NewReader(string(data))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if !c.Quiet {
		fmt.Println("Launching Claude to help manage sandbox config…")
	}
	return cmd.Run()
}

// sandboxConfigSystemPrompt is the appended system prompt for the --ai flow.
// (Intentionally minimal — refine the guidance for your workflow.)
func sandboxConfigSystemPrompt() string {
	return strings.TrimSpace(`
You are helping manage the sr sandbox configuration for this repository.
The current effective config is provided as JSON on stdin. It has three tiers:
portable (network, base image, firewall allowlist, caches, bin dir, watch scope),
machine-specific (host cache paths, extra mounts, PATH mounts), and auto-derived
values you never set. Only change what the user asks for. Prefer the egress
allowlist over full network unless the user opts out. Confirm before writing.`)
}
