package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/amustafa/stackr/internal/engine"
)

// handleNavigateResult reports a worktree switch so the shell hook can cd.
//
// New hooks pass SR_CD_FILE, a scratch file to write the target into — a side
// channel that exists so the hook never has to capture stdout (capturing
// un-TTYs it, which disables every interactive prompt). The __sr_cd: sentinel
// on stdout remains as the fallback for hooks eval'd before this change.
func handleNavigateResult(result engine.NavigateResult) {
	if !result.IsWorktree() {
		return
	}
	absPath := result.WorktreePath
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(ctx.Git.Dir, absPath)
	}
	if cdFile := os.Getenv("SR_CD_FILE"); cdFile != "" {
		if err := os.WriteFile(cdFile, []byte(absPath+"\n"), 0o600); err == nil {
			return
		}
		// Couldn't write the file — fall through to the sentinel.
	}
	fmt.Printf("__sr_cd:%s\n", absPath)
}
