package store

import "github.com/amustafa/stackr/internal/graph"

// Backend defines the interface for stackr metadata storage.
type Backend interface {
	Init() error
	Exists() bool
	Root() string

	ReadGraph() (*graph.Graph, error)
	WriteGraph(g *graph.Graph) error

	ReadConfig() (*Config, error)
	WriteConfig(cfg *Config) error

	ReadPRInfo() (*PRInfo, error)
	WritePRInfo(info *PRInfo) error

	ReadRebaseState() (*RebaseState, error)
	WriteRebaseState(rs *RebaseState) error
	ClearRebaseState() error
	HasRebaseState() bool

	SaveSnapshot(operation, branch string) error
	PopSnapshot() (*UndoEvent, []byte, error)
}
