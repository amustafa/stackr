package cmd

import (
	"fmt"

	"strings"

	"github.com/amustafa/stackr/internal/engine"
	"github.com/amustafa/stackr/internal/store"
	"github.com/amustafa/stackr/internal/ui"
	"github.com/spf13/cobra"
)

var (
	sandboxFlagNetwork string
	sandboxFlagNoAttach bool
	sandboxRmDelete     bool
)

var sandboxCmd = &cobra.Command{
	Use:     "sandbox [branch] [-- <prompt>]",
	Aliases: []string{"sb"},
	Short:   "Run Claude with skip-permissions in a disposable Docker sandbox",
	Long: `Launch (or reuse) a sandboxed Claude session for a branch: a disposable
Docker container running claude --dangerously-skip-permissions on the branch's
worktree, with your ~/.claude mounted for config + session continuity.

Everything after -- is passed as the initial prompt.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		branch, prompt := splitSandboxArgs(cmd, args)
		if branch == "" {
			return fmt.Errorf("a branch name is required: sr sandbox <branch>")
		}
		return engine.SandboxRun(ctx, engine.SandboxRunOpts{
			Branch:  branch,
			Prompt:  prompt,
			Network: sandboxFlagNetwork,
			Attach:  !sandboxFlagNoAttach,
		})
	},
}

var sandboxAttachCmd = &cobra.Command{
	Use:   "attach [branch]",
	Short: "Attach to a running sandbox's session",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		branch := ""
		if len(args) > 0 {
			branch = args[0]
		} else {
			picked, err := pickSandbox()
			if err != nil {
				return err
			}
			branch = picked
		}
		return engine.SandboxAttach(ctx, branch)
	},
}

// pickSandbox shows the searchable picker over this repo's sandboxes.
func pickSandbox() (string, error) {
	infos, err := engine.SandboxList(ctx)
	if err != nil {
		return "", err
	}
	if len(infos) == 0 {
		return "", fmt.Errorf("no sandboxes running — launch one with: sr sandbox <branch>")
	}
	items := make([]ui.FilterItem, 0, len(infos))
	for _, in := range infos {
		detail := "running"
		if in.Status != nil {
			detail = string(in.Status.State)
			if in.Status.Reason != "" {
				detail += " — " + in.Status.Reason
			}
		}
		items = append(items, ui.FilterItem{Value: in.Branch, Label: in.Branch, Detail: detail})
	}
	return ui.FilterSelect("Attach to sandbox", items)
}

var sandboxLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List sandboxes for this repo",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		infos, err := engine.SandboxList(ctx)
		if err != nil {
			return err
		}
		if len(infos) == 0 {
			fmt.Println("No sandboxes running.")
			return nil
		}
		for _, in := range infos {
			state := "running"
			if in.Status != nil {
				state = string(in.Status.State)
			}
			line := fmt.Sprintf("  %-30s %s", in.Branch, state)
			if in.Status != nil && in.Status.Reason != "" {
				line += "  — " + in.Status.Reason
			}
			fmt.Println(line)
		}
		return nil
	},
}

var sandboxStopCmd = &cobra.Command{
	Use:   "stop <branch>",
	Short: "Stop a sandbox (keeps the container; resume with a relaunch)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		return engine.SandboxStop(ctx, args[0])
	},
}

var sandboxRmCmd = &cobra.Command{
	Use:   "rm <branch>",
	Short: "Remove a sandbox container (worktree kept unless --delete)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		return engine.SandboxRm(ctx, args[0], sandboxRmDelete)
	},
}

var sandboxConfigAI bool

var sandboxConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Show/edit sandbox config (TUI), or --ai to have Claude help",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		if sandboxConfigAI {
			return engine.SandboxConfigAI(ctx)
		}
		return runSandboxConfigTUI()
	},
}

func runSandboxConfigTUI() error {
	cfg, err := ctx.Store.ReadConfig()
	if err != nil {
		return err
	}
	r := cfg.Sandbox.Resolved()
	extras := ""
	if cfg.Sandbox != nil {
		extras = strings.Join(cfg.Sandbox.FirewallAllowlist, ",")
	}
	fields := []ui.FormField{
		{Key: "network", Label: "Network (allowlist|full)", Kind: ui.FieldText, Value: r.Network, Required: true},
		{Key: "baseImage", Label: "Base image", Kind: ui.FieldText, Value: r.BaseImage},
		{Key: "dockerfilePath", Label: "Per-project Dockerfile", Kind: ui.FieldText, Value: r.DockerfilePath},
		{Key: "binDir", Label: "Sandbox bin dir", Kind: ui.FieldText, Value: r.BinDir},
		{Key: "watchScope", Label: "Watch scope (project|all)", Kind: ui.FieldText, Value: r.WatchScope},
		{Key: "allowlist", Label: "Extra firewall domains (comma-sep)", Kind: ui.FieldText, Value: extras},
		{Key: "caches", Label: "Mount caches", Kind: ui.FieldToggle, Toggle: r.CachesEnabled()},
	}
	res, err := ui.Form("Sandbox config", fields)
	if err != nil {
		return err
	}
	sc := &store.SandboxConfig{
		Network:        res.Values["network"],
		BaseImage:      res.Values["baseImage"],
		DockerfilePath: res.Values["dockerfilePath"],
		BinDir:         res.Values["binDir"],
		WatchScope:     res.Values["watchScope"],
	}
	if v := strings.TrimSpace(res.Values["allowlist"]); v != "" {
		for _, d := range strings.Split(v, ",") {
			if d = strings.TrimSpace(d); d != "" {
				sc.FirewallAllowlist = append(sc.FirewallAllowlist, d)
			}
		}
	}
	caches := res.Toggles["caches"]
	sc.Caches = &caches
	cfg.Sandbox = sc
	if err := ctx.Store.WriteConfig(cfg); err != nil {
		return err
	}
	fmt.Println("Sandbox config saved.")
	return nil
}

var sandboxWatchNotify bool

var sandboxWatchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Live dashboard of sandboxes (or --notify for desktop notifications)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		if sandboxWatchNotify {
			return engine.SandboxNotify(ctx)
		}
		fetch := func() []ui.WatchRow {
			infos, err := engine.SandboxList(ctx)
			if err != nil {
				return nil
			}
			rows := make([]ui.WatchRow, 0, len(infos))
			for _, in := range infos {
				row := ui.WatchRow{Branch: in.Branch, State: "running"}
				if in.Status != nil {
					row.State = string(in.Status.State)
					row.Reason = in.Status.Reason
					row.Awaiting = in.Status.State.Awaiting()
				}
				rows = append(rows, row)
			}
			return rows
		}
		return ui.RunWatch("Sandboxes", fetch, engine.SandboxAttachCommand)
	},
}

var sandboxAwaitingCmd = &cobra.Command{
	Use:   "awaiting",
	Short: "Print the number of sandboxes awaiting input (for prompts)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ctx.RequireInit(); err != nil {
			return err
		}
		n, err := engine.SandboxAwaitingCount(ctx)
		if err != nil {
			return err
		}
		fmt.Println(n)
		return nil
	},
}

// splitSandboxArgs separates the branch from the post-`--` prompt.
func splitSandboxArgs(cmd *cobra.Command, args []string) (branch, prompt string) {
	dashIdx := cmd.ArgsLenAtDash()
	if dashIdx == -1 {
		if len(args) > 0 {
			return args[0], ""
		}
		return "", ""
	}
	before := args[:dashIdx]
	after := args[dashIdx:]
	if len(before) > 0 {
		branch = before[0]
	}
	prompt = joinArgs(after)
	return branch, prompt
}

func joinArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}

func init() {
	sandboxCmd.Flags().StringVar(&sandboxFlagNetwork, "network", "", "network mode: allowlist (default) | full")
	sandboxCmd.Flags().BoolVar(&sandboxFlagNoAttach, "no-attach", false, "launch without attaching")
	sandboxRmCmd.Flags().BoolVar(&sandboxRmDelete, "delete", false, "also remove the worktree and branch")

	sandboxWatchCmd.Flags().BoolVar(&sandboxWatchNotify, "notify", false, "headless: desktop notifications on transition to awaiting")
	sandboxConfigCmd.Flags().BoolVar(&sandboxConfigAI, "ai", false, "let Claude help manage the config")
	sandboxCmd.AddCommand(sandboxConfigCmd)

	sandboxCmd.AddCommand(sandboxAttachCmd)
	sandboxCmd.AddCommand(sandboxLsCmd)
	sandboxCmd.AddCommand(sandboxStopCmd)
	sandboxCmd.AddCommand(sandboxRmCmd)
	sandboxCmd.AddCommand(sandboxWatchCmd)
	sandboxCmd.AddCommand(sandboxAwaitingCmd)
	rootCmd.AddCommand(sandboxCmd)
}
