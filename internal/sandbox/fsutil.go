package sandbox

import (
	"os"
	"path/filepath"
	"strings"
)

// EncodeBranch makes a branch name safe as a flat filename. Git ref rules
// forbid every filesystem-hostile character except "/", so encoding just "/"
// as "%2F" is sufficient and reversible — and trivial to reproduce in the
// Phase-6 bash hook (${SR_SANDBOX//\//%2F}).
func EncodeBranch(branch string) string {
	return strings.ReplaceAll(branch, "/", "%2F")
}

// writeFileAtomic writes data to path via a temp file + rename, so a concurrent
// reader (the host watching status files) never observes a partial write.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
