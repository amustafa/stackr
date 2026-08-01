package store

import (
	"os"
	"time"
)

const pushRecordsFile = "push_records.json"

// PushRecords maps each remote's branches to the commit this clone last left
// there. It is deliberately **Local Data**: the record's meaning is "*we* put
// that there", which is what makes it safe to force-push over. A record that
// travelled between clones would let one developer's push authorise another
// developer's force push over it, turning the only sound check in the
// divergence ladder into the most dangerous one (ADR-0014).
//
// Nested by remote rather than keyed on a joined "remote/branch" string: branch
// names may contain slashes, so a flat key cannot be split back apart
// unambiguously.
type PushRecords struct {
	Version int                                   `json:"version"`
	Remotes map[string]map[string]PushRecordEntry `json:"remotes"`
}

// PushRecordEntry is one branch's record on one remote.
type PushRecordEntry struct {
	SHA       string `json:"sha"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

const pushRecordsVersion = 1

// ReadPushRecords loads the records, returning an empty set when none exist.
func (s *Store) ReadPushRecords() (*PushRecords, error) {
	var pr PushRecords
	if err := s.readJSON(pushRecordsFile, &pr); err != nil {
		if os.IsNotExist(unwrapStoreErr(err)) {
			return newPushRecords(), nil
		}
		return nil, err
	}
	if pr.Remotes == nil {
		pr.Remotes = map[string]map[string]PushRecordEntry{}
	}
	return &pr, nil
}

func (s *Store) WritePushRecords(pr *PushRecords) error {
	if pr.Version == 0 {
		pr.Version = pushRecordsVersion
	}
	return s.writeJSON(pushRecordsFile, pr)
}

func newPushRecords() *PushRecords {
	return &PushRecords{
		Version: pushRecordsVersion,
		Remotes: map[string]map[string]PushRecordEntry{},
	}
}

// PushRecordFor returns the SHA this clone last left on remote/branch, or "".
func (s *Store) PushRecordFor(remote, branch string) string {
	pr, err := s.ReadPushRecords()
	if err != nil {
		return ""
	}
	return pr.Get(remote, branch)
}

// SetPushRecord records that remote/branch now holds sha.
//
// Called by every operation that settles local against remote — not only
// pushes. Resetting a branch to its remote is equally a statement that we
// accept what is there as ours.
func (s *Store) SetPushRecord(remote, branch, sha string) error {
	pr, err := s.ReadPushRecords()
	if err != nil {
		return err
	}
	pr.Set(remote, branch, sha)
	return s.WritePushRecords(pr)
}

// DeletePushRecordsForBranch drops every remote's record for a branch. Called
// wherever the graph drops a branch, so a branch name that is later recreated
// does not inherit a stale claim on someone else's work.
func (s *Store) DeletePushRecordsForBranch(branch string) error {
	pr, err := s.ReadPushRecords()
	if err != nil {
		return err
	}
	changed := false
	for _, branches := range pr.Remotes {
		if _, ok := branches[branch]; ok {
			delete(branches, branch)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.WritePushRecords(pr)
}

// Get returns the recorded SHA for remote/branch, or "".
func (p *PushRecords) Get(remote, branch string) string {
	if p == nil || p.Remotes == nil {
		return ""
	}
	return p.Remotes[remote][branch].SHA
}

// Set records a SHA for remote/branch.
func (p *PushRecords) Set(remote, branch, sha string) {
	if p.Remotes == nil {
		p.Remotes = map[string]map[string]PushRecordEntry{}
	}
	if p.Remotes[remote] == nil {
		p.Remotes[remote] = map[string]PushRecordEntry{}
	}
	p.Remotes[remote][branch] = PushRecordEntry{
		SHA:       sha,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

// unwrapStoreErr digs the os error out of a StoreError so callers can test for
// a missing file without importing the error type's internals.
func unwrapStoreErr(err error) error {
	type unwrapper interface{ Unwrap() error }
	for err != nil {
		if u, ok := err.(unwrapper); ok {
			err = u.Unwrap()
			continue
		}
		return err
	}
	return err
}
