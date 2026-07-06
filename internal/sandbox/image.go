package sandbox

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/amustafa/stackr/internal/docker"
	"github.com/amustafa/stackr/internal/store"
)

//go:embed assets/Dockerfile.base
var assets embed.FS

func baseDockerfile() []byte {
	data, err := assets.ReadFile("assets/Dockerfile.base")
	if err != nil {
		// Embedded at build time; a read failure is a programming error.
		panic("sandbox: embedded Dockerfile.base missing: " + err.Error())
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
// its tag (store.DefaultBaseImage). It is a cache hit — no docker build — when
// the image exists and the embedded Dockerfile hash matches the last build,
// recorded at <stackrDir>/sandbox-base.hash. srBinary is the path to the sr
// executable to bake in (typically os.Executable()).
func EnsureBaseImage(dr *docker.Runner, stackrDir, srBinary string) (string, error) {
	tag := store.DefaultBaseImage
	dockerfile := baseDockerfile()
	want := contentHash(dockerfile)
	hashFile := filepath.Join(stackrDir, "sandbox-base.hash")

	if dr.ImageExists(tag) && readHash(hashFile) == want {
		return tag, nil
	}

	ctxDir, err := os.MkdirTemp("", "stackr-sandbox-base-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(ctxDir)

	if err := os.WriteFile(filepath.Join(ctxDir, "Dockerfile"), dockerfile, 0o644); err != nil {
		return "", err
	}
	if err := copyFile(srBinary, filepath.Join(ctxDir, "sr"), 0o755); err != nil {
		return "", fmt.Errorf("staging sr binary into build context: %w", err)
	}
	if err := dr.Build(ctxDir, filepath.Join(ctxDir, "Dockerfile"), tag); err != nil {
		return "", err
	}
	if err := os.MkdirAll(stackrDir, 0o755); err != nil {
		return "", err
	}
	_ = os.WriteFile(hashFile, []byte(want), 0o644)
	return tag, nil
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

func readHash(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Chmod(mode)
}
