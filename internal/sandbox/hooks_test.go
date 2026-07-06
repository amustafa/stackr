package sandbox

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildSettings(t *testing.T) {
	data, err := BuildSettings("/x/status-writer.js")
	if err != nil {
		t.Fatalf("BuildSettings: %v", err)
	}
	// Must be valid JSON.
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("settings not valid JSON: %v", err)
	}
	hooks, ok := parsed["hooks"].(map[string]any)
	if !ok {
		t.Fatal("missing hooks object")
	}
	for _, ev := range []string{"PreToolUse", "Stop", "Notification", "UserPromptSubmit", "SessionEnd"} {
		if _, ok := hooks[ev]; !ok {
			t.Errorf("missing hook for %s", ev)
		}
	}
	s := string(data)
	if !strings.Contains(s, "AskUserQuestion") {
		t.Error("PreToolUse should match AskUserQuestion")
	}
	if !strings.Contains(s, "node /x/status-writer.js awaiting-choice") {
		t.Error("AskUserQuestion hook should publish awaiting-choice")
	}
	if !strings.Contains(s, "awaiting-input") || !strings.Contains(s, "exited") {
		t.Error("expected awaiting-input + exited states wired")
	}
}

func TestStatusWriterScriptEmbedded(t *testing.T) {
	if len(StatusWriterScript()) == 0 {
		t.Fatal("status-writer.js not embedded")
	}
}
