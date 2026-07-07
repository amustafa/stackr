package store

import (
	"slices"
	"testing"
)

func TestSandboxConfigResolvedDefaults(t *testing.T) {
	var c *SandboxConfig // nil — nothing configured
	got := c.Resolved()
	if got.Network != SandboxNetworkAllowlist {
		t.Errorf("default network = %q, want allowlist", got.Network)
	}
	if got.BaseImage != DefaultBaseImage || got.BinDir != DefaultBinDir || got.WatchScope != WatchScopeProject {
		t.Errorf("defaults not applied: %+v", got)
	}
	if !got.CachesEnabled() {
		t.Error("caches should default on")
	}
	if !slices.Equal(got.FirewallAllowlist, DefaultFirewallAllowlist) {
		t.Errorf("allowlist = %v, want defaults", got.FirewallAllowlist)
	}
}

func TestSandboxConfigResolvedOverrides(t *testing.T) {
	off := false
	c := &SandboxConfig{
		Network:           SandboxNetworkFull,
		BaseImage:         "custom:img",
		Caches:            &off,
		FirewallAllowlist: []string{"example.com", "github.com"}, // github.com is a dup of default
	}
	got := c.Resolved()
	if got.Network != SandboxNetworkFull || got.BaseImage != "custom:img" {
		t.Errorf("overrides not applied: %+v", got)
	}
	if got.CachesEnabled() {
		t.Error("caches explicitly off should stay off")
	}
	if !slices.Contains(got.FirewallAllowlist, "example.com") {
		t.Error("extra allowlist domain missing")
	}
	// github.com must appear exactly once (deduped against the default list).
	count := 0
	for _, d := range got.FirewallAllowlist {
		if d == "github.com" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("github.com appears %d times, want 1 (deduped)", count)
	}
}
