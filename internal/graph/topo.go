package graph

// TopoOrder returns branches in topological order (parents before children)
// starting from the given root.
func (g *Graph) TopoOrder(root string) []string {
	var result []string
	var walk func(name string)
	walk = func(name string) {
		result = append(result, name)
		for _, child := range g.ChildrenOf(name) {
			walk(child)
		}
	}
	walk(root)
	return result
}

// UpstackTopo returns branches upstack from name in topological order,
// excluding name itself. This is the order needed for restacking.
func (g *Graph) UpstackTopo(name string) []string {
	all := g.TopoOrder(name)
	if len(all) <= 1 {
		return nil
	}
	return all[1:] // Skip the root itself
}
