package sandbox

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amustafa/stackr/internal/docker"
)

func TestContentHash(t *testing.T) {
	a := contentHash([]byte("hello"))
	if len(a) != 12 {
		t.Fatalf("hash length = %d, want 12", len(a))
	}
	if a != contentHash([]byte("hello")) {
		t.Fatal("hash not deterministic")
	}
	if a == contentHash([]byte("world")) {
		t.Fatal("different input should differ")
	}
	// Domain separation: ["a","b"] must differ from ["ab"].
	if contentHash([]byte("a"), []byte("b")) == contentHash([]byte("ab")) {
		t.Fatal("parts should be domain-separated")
	}
}

func TestEmbeddedDockerfilePresent(t *testing.T) {
	if len(baseDockerfile()) == 0 {
		t.Fatal("embedded Dockerfile.base is empty")
	}
}

func TestEnsureProjectImageNoDockerfile(t *testing.T) {
	// No .stackr/sandbox/Dockerfile → returns baseTag unchanged, no docker call.
	got, err := EnsureProjectImage(&docker.Runner{}, t.TempDir(), ".stackr/sandbox/Dockerfile", "stackr-sandbox:base")
	if err != nil {
		t.Fatalf("EnsureProjectImage: %v", err)
	}
	if got != "stackr-sandbox:base" {
		t.Fatalf("expected baseTag passthrough, got %q", got)
	}
}

// TestEnsureProjectImageBuilds verifies the docker.Build plumbing end-to-end
// with a network-free `FROM scratch` image, then a cache hit on the second call.
func TestEnsureProjectImageBuilds(t *testing.T) {
	dr := &docker.Runner{}
	if !dr.Available() {
		t.Skip("docker not available")
	}
	root := t.TempDir()
	dfDir := filepath.Join(root, ".stackr", "sandbox")
	if err := os.MkdirAll(dfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Unique marker so the content hash (and thus the image tag) is unique to
	// this test run — avoids colliding with a stale image.
	marker := "stackr-test-" + contentHash([]byte(t.Name()), []byte(root))
	if err := os.WriteFile(filepath.Join(root, "marker"), []byte(marker), 0o644); err != nil {
		t.Fatal(err)
	}
	df := "FROM scratch\nCOPY marker /marker\n"
	if err := os.WriteFile(filepath.Join(dfDir, "Dockerfile"), []byte(df), 0o644); err != nil {
		t.Fatal(err)
	}

	tag, err := EnsureProjectImage(dr, root, ".stackr/sandbox/Dockerfile", "scratch")
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	t.Cleanup(func() { _, _ = dr.RunCapture("rmi", "-f", tag) })

	if !dr.ImageExists(tag) {
		t.Fatalf("image %q should exist after build", tag)
	}
	// Second call is a cache hit (same tag, no rebuild needed).
	tag2, err := EnsureProjectImage(dr, root, ".stackr/sandbox/Dockerfile", "scratch")
	if err != nil || tag2 != tag {
		t.Fatalf("second call should return same tag: %q %v", tag2, err)
	}
}
