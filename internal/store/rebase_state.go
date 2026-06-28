package store

import "os"

// RebaseState tracks an in-progress restack operation so `sr continue` can resume.
type RebaseState struct {
	Operation    string   `json:"operation"`    // "restack", "modify", "move", etc.
	OrigBranch   string   `json:"origBranch"`   // Branch the user was on before the operation
	Pending      []string `json:"pending"`      // Branches still to rebase (in topo order)
	Completed    []string `json:"completed"`    // Branches already rebased
	CurrentBranch string  `json:"currentBranch"` // Branch currently being rebased
}

func (s *Store) ReadRebaseState() (*RebaseState, error) {
	var rs RebaseState
	if err := s.readJSON("rebase_state.json", &rs); err != nil {
		return nil, err
	}
	return &rs, nil
}

func (s *Store) WriteRebaseState(rs *RebaseState) error {
	return s.writeJSON("rebase_state.json", rs)
}

func (s *Store) ClearRebaseState() error {
	path := s.path("rebase_state.json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Store) HasRebaseState() bool {
	_, err := os.Stat(s.path("rebase_state.json"))
	return err == nil
}
