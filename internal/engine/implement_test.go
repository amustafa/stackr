package engine

import "testing"

func TestChooseDrive(t *testing.T) {
	tests := []struct {
		name                               string
		sandbox, ai, interactive, inClaude bool
		want                               driveKind
	}{
		// non-sandbox
		{name: "bare interactive terminal spawns", interactive: true, want: driveSpawn},
		{name: "--ai emits", ai: true, interactive: true, want: driveEmitJSON},
		{name: "inside claude emits", interactive: true, inClaude: true, want: driveEmitJSON},
		{name: "non-interactive emits", interactive: false, want: driveEmitJSON},
		{name: "--ai inside claude emits", ai: true, inClaude: true, interactive: true, want: driveEmitJSON},

		// sandbox
		{name: "sandbox bare terminal attaches", sandbox: true, interactive: true, want: driveSandboxAttach},
		{name: "sandbox --ai detaches", sandbox: true, ai: true, interactive: true, want: driveSandboxDetached},
		{name: "sandbox inside claude detaches", sandbox: true, interactive: true, inClaude: true, want: driveSandboxDetached},
		{name: "sandbox non-interactive detaches", sandbox: true, interactive: false, want: driveSandboxDetached},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := chooseDrive(tc.sandbox, tc.ai, tc.interactive, tc.inClaude)
			if got != tc.want {
				t.Errorf("chooseDrive(sandbox=%v ai=%v interactive=%v inClaude=%v) = %d, want %d",
					tc.sandbox, tc.ai, tc.interactive, tc.inClaude, got, tc.want)
			}
		})
	}
}

func TestImplementResultJSON(t *testing.T) {
	// A worktree-less hand-off omits worktreePath and attachCommand.
	res := ImplementResult{Branch: "123-x", IssueRef: "#123", Prompt: "do it"}
	if err := emitImplementJSON(res); err != nil {
		t.Fatalf("emit: %v", err)
	}
}
