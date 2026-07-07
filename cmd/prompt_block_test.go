package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func TestWritePromptFileCreatesAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	if _, err := writePromptFile(dir); err != nil {
		t.Fatalf("writePromptFile: %v", err)
	}
	path := filepath.Join(dir, filepath.FromSlash(promptFileRel))
	got := readFileString(t, path)
	if got != promptContent {
		t.Fatalf("prompt file content mismatch:\n%s", got)
	}
	// Mentions the navigation + stacking guidance the block must carry.
	for _, want := range []string{"sr up", "sr down", "Stack sequential work", "stackr` skill"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	// Second run reports current, content unchanged.
	msg, err := writePromptFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "already current") {
		t.Errorf("expected idempotent message, got %q", msg)
	}
	if readFileString(t, path) != got {
		t.Error("prompt file changed on second write")
	}
}

func TestWriteClaudeMdImportCreatesFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := writeClaudeMdImport(dir); err != nil {
		t.Fatalf("writeClaudeMdImport: %v", err)
	}
	got := readFileString(t, filepath.Join(dir, claudeMdFile))
	if !strings.Contains(got, promptBlockBegin) || !strings.Contains(got, promptImportRef) {
		t.Fatalf("import block missing:\n%s", got)
	}
	if strings.Count(got, promptBlockBegin) != 1 {
		t.Fatalf("expected one block:\n%s", got)
	}
}

func TestWriteClaudeMdImportAppendsPreservingContent(t *testing.T) {
	dir := t.TempDir()
	existing := "# My Project\n\nSome instructions.\n"
	path := filepath.Join(dir, claudeMdFile)
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := writeClaudeMdImport(dir); err != nil {
		t.Fatal(err)
	}
	got := readFileString(t, path)
	if !strings.HasPrefix(got, existing) {
		t.Fatalf("existing content not preserved:\n%q", got)
	}
	if !strings.Contains(got, promptImportRef) {
		t.Fatalf("import ref missing:\n%s", got)
	}
}

func TestWriteClaudeMdImportIdempotentAndUpdates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, claudeMdFile)
	// Stale block (old inline style) gets reconciled to the import ref.
	stale := "# My Project\n\n" + promptBlockBegin + "\nold inline text\n" + promptBlockEnd + "\n"
	if err := os.WriteFile(path, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := writeClaudeMdImport(dir); err != nil {
		t.Fatal(err)
	}
	got := readFileString(t, path)
	if strings.Contains(got, "old inline text") {
		t.Fatalf("stale content not replaced:\n%s", got)
	}
	if !strings.Contains(got, promptImportRef) {
		t.Fatalf("import ref missing:\n%s", got)
	}
	// Idempotent on repeat.
	if _, err := writeClaudeMdImport(dir); err != nil {
		t.Fatal(err)
	}
	if again := readFileString(t, path); again != got {
		t.Fatalf("not idempotent:\n%q\nvs\n%q", got, again)
	}
	if strings.Count(got, promptBlockBegin) != 1 {
		t.Fatalf("duplicate block:\n%s", got)
	}
}

func TestRemovePromptFileAndImport(t *testing.T) {
	dir := t.TempDir()
	existing := "# My Project\n\nSome instructions.\n"
	mdPath := filepath.Join(dir, claudeMdFile)
	if err := os.WriteFile(mdPath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := writePromptFile(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := writeClaudeMdImport(dir); err != nil {
		t.Fatal(err)
	}

	removed, err := removePromptFile(dir)
	if err != nil || !removed {
		t.Fatalf("removePromptFile: removed=%v err=%v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(promptFileRel))); !os.IsNotExist(err) {
		t.Error("prompt file not deleted")
	}

	blockRemoved, err := removeClaudeMdImport(dir)
	if err != nil || !blockRemoved {
		t.Fatalf("removeClaudeMdImport: removed=%v err=%v", blockRemoved, err)
	}
	got := readFileString(t, mdPath)
	if strings.Contains(got, "stackr") || strings.Contains(got, promptBlockBegin) {
		t.Fatalf("block not fully removed:\n%s", got)
	}
	if !strings.Contains(got, "Some instructions.") {
		t.Fatalf("surrounding content lost:\n%s", got)
	}
}

func TestRemoveOnMissingFilesIsNoop(t *testing.T) {
	dir := t.TempDir()
	if removed, err := removePromptFile(dir); err != nil || removed {
		t.Fatalf("removePromptFile on empty: removed=%v err=%v", removed, err)
	}
	if removed, err := removeClaudeMdImport(dir); err != nil || removed {
		t.Fatalf("removeClaudeMdImport on empty: removed=%v err=%v", removed, err)
	}
}

// TestInstalledPromptMatchesConstant guards against the checked-in dogfood copy
// of the prompt drifting from the source constant — the same discipline as the
// skill drift guard.
func TestInstalledPromptMatchesConstant(t *testing.T) {
	installed := filepath.Join("..", filepath.FromSlash(promptFileRel))
	data, err := os.ReadFile(installed)
	if err != nil {
		t.Fatalf("installed prompt missing (run `sr claude install`): %v", err)
	}
	if string(data) != promptContent {
		t.Error("installed .claude/prompts/stackr.md is out of date — run `sr claude install`")
	}
}
