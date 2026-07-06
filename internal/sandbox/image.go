package sandbox

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/amustafa/stackr/internal/docker"
	"github.com/amustafa/stackr/internal/store"
)

//go:embed assets/Dockerfile.base assets/firewall-init.sh
var assets embed.FS

func baseDockerfile() []byte {
	return mustAsset("assets/Dockerfile.base")
}

// FirewallScript returns the embedded egress-allowlist init script (ADR-0012).
func FirewallScript() []byte {
	return mustAsset("assets/firewall-init.sh")
}

func mustAsset(name string) []byte {
	data, err := assets.ReadFile(name)
	if err != nil {
		// Embedded at build time; a read failure is a programming error.
		panic("sandbox: embedded asset missing: " + name + ": " + err.Error())
	}
	return data
}

// contentHash returns a short, stable hash of the given bytes — used to
// content-address images so an unchanged definition is a cache hit and a change
// forces a rebuild.
func contentHash(parts ...[]byte) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write(p)
		h.Write([]byte{0}) // domain separator between parts
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// EnsureBaseImage builds the shared base image if absent or stale, and returns
// the stable tag (store.DefaultBaseImage). Staleness is global and needs no
// bookkeeping file: the image is content-addressed by its Dockerfile
// (stackr-sandbox:base-<hash>) with the stable :base tag as an alias, so an
// unchanged Dockerfile is a cache hit for every repo. sr is not baked in — it
// is bind-mounted at launch — so an sr rebuild never invalidates the image.
func EnsureBaseImage(dr *docker.Runner) (string, error) {
	stable := store.DefaultBaseImage
	dockerfile := baseDockerfile()
	contentTag := stable + "-" + contentHash(dockerfile)

	if dr.ImageExists(contentTag) {
		_ = dr.Tag(contentTag, stable) // keep the stable alias current
		return stable, nil
	}

	ctxDir, err := os.MkdirTemp("", "stackr-sandbox-base-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(ctxDir)

	dfPath := filepath.Join(ctxDir, "Dockerfile")
	if err := os.WriteFile(dfPath, dockerfile, 0o644); err != nil {
		return "", err
	}
	if err := dr.Build(ctxDir, dfPath, contentTag); err != nil {
		return "", err
	}
	if err := dr.Tag(contentTag, stable); err != nil {
		return "", err
	}
	return stable, nil
}

// EnsureProjectImage builds the optional per-project image layer if the repo
// ships one at dockerfilePath (relative to mainRoot). If absent, it returns
// baseTag unchanged. The project image is content-addressed by its Dockerfile,
// so all sandboxes of the repo share one cached build.
func EnsureProjectImage(dr *docker.Runner, mainRoot, dockerfilePath, baseTag string) (string, error) {
	full := filepath.Join(mainRoot, dockerfilePath)
	content, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return baseTag, nil
		}
		return "", err
	}
	tag := "stackr-sandbox:proj-" + contentHash([]byte(baseTag), content)
	if dr.ImageExists(tag) {
		return tag, nil
	}
	// Build with the repo root as context so the project Dockerfile can COPY
	// repo files if it wants.
	if err := dr.Build(mainRoot, full, tag); err != nil {
		return "", err
	}
	return tag, nil
}
