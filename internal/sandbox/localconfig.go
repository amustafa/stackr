package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
)

// LocalConfig is the machine-specific, git-ignored sandbox config, stored at
// <main .git>/.stackr/sandbox.local.json. It holds absolute host paths that
// must never be shared or committed (they differ per machine).
type LocalConfig struct {
	CachePaths   []string `json:"cachePaths,omitempty"`   // extra host cache dirs to bind-mount
	ExtraMounts  []Mount  `json:"extraMounts,omitempty"`  // arbitrary host dirs to bind-mount
	PathMounts   []string `json:"pathMounts,omitempty"`   // host dirs bind-mounted AND added to PATH
	DockerSocket string   `json:"dockerSocket,omitempty"` // non-standard docker socket
}

// LoadLocalConfig reads the local config at path. A missing file yields an
// empty config and no error (the common case — most machines add nothing).
func LoadLocalConfig(path string) (*LocalConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &LocalConfig{}, nil
		}
		return nil, err
	}
	var c LocalConfig
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &c, nil
}

// Save writes the local config to path atomically.
func (c *LocalConfig) Save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(path, data)
}
