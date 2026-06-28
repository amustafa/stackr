package graph

// Upstack returns all branches from name upward (away from trunk) in BFS order.
func (g *Graph) Upstack(name string) []string {
	var result []string
	queue := []string{name}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)
		queue = append(queue, g.ChildrenOf(current)...)
	}
	return result
}

// Downstack returns branches from name down to (and including) trunk.
func (g *Graph) Downstack(name string) []string {
	var result []string
	current := name
	for current != "" {
		result = append(result, current)
		b := g.Branches[current]
		if b == nil || b.IsTrunk {
			break
		}
		current = b.ParentBranchName
	}
	return result
}

// StackOf returns all branches in the same stack (downstack + upstack of each).
func (g *Graph) StackOf(name string) []string {
	// Walk down to the bottom of the stack (first child of trunk).
	bottom := name
	for {
		b := g.Branches[bottom]
		if b == nil || b.IsTrunk {
			break
		}
		parent := b.ParentBranchName
		if g.IsTrunk(parent) {
			break
		}
		bottom = parent
	}
	// BFS from bottom gives the full stack.
	return g.Upstack(bottom)
}

// Bottom returns the bottom branch of the stack containing name
// (the first branch above trunk).
func (g *Graph) Bottom(name string) string {
	current := name
	for {
		b := g.Branches[current]
		if b == nil || b.IsTrunk {
			return current
		}
		parent := b.ParentBranchName
		if g.IsTrunk(parent) {
			return current
		}
		current = parent
	}
}

// Top returns a tip branch (first child with no children) of the stack containing name.
func (g *Graph) Top(name string) string {
	current := name
	for {
		children := g.ChildrenOf(current)
		if len(children) == 0 {
			return current
		}
		current = children[0]
	}
}

// Up returns the branch N steps upstack from name.
func (g *Graph) Up(name string, n int) string {
	current := name
	for i := 0; i < n; i++ {
		children := g.ChildrenOf(current)
		if len(children) == 0 {
			return current
		}
		current = children[0]
	}
	return current
}

// Down returns the branch N steps downstack from name (toward trunk).
func (g *Graph) Down(name string, n int) string {
	current := name
	for i := 0; i < n; i++ {
		b := g.Branches[current]
		if b == nil || b.IsTrunk {
			return current
		}
		current = b.ParentBranchName
	}
	return current
}

// AllStacks returns the names of bottom-of-stack branches (direct children of trunk).
func (g *Graph) AllStacks() []string {
	trunk := g.TrunkName()
	if trunk == "" {
		return nil
	}
	return g.ChildrenOf(trunk)
}
