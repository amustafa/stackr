package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// EditText opens the user's preferred editor with initial content and returns
// the edited text. It checks $EDITOR, then $VISUAL, falling back to vi.
func EditText(initial string) (string, error) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}

	f, err := os.CreateTemp("", "stackr-*.md")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := f.Name()
	defer os.Remove(tmpPath)

	if _, err := f.WriteString(initial); err != nil {
		f.Close()
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}
	f.Close()

	cmd := exec.Command(editor, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor exited with error: %w", err)
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", fmt.Errorf("failed to read edited file: %w", err)
	}

	return strings.TrimSpace(string(data)), nil
}
