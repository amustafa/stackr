package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The always-on stackr guidance lives in its own file, .claude/prompts/stackr.md,
// which CLAUDE.md imports via a marker-delimited @-reference. Keeping the prose in
// a dedicated file (rather than inline in CLAUDE.md) means one source of truth for
// the content and a stable, tiny managed region in CLAUDE.md.
const (
	promptBlockBegin = "<!-- stackr:begin -->"
	promptBlockEnd   = "<!-- stackr:end -->"
	claudeMdFile     = "CLAUDE.md"
	promptFileRel    = ".claude/prompts/stackr.md"
	promptImportRef  = "@.claude/prompts/stackr.md"
)

// promptContent is the always-on stackr guidance. It is a pointer to the skill
// plus the few rules an agent needs before the skill loads — not a copy of the
// skill's command reference.
const promptContent = "# stackr\n" +
	"\n" +
	"This repo uses stackr (`sr`) for stacked-branch development. Prefer `sr` over\n" +
	"raw git for branch, commit, and PR operations — raw git desyncs the stack graph.\n" +
	"\n" +
	"**Stack sequential work.** When a change builds on earlier work, don't pile it\n" +
	"all into one branch — stack it: put each reviewable step on its own branch on top\n" +
	"of the previous one, so every branch builds on its parent and reviews on its own.\n" +
	"\n" +
	"**Travel the stack** with `sr up` and `sr down` to move between branches\n" +
	"(`sr top` / `sr bottom` jump to the ends); `sr log` shows the tree.\n" +
	"\n" +
	"See the `stackr` skill for the full command set and workflow.\n"

// managedBlock is the CLAUDE.md region that imports the prompt file.
func managedBlock() string {
	return promptBlockBegin + "\n" + promptImportRef + "\n" + promptBlockEnd
}

// writePromptFile writes the always-on stackr prompt to baseDir/.claude/prompts/stackr.md.
func writePromptFile(baseDir string) (string, error) {
	path := filepath.Join(baseDir, filepath.FromSlash(promptFileRel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("could not create prompts directory: %w", err)
	}
	if existing, err := os.ReadFile(path); err == nil && string(existing) == promptContent {
		return "stackr prompt already current at " + promptFileRel, nil
	}
	if err := os.WriteFile(path, []byte(promptContent), 0o644); err != nil {
		return "", fmt.Errorf("could not write %s: %w", promptFileRel, err)
	}
	return "wrote stackr prompt to " + promptFileRel, nil
}

// removePromptFile deletes the prompt file, and prunes .claude/prompts if it is
// left empty. Returns whether the file existed.
func removePromptFile(baseDir string) (bool, error) {
	path := filepath.Join(baseDir, filepath.FromSlash(promptFileRel))
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("could not remove %s: %w", promptFileRel, err)
	}
	if dir := filepath.Dir(path); dirIsEmpty(dir) {
		_ = os.Remove(dir)
	}
	return true, nil
}

// writeClaudeMdImport creates, updates, or appends the marker-delimited import
// block in baseDir/CLAUDE.md. Content outside the markers is preserved and the
// operation is idempotent. The returned string describes what happened.
func writeClaudeMdImport(baseDir string) (string, error) {
	path := filepath.Join(baseDir, claudeMdFile)
	block := managedBlock()

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(path, []byte(block+"\n"), 0o644); err != nil {
			return "", fmt.Errorf("could not create %s: %w", claudeMdFile, err)
		}
		return "created " + claudeMdFile + " importing the stackr prompt", nil
	}
	if err != nil {
		return "", fmt.Errorf("could not read %s: %w", claudeMdFile, err)
	}

	content := string(data)
	if before, after, ok := splitManagedBlock(content); ok {
		updated := before + block + after
		if updated == content {
			return "stackr import already current in " + claudeMdFile, nil
		}
		if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
			return "", fmt.Errorf("could not update %s: %w", claudeMdFile, err)
		}
		return "updated stackr import in " + claudeMdFile, nil
	}

	updated := strings.TrimRight(content, "\n") + "\n\n" + block + "\n"
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return "", fmt.Errorf("could not update %s: %w", claudeMdFile, err)
	}
	return "added stackr import to " + claudeMdFile, nil
}

// removeClaudeMdImport strips the managed block from baseDir/CLAUDE.md, leaving
// the rest intact. Returns whether anything was removed.
func removeClaudeMdImport(baseDir string) (bool, error) {
	path := filepath.Join(baseDir, claudeMdFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("could not read %s: %w", claudeMdFile, err)
	}

	content := string(data)
	before, after, ok := splitManagedBlock(content)
	if !ok {
		return false, nil
	}

	before = strings.TrimRight(before, "\n")
	after = strings.TrimLeft(after, "\n")
	var updated string
	switch {
	case before == "" && after == "":
		updated = ""
	case before == "":
		updated = after
	case after == "":
		updated = before + "\n"
	default:
		updated = before + "\n\n" + after
	}

	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return false, fmt.Errorf("could not update %s: %w", claudeMdFile, err)
	}
	return true, nil
}

// splitManagedBlock returns the content before and after the managed block (with
// the markers removed) and whether a well-formed block was found.
func splitManagedBlock(content string) (before, after string, ok bool) {
	bi := strings.Index(content, promptBlockBegin)
	if bi == -1 {
		return "", "", false
	}
	ei := strings.Index(content, promptBlockEnd)
	if ei == -1 || ei < bi {
		return "", "", false
	}
	return content[:bi], content[ei+len(promptBlockEnd):], true
}

func dirIsEmpty(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) == 0
}
