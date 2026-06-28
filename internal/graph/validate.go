package graph

import "fmt"

// Validate checks the graph for structural integrity.
func (g *Graph) Validate() []error {
	var errs []error

	// Check for exactly one trunk.
	trunkCount := 0
	for name, b := range g.Branches {
		if b.IsTrunk {
			trunkCount++
		}
		// Validate parent references.
		if !b.IsTrunk && b.ParentBranchName != "" {
			if _, ok := g.Branches[b.ParentBranchName]; !ok {
				errs = append(errs, fmt.Errorf("branch %q references nonexistent parent %q", name, b.ParentBranchName))
			}
		}
		// Validate children references.
		for _, child := range b.Children {
			if _, ok := g.Branches[child]; !ok {
				errs = append(errs, fmt.Errorf("branch %q references nonexistent child %q", name, child))
			}
		}
	}
	if trunkCount == 0 {
		errs = append(errs, fmt.Errorf("no trunk branch found"))
	} else if trunkCount > 1 {
		errs = append(errs, fmt.Errorf("multiple trunk branches found"))
	}

	return errs
}
