package store

import "os"

// GetState tracks an in-progress sr get operation so `sr continue` can resume.
type GetState struct {
	Operation     string   `json:"operation"`     // "get"
	OrigBranch    string   `json:"origBranch"`    // Branch the user was on before sr get
	Target        string   `json:"target"`        // The requested target branch
	WalkPath      []string `json:"walkPath"`      // Full walk path (trunk→target order)
	Completed     []string `json:"completed"`     // Branches already synced
	CurrentBranch string   `json:"currentBranch"` // Branch where conflict occurred
	Flags         GetFlags `json:"flags"`         // Preserved flags for resume
}

// GetFlags preserves the flags passed to sr get for resume after conflict.
type GetFlags struct {
	Downstack     bool `json:"downstack"`
	RemoteUpstack bool `json:"remoteUpstack"`
	Worktree      bool `json:"worktree"`
	Stay          bool `json:"stay"`
	Force         bool `json:"force"`
}

func (s *Store) ReadGetState() (*GetState, error) {
	var gs GetState
	if err := s.readJSON("get_state.json", &gs); err != nil {
		return nil, err
	}
	return &gs, nil
}

func (s *Store) WriteGetState(gs *GetState) error {
	return s.writeJSON("get_state.json", gs)
}

func (s *Store) ClearGetState() error {
	path := s.path("get_state.json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Store) HasGetState() bool {
	_, err := os.Stat(s.path("get_state.json"))
	return err == nil
}
