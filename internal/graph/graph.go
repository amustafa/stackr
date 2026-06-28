package graph

import "fmt"

// BranchState holds the stack metadata for a single branch.
type BranchState struct {
	ParentBranchName     string   `json:"parentBranchName,omitempty"`
	ParentBranchRevision string   `json:"parentBranchRevision,omitempty"`
	BranchRevision       string   `json:"branchRevision"`
	Children             []string `json:"children"`
	IsTrunk              bool     `json:"isTrunk,omitempty"`
	Frozen               bool                          `json:"frozen,omitempty"`
	Description          string                        `json:"description,omitempty"`
	Context              []BranchContext               `json:"context,omitempty"`
	CommitContexts       map[string][]BranchContext    `json:"commitContexts,omitempty"`
}

// Source identifies where a piece of context came from.
type Source struct {
	Type      string `json:"type"`      // "file", "url", "ticket", "conversation"
	Reference string `json:"reference"` // "internal/auth/jwt.go", "https://..."
}

// BranchContext is a structured context entry attached to a branch.
type BranchContext struct {
	Key     string   `json:"key"`               // Identifier for upsert/removal
	Text    string   `json:"text"`              // The context content
	Sources []Source `json:"sources,omitempty"` // Where this came from
	Tickets []string `json:"tickets,omitempty"` // Related issue/ticket IDs
}

// Graph is the in-memory representation of the stack graph.
type Graph struct {
	Version  int                    `json:"version"`
	Branches map[string]*BranchState `json:"branches"`
}

// New creates an empty graph.
func New() *Graph {
	return &Graph{
		Version:  1,
		Branches: make(map[string]*BranchState),
	}
}

// AddTrunk registers the trunk branch in the graph.
func (g *Graph) AddTrunk(name, revision string) {
	g.Branches[name] = &BranchState{
		IsTrunk:        true,
		BranchRevision: revision,
		Children:       []string{},
	}
}

// AddBranch registers a new branch with its parent.
func (g *Graph) AddBranch(name, parentName, parentRevision, branchRevision string) error {
	parent, ok := g.Branches[parentName]
	if !ok {
		return fmt.Errorf("parent branch %q not found in graph", parentName)
	}
	g.Branches[name] = &BranchState{
		ParentBranchName:     parentName,
		ParentBranchRevision: parentRevision,
		BranchRevision:       branchRevision,
		Children:             []string{},
	}
	parent.Children = append(parent.Children, name)
	return nil
}

// RemoveBranch removes a branch and reparents its children to its parent.
func (g *Graph) RemoveBranch(name string) error {
	branch, ok := g.Branches[name]
	if !ok {
		return fmt.Errorf("branch %q not found", name)
	}
	if branch.IsTrunk {
		return fmt.Errorf("cannot remove trunk branch")
	}

	parent := g.Branches[branch.ParentBranchName]

	// Remove from parent's children list.
	parent.Children = removeFromSlice(parent.Children, name)

	// Reparent children to this branch's parent.
	for _, child := range branch.Children {
		g.Branches[child].ParentBranchName = branch.ParentBranchName
		g.Branches[child].ParentBranchRevision = branch.BranchRevision
		parent.Children = append(parent.Children, child)
	}

	delete(g.Branches, name)
	return nil
}

// Parent returns the parent branch name, or empty string for trunk.
func (g *Graph) Parent(name string) string {
	b, ok := g.Branches[name]
	if !ok {
		return ""
	}
	return b.ParentBranchName
}

// ChildrenOf returns the children of a branch.
func (g *Graph) ChildrenOf(name string) []string {
	b, ok := g.Branches[name]
	if !ok {
		return nil
	}
	return b.Children
}

// IsTrunk returns whether the branch is the trunk.
func (g *Graph) IsTrunk(name string) bool {
	b, ok := g.Branches[name]
	if !ok {
		return false
	}
	return b.IsTrunk
}

// TrunkName returns the name of the trunk branch.
func (g *Graph) TrunkName() string {
	for name, b := range g.Branches {
		if b.IsTrunk {
			return name
		}
	}
	return ""
}

// Has returns true if the branch exists in the graph.
func (g *Graph) Has(name string) bool {
	_, ok := g.Branches[name]
	return ok
}

// SetDescription sets the description for a branch.
func (g *Graph) SetDescription(name, desc string) error {
	b, ok := g.Branches[name]
	if !ok {
		return fmt.Errorf("branch %q not found", name)
	}
	b.Description = desc
	return nil
}

// Description returns the description for a branch.
func (g *Graph) Description(name string) string {
	b, ok := g.Branches[name]
	if !ok {
		return ""
	}
	return b.Description
}

// SetContext adds or updates a context entry by key (upsert).
func (g *Graph) SetContext(branch string, ctx BranchContext) error {
	b, ok := g.Branches[branch]
	if !ok {
		return fmt.Errorf("branch %q not found", branch)
	}
	for i, existing := range b.Context {
		if existing.Key == ctx.Key {
			b.Context[i] = ctx
			return nil
		}
	}
	b.Context = append(b.Context, ctx)
	return nil
}

// RemoveContext removes a context entry by key.
func (g *Graph) RemoveContext(branch, key string) error {
	b, ok := g.Branches[branch]
	if !ok {
		return fmt.Errorf("branch %q not found", branch)
	}
	for i, existing := range b.Context {
		if existing.Key == key {
			b.Context = append(b.Context[:i], b.Context[i+1:]...)
			return nil
		}
	}
	return nil
}

// GetContext returns all context entries for a branch.
func (g *Graph) GetContext(branch string) []BranchContext {
	b, ok := g.Branches[branch]
	if !ok {
		return nil
	}
	return b.Context
}

// SetCommitContext adds or updates a context entry for a specific commit.
func (g *Graph) SetCommitContext(branch, sha string, ctx BranchContext) error {
	b, ok := g.Branches[branch]
	if !ok {
		return fmt.Errorf("branch %q not found", branch)
	}
	if b.CommitContexts == nil {
		b.CommitContexts = make(map[string][]BranchContext)
	}
	entries := b.CommitContexts[sha]
	for i, existing := range entries {
		if existing.Key == ctx.Key {
			entries[i] = ctx
			b.CommitContexts[sha] = entries
			return nil
		}
	}
	b.CommitContexts[sha] = append(entries, ctx)
	return nil
}

// GetCommitContexts returns context entries for a specific commit.
func (g *Graph) GetCommitContexts(branch, sha string) []BranchContext {
	b, ok := g.Branches[branch]
	if !ok {
		return nil
	}
	return b.CommitContexts[sha]
}

func removeFromSlice(s []string, val string) []string {
	result := make([]string, 0, len(s))
	for _, v := range s {
		if v != val {
			result = append(result, v)
		}
	}
	return result
}
