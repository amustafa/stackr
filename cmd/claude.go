package cmd

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

const skillDir = ".claude/skills/stackr"

// skillAssets holds the unified stackr skill (SKILL.md plus its progressive-
// disclosure reference files). all: includes files that would otherwise be
// skipped by the embed globbing rules.
//
//go:embed all:assets/stackr-skill
var skillAssets embed.FS

const skillAssetRoot = "assets/stackr-skill"

// obsoleteSkillDirs are separate skill directories that predate the unified
// stackr skill. install removes them so upgraders aren't left with stale
// duplicate skills.
var obsoleteSkillDirs = []string{
	".claude/skills/sr-sandbox",
	".claude/skills/sr-implement",
}

var claudeCmd = &cobra.Command{
	Use:   "claude",
	Short: "Claude Code integration",
}

var claudeInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the stackr skill for Claude Code",
	Long:  "Creates a Claude Code skill at .claude/skills/stackr/ that teaches Claude how to use sr commands (including the sandbox and implement lanes).",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := ctx.Git.RepoRoot()
		if err != nil {
			return fmt.Errorf("could not find repo root: %w", err)
		}

		n, err := writeSkill(repoRoot)
		if err != nil {
			return err
		}
		fmt.Printf("Installed stackr skill (%d files) to %s\n", n, skillDir)

		for _, d := range obsoleteSkillDirs {
			p := filepath.Join(repoRoot, d)
			if _, err := os.Stat(p); err == nil {
				if err := os.RemoveAll(p); err != nil {
					return fmt.Errorf("could not remove obsolete skill %s: %w", d, err)
				}
				fmt.Printf("Removed obsolete skill %s (folded into stackr)\n", d)
			}
		}
		return nil
	},
}

var claudeUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove the stackr skill from Claude Code",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := ctx.Git.RepoRoot()
		if err != nil {
			return fmt.Errorf("could not find repo root: %w", err)
		}

		removed := false
		for _, d := range append([]string{skillDir}, obsoleteSkillDirs...) {
			p := filepath.Join(repoRoot, d)
			if _, err := os.Stat(p); os.IsNotExist(err) {
				continue
			}
			if err := os.RemoveAll(p); err != nil {
				return fmt.Errorf("could not remove %s: %w", d, err)
			}
			removed = true
		}
		if removed {
			fmt.Println("Removed stackr skill")
		} else {
			fmt.Println("No stackr skill found")
		}
		return nil
	},
}

// writeSkill renders the embedded skill assets into repoRoot/.claude/skills/stackr,
// preserving their relative layout, and returns the number of files written.
func writeSkill(repoRoot string) (int, error) {
	dest := filepath.Join(repoRoot, skillDir)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return 0, fmt.Errorf("could not create skill directory: %w", err)
	}

	count := 0
	err := fs.WalkDir(skillAssets, skillAssetRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := skillAssets.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(skillAssetRoot, p)
		if err != nil {
			return err
		}
		out := filepath.Join(dest, rel)
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
			return fmt.Errorf("could not write skill file %s: %w", rel, err)
		}
		count++
		return nil
	})
	if err != nil {
		return count, fmt.Errorf("could not write skill: %w", err)
	}
	return count, nil
}

func init() {
	claudeCmd.AddCommand(claudeInstallCmd)
	claudeCmd.AddCommand(claudeUninstallCmd)
	rootCmd.AddCommand(claudeCmd)
}
