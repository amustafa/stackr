package engine

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/amustafa/stackr/internal/context"
)

// SandboxAttachCommand builds the docker exec command that attaches to a
// sandbox's zellij session. Returned as *exec.Cmd so a TUI can run it via a
// suspend (tea.ExecProcess).
func SandboxAttachCommand(branch string) *exec.Cmd {
	name := sandboxContainerName(branch)
	return exec.Command("docker", "exec", "-it", name, "zellij", "attach", "--create", branch)
}

// SandboxAwaitingCount returns how many of this repo's sandboxes are awaiting
// human input — cheap enough to call from a shell prompt.
func SandboxAwaitingCount(c *context.Context) (int, error) {
	infos, err := SandboxList(c)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, in := range infos {
		if in.Status != nil && in.Status.State.Awaiting() {
			n++
		}
	}
	return n, nil
}

// SandboxNotify polls the sandboxes and fires a desktop notification whenever a
// sandbox transitions into an awaiting state. It runs until interrupted.
func SandboxNotify(c *context.Context) error {
	if !c.Quiet {
		fmt.Println("Watching sandboxes for input requests (Ctrl-C to stop)…")
	}
	wasAwaiting := map[string]bool{}
	for {
		infos, err := SandboxList(c)
		if err == nil {
			seen := map[string]bool{}
			for _, in := range infos {
				aw := in.Status != nil && in.Status.State.Awaiting()
				seen[in.Branch] = true
				if aw && !wasAwaiting[in.Branch] {
					reason := ""
					if in.Status != nil {
						reason = in.Status.Reason
					}
					notifyDesktop("sandbox "+in.Branch+" needs input", reason)
				}
				wasAwaiting[in.Branch] = aw
			}
			// Forget sandboxes that disappeared.
			for b := range wasAwaiting {
				if !seen[b] {
					delete(wasAwaiting, b)
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
}

// notifyDesktop sends an OS notification, falling back to stdout.
func notifyDesktop(title, body string) {
	if path, err := exec.LookPath("notify-send"); err == nil {
		_ = exec.Command(path, title, body).Run()
		return
	}
	if path, err := exec.LookPath("osascript"); err == nil { // macOS
		script := fmt.Sprintf("display notification %q with title %q", body, title)
		_ = exec.Command(path, "-e", script).Run()
		return
	}
	fmt.Printf("\a[sandbox] %s: %s\n", title, body)
}
