package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/amustafa/stackr/internal/engine"
)

// handleNavigateResult prints worktree cd markers or checkout confirmation.
// The shell hook (sr shell-hook) parses __sr_cd: lines to trigger cd.
func handleNavigateResult(result engine.NavigateResult) {
	if result.IsWorktree() {
		absPath := result.WorktreePath
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(ctx.Git.Dir, absPath)
		}
		fmt.Printf("__sr_cd:%s\n", absPath)
	}
}
