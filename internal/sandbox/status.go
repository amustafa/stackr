package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// State is a sandbox's interaction state, published by Claude Code hooks.
type State string

const (
	// StateWorking — the agent is working; nothing is needed from the human.
	StateWorking State = "working"
	// StateAwaitingInput — the agent ended its turn / asked a question.
	StateAwaitingInput State = "awaiting-input"
	// StateAwaitingChoice — the agent presented options (AskUserQuestion).
	StateAwaitingChoice State = "awaiting-choice"
	// StateExited — the session ended.
	StateExited State = "exited"
)

// Awaiting reports whether the state means the human's input is needed.
func (s State) Awaiting() bool {
	return s == StateAwaitingInput || s == StateAwaitingChoice
}

// Status is the current interaction state of a sandbox, keyed by branch.
type Status struct {
	Branch    string    `json:"branch"`
	State     State     `json:"state"`
	Reason    string    `json:"reason,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

const statusSuffix = ".status"

func statusPath(dir, branch string) string {
	return filepath.Join(dir, EncodeBranch(branch)+statusSuffix)
}

// WriteStatus persists a status under dir (creating dir if needed).
func WriteStatus(dir string, s *Status) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating sandboxes dir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling status: %w", err)
	}
	data = append(data, '\n')
	return writeFileAtomic(statusPath(dir, s.Branch), data)
}

// ReadStatus loads the status for a branch. Returns os.ErrNotExist if none.
func ReadStatus(dir, branch string) (*Status, error) {
	data, err := os.ReadFile(statusPath(dir, branch))
	if err != nil {
		return nil, err
	}
	var s Status
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing status for %q: %w", branch, err)
	}
	return &s, nil
}

// RemoveStatus deletes a branch's status file. Missing is not an error.
func RemoveStatus(dir, branch string) error {
	err := os.Remove(statusPath(dir, branch))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ListStatuses returns all statuses present under dir, sorted by branch.
// A missing directory yields an empty slice, not an error.
func ListStatuses(dir string) ([]*Status, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*Status
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), statusSuffix) {
			continue
		}
		// Use the Branch field inside the file rather than decoding the
		// filename — robust to the encoding and to any stray files.
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var s Status
		if err := json.Unmarshal(data, &s); err != nil {
			continue // skip unreadable/partial entries
		}
		out = append(out, &s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Branch < out[j].Branch })
	return out, nil
}
