package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/internal/docker"
	"github.com/amustafa/stackr/internal/sandbox"
)

const sandboxLabel = "stackr.sandbox"
const sandboxBranchLabel = "stackr.sandbox.branch"

// launchSpec is the fully-resolved input to a sandbox launch. Everything here is
// concrete (paths canonicalized, config defaulted) so the pure builders below
// are trivially testable.
type launchSpec struct {
	Branch       string
	WorktreePath string // canonical, == container cwd (ADR-0008)
	GitCommonDir string // shared main .git
	Home         string
	ClaudeDir    string // <home>/.claude
	ClaudeJSON   string // <home>/.claude.json  (REQUIRED — see ADR-0008)
	User         string // uid:gid
	RepoHash     string // scopes `ls` to a repo
	Image        string
	Network      string // "allowlist" | "full"
	Allowlist    []string
	CachePaths   []string
	BinDir       string // absolute
	PathMounts   []string
	ExtraMounts  []sandbox.Mount
	Prompt       string
	SettingsFile string // sandbox-only claude --settings file (Phase 6); "" to omit
}

// buildMounts assembles the bind mounts — all at identical host paths so git
// resolves and the Claude project slug matches the host (ADR-0008).
func buildMounts(s launchSpec) []docker.Mount {
	same := func(p string) docker.Mount { return docker.Mount{Source: p, Target: p} }
	m := []docker.Mount{
		same(s.WorktreePath),
		same(s.GitCommonDir),
		same(s.ClaudeDir),
		same(s.ClaudeJSON),
	}
	for _, c := range s.CachePaths {
		m = append(m, same(c))
	}
	if s.BinDir != "" {
		m = append(m, same(s.BinDir))
	}
	for _, p := range s.PathMounts {
		m = append(m, same(p))
	}
	for _, e := range s.ExtraMounts {
		m = append(m, docker.Mount{Source: e.Source, Target: e.Target, ReadOnly: e.ReadOnly})
	}
	return dedupeMounts(m)
}

func dedupeMounts(in []docker.Mount) []docker.Mount {
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, m := range in {
		if m.Source == "" || seen[m.Target] {
			continue
		}
		seen[m.Target] = true
		out = append(out, m)
	}
	return out
}

// buildPATH prepends the bin dir and PATH mounts to the image default PATH.
func buildPATH(s launchSpec) string {
	parts := make([]string, 0, len(s.PathMounts)+2)
	if s.BinDir != "" {
		parts = append(parts, s.BinDir)
	}
	parts = append(parts, s.PathMounts...)
	parts = append(parts, "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	return strings.Join(parts, ":")
}

// xdgBase is a writable location for XDG dirs. HOME is bind-mount-populated and
// root-owned (so unwritable by the sandbox user) except for ~/.claude; tools
// like zellij need writable cache/config/runtime dirs, so we redirect XDG here.
// Claude is unaffected — it uses the explicit ~/.claude and ~/.claude.json.
const xdgBase = "/tmp/sr-xdg"

// buildEnv assembles the container environment.
func buildEnv(s launchSpec) map[string]string {
	env := map[string]string{
		"HOME":       s.Home,
		"SR_SANDBOX": s.Branch,
		"PATH":       buildPATH(s),
		// Redirect XDG + git global config to writable paths (see xdgBase).
		"XDG_CACHE_HOME":    xdgBase + "/cache",
		"XDG_CONFIG_HOME":   xdgBase + "/config",
		"XDG_DATA_HOME":     xdgBase + "/data",
		"XDG_STATE_HOME":    xdgBase + "/state",
		"XDG_RUNTIME_DIR":   xdgBase + "/run",
		"GIT_CONFIG_GLOBAL": xdgBase + "/gitconfig",
	}
	return env
}

// xdgMkdirs is the shell snippet that pre-creates the writable XDG dirs.
func xdgMkdirs() string {
	return fmt.Sprintf("mkdir -p %s/cache %s/config %s/data %s/state %s/run",
		xdgBase, xdgBase, xdgBase, xdgBase, xdgBase)
}

// shellQuote single-quotes a string for safe embedding in an sh -c command.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// claudeCommand is the command run inside the zellij session.
func claudeCommand(s launchSpec) []string {
	cmd := []string{"claude"}
	if s.SettingsFile != "" {
		cmd = append(cmd, "--settings", s.SettingsFile)
	}
	cmd = append(cmd, "--dangerously-skip-permissions")
	if s.Prompt != "" {
		cmd = append(cmd, s.Prompt)
	}
	return cmd
}

// buildLayout renders a zellij KDL layout that launches Claude in a single pane.
// Args go on ONE `args` node with multiple string values (zellij KDL) — repeated
// `args` lines are invalid and cause zellij to launch nothing.
func buildLayout(s launchSpec) string {
	cmd := claudeCommand(s)
	var b strings.Builder
	b.WriteString("layout {\n")
	fmt.Fprintf(&b, "    pane command=%q", cmd[0])
	if len(cmd) > 1 {
		b.WriteString(" {\n        args")
		for _, a := range cmd[1:] {
			fmt.Fprintf(&b, " %q", a)
		}
		b.WriteString("\n    }")
	}
	b.WriteString("\n}\n")
	return b.String()
}

func sandboxContainerName(branch string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, branch)
	if len(safe) > 40 {
		safe = safe[:40]
	}
	h := sha256.Sum256([]byte(branch))
	return "sr-sb-" + safe + "-" + hex.EncodeToString(h[:])[:6]
}

func repoHash(mainRoot string) string {
	h := sha256.Sum256([]byte(mainRoot))
	return hex.EncodeToString(h[:])[:12]
}

// resolveLaunchSpec gathers everything needed to launch a sandbox for a branch.
func resolveLaunchSpec(c *context.Context, branch, networkOverride, prompt string) (launchSpec, error) {
	gitCommon, err := absGitCommonDir(c)
	if err != nil {
		return launchSpec{}, err
	}
	mainRoot := sandbox.MainRoot(gitCommon)

	home, err := sandbox.Home()
	if err != nil {
		return launchSpec{}, err
	}

	cfg, err := c.Store.ReadConfig()
	if err != nil {
		return launchSpec{}, err
	}
	sc := cfg.Sandbox.Resolved()

	network := sc.Network
	if networkOverride != "" {
		network = networkOverride
	}

	var cachePaths []string
	binDir := ""
	var pathMounts []string
	var extra []sandbox.Mount
	if local, err := sandbox.LoadLocalConfig(filepath.Join(c.Store.Root(), "sandbox.local.json")); err == nil {
		cachePaths = existingDirs(local.CachePaths)
		pathMounts = existingDirs(local.PathMounts)
		extra = local.ExtraMounts
	}
	if sc.CachesEnabled() {
		cachePaths = append(cachePaths, existingDirs(defaultCacheDirs(home))...)
	}
	if bd := filepath.Join(mainRoot, sc.BinDir); dirExists(bd) {
		binDir = bd
	}

	return launchSpec{
		Branch:       branch,
		WorktreePath: sandbox.WorktreePath(mainRoot, branch),
		GitCommonDir: gitCommon,
		Home:         home,
		ClaudeDir:    filepath.Join(home, ".claude"),
		ClaudeJSON:   filepath.Join(home, ".claude.json"),
		User:         sandbox.ProcessUser(),
		RepoHash:     repoHash(mainRoot),
		Image:        sc.BaseImage,
		Network:      network,
		Allowlist:    sc.FirewallAllowlist,
		CachePaths:   cachePaths,
		BinDir:       binDir,
		PathMounts:   pathMounts,
		ExtraMounts:  extra,
		Prompt:       prompt,
	}, nil
}

func defaultCacheDirs(home string) []string {
	return []string{
		filepath.Join(home, "go", "pkg", "mod"),
		filepath.Join(home, ".cache", "go-build"),
		filepath.Join(home, ".npm"),
	}
}

func existingDirs(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if dirExists(p) {
			out = append(out, p)
		}
	}
	return out
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// absGitCommonDir returns the shared .git directory as a canonical absolute
// path. `git rev-parse --git-common-dir` may return a relative path (e.g.
// ".git"), which we resolve against the repo root before canonicalizing —
// Docker mounts and the container workdir must be absolute.
func absGitCommonDir(c *context.Context) (string, error) {
	gitCommon, err := c.Git.GitCommonDir()
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(gitCommon) {
		gitCommon = filepath.Join(c.Git.Dir, gitCommon)
	}
	if resolved, err := filepath.EvalSymlinks(gitCommon); err == nil {
		return resolved, nil
	}
	return filepath.Clean(gitCommon), nil
}
