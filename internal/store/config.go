package store

// Config holds the stackr configuration persisted to config.json.
type Config struct {
	Trunk   string         `json:"trunk"`
	Remote  string         `json:"remote"`
	Sandbox *SandboxConfig `json:"sandbox,omitempty"`
}

// SandboxConfig is the portable (shared, mergeable) sandbox settings. Only
// human-chosen values live here; machine-specific paths live in the git-ignored
// local config, and everything else is auto-derived. See spec 5 / ADR-0012.
type SandboxConfig struct {
	Network           string   `json:"network,omitempty"`           // "allowlist" (default) | "full"
	BaseImage         string   `json:"baseImage,omitempty"`         //
	DockerfilePath    string   `json:"dockerfilePath,omitempty"`    // per-project image layer
	FirewallAllowlist []string `json:"firewallAllowlist,omitempty"` // extra egress domains
	Caches            *bool    `json:"caches,omitempty"`            // nil = default (on)
	PromptTemplate    string   `json:"promptTemplate,omitempty"`
	BinDir            string   `json:"binDir,omitempty"`     // repo-relative bin dir added to PATH
	WatchScope        string   `json:"watchScope,omitempty"` // "project" (default) | "all"
}

// Sandbox default values.
const (
	SandboxNetworkAllowlist = "allowlist"
	SandboxNetworkFull      = "full"
	DefaultBaseImage        = "stackr-sandbox:base"
	DefaultDockerfilePath   = ".stackr/sandbox/Dockerfile"
	DefaultBinDir           = ".stackr/sandbox/bin"
	WatchScopeProject       = "project"
	WatchScopeAll           = "all"
)

// DefaultFirewallAllowlist is the base egress allowlist (ADR-0012): the
// Anthropic API, GitHub, and common package registries.
var DefaultFirewallAllowlist = []string{
	"api.anthropic.com",
	"github.com",
	"api.github.com",
	"codeload.github.com",
	"objects.githubusercontent.com",
	"proxy.golang.org",
	"sum.golang.org",
	"registry.npmjs.org",
	"pypi.org",
	"files.pythonhosted.org",
}

// Resolved returns the effective config with defaults applied. The stored
// SandboxConfig may be nil or hold only overrides; this fills the blanks so
// callers never branch on unset fields. FirewallAllowlist is the defaults plus
// any configured extras (deduped).
func (c *SandboxConfig) Resolved() SandboxConfig {
	out := SandboxConfig{
		Network:        SandboxNetworkAllowlist,
		BaseImage:      DefaultBaseImage,
		DockerfilePath: DefaultDockerfilePath,
		BinDir:         DefaultBinDir,
		WatchScope:     WatchScopeProject,
	}
	if c != nil {
		if c.Network != "" {
			out.Network = c.Network
		}
		if c.BaseImage != "" {
			out.BaseImage = c.BaseImage
		}
		if c.DockerfilePath != "" {
			out.DockerfilePath = c.DockerfilePath
		}
		if c.BinDir != "" {
			out.BinDir = c.BinDir
		}
		if c.WatchScope != "" {
			out.WatchScope = c.WatchScope
		}
		out.PromptTemplate = c.PromptTemplate
		out.Caches = c.Caches
	}
	out.FirewallAllowlist = mergeAllowlist(DefaultFirewallAllowlist, sandboxExtraAllowlist(c))
	return out
}

// CachesEnabled reports the effective caches setting (default on).
func (c SandboxConfig) CachesEnabled() bool {
	return c.Caches == nil || *c.Caches
}

func sandboxExtraAllowlist(c *SandboxConfig) []string {
	if c == nil {
		return nil
	}
	return c.FirewallAllowlist
}

func mergeAllowlist(base, extra []string) []string {
	seen := make(map[string]bool, len(base)+len(extra))
	out := make([]string, 0, len(base)+len(extra))
	for _, list := range [][]string{base, extra} {
		for _, d := range list {
			if d == "" || seen[d] {
				continue
			}
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}
