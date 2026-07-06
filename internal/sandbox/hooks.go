package sandbox

import "encoding/json"

// StatusWriterScript returns the embedded Node script that Claude Code hooks run
// inside the sandbox to publish Sandbox Status (ADR-0011).
func StatusWriterScript() []byte {
	return mustAsset("assets/status-writer.js")
}

// hookEntry is one Claude Code hook binding.
type hookEntry struct {
	Matcher string        `json:"matcher,omitempty"`
	Hooks   []hookCommand `json:"hooks"`
}

type hookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// BuildSettings returns a sandbox-only Claude settings JSON (passed via
// `claude --settings`, additive over ~/.claude — never installed globally,
// ADR-0011). Each hook runs the status-writer with the state to publish.
// scriptPath is the in-container path of the status-writer script.
func BuildSettings(scriptPath string) ([]byte, error) {
	run := func(state string) []hookEntry {
		return []hookEntry{{Hooks: []hookCommand{{Type: "command", Command: "node " + scriptPath + " " + state}}}}
	}
	askChoice := []hookEntry{{
		Matcher: "AskUserQuestion",
		Hooks:   []hookCommand{{Type: "command", Command: "node " + scriptPath + " awaiting-choice"}},
	}}

	settings := map[string]any{
		"hooks": map[string]any{
			"PreToolUse":       askChoice,
			"Stop":             run("awaiting-input"),
			"Notification":     run("awaiting-input"),
			"UserPromptSubmit": run("working"),
			"SessionEnd":       run("exited"),
		},
	}
	return json.MarshalIndent(settings, "", "  ")
}
