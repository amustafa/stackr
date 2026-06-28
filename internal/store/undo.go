package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// UndoEvent represents a mutation that can be undone.
type UndoEvent struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Operation string    `json:"operation"` // "create", "delete", "modify", etc.
	Branch    string    `json:"branch"`
	Snapshot  string    `json:"snapshot"` // Filename of the snapshot
}

// EventLog holds the list of undo events.
type EventLog struct {
	Events []UndoEvent `json:"events"`
}

// SaveSnapshot saves the current branches.json as a snapshot for undo.
func (s *Store) SaveSnapshot(operation, branch string) error {
	// Read current graph.
	data, err := os.ReadFile(s.path("branches.json"))
	if err != nil {
		return err
	}

	// Create snapshot file.
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	snapName := id + ".json"
	snapPath := filepath.Join(s.root, "undo", "snapshots", snapName)
	if err := os.WriteFile(snapPath, data, 0o644); err != nil {
		return err
	}

	// Append to event log.
	log := s.readEventLog()
	log.Events = append(log.Events, UndoEvent{
		ID:        id,
		Timestamp: time.Now(),
		Operation: operation,
		Branch:    branch,
		Snapshot:  snapName,
	})

	return s.writeEventLog(log)
}

// PopSnapshot restores the most recent snapshot and removes it from the log.
func (s *Store) PopSnapshot() (*UndoEvent, []byte, error) {
	log := s.readEventLog()
	if len(log.Events) == 0 {
		return nil, nil, fmt.Errorf("nothing to undo")
	}

	// Pop last event.
	event := log.Events[len(log.Events)-1]
	log.Events = log.Events[:len(log.Events)-1]

	// Read snapshot.
	snapPath := filepath.Join(s.root, "undo", "snapshots", event.Snapshot)
	data, err := os.ReadFile(snapPath)
	if err != nil {
		return nil, nil, fmt.Errorf("snapshot not found: %w", err)
	}

	// Clean up snapshot file.
	_ = os.Remove(snapPath)

	// Write updated log.
	if err := s.writeEventLog(log); err != nil {
		return nil, nil, err
	}

	return &event, data, nil
}

func (s *Store) readEventLog() *EventLog {
	var log EventLog
	data, err := os.ReadFile(filepath.Join(s.root, "undo", "event_log.json"))
	if err != nil {
		return &EventLog{}
	}
	_ = json.Unmarshal(data, &log)
	return &log
}

func (s *Store) writeEventLog(log *EventLog) error {
	data, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.root, "undo", "event_log.json"), data, 0o644)
}
