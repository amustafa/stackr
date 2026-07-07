package cmd

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

const implementSkillDir = ".claude/skills/sr-implement"

//go:embed assets/sr-implement-SKILL.md
var implementSkillContent string

// installImplementSkill writes the sr-implement skill under repoRoot.
func installImplementSkill(repoRoot string) error {
	dir := filepath.Join(repoRoot, implementSkillDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("could not create implement skill directory: %w", err)
	}
	path := filepath.Join(dir, skillFile)
	if err := os.WriteFile(path, []byte(implementSkillContent), 0o644); err != nil {
		return fmt.Errorf("could not write implement skill: %w", err)
	}
	return nil
}
