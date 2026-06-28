package git

import (
	"fmt"
	"strings"
)

// LogOneline returns one-line log entries for a range.
func (r *Runner) LogOneline(revRange string, maxCount int) ([]LogEntry, error) {
	args := []string{"log", "--oneline", "--format=%H %s", revRange}
	if maxCount > 0 {
		args = append(args, fmt.Sprintf("-%d", maxCount))
	}
	out, err := r.RunGitCapture(args...)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	var entries []LogEntry
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 {
			entries = append(entries, LogEntry{SHA: parts[0], Subject: parts[1]})
		}
	}
	return entries, nil
}

// CommitsBetween returns commits between parent..branch.
func (r *Runner) CommitsBetween(parent, branch string) ([]LogEntry, error) {
	return r.LogOneline(parent+".."+branch, 0)
}

// LogEntry holds a single commit log line.
type LogEntry struct {
	SHA     string
	Subject string
}
