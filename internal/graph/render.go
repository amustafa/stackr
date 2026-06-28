package graph

import (
	"fmt"
	"strings"
)

// RenderOpts controls log rendering.
type RenderOpts struct {
	CurrentBranch string
	ShowAll       bool                              // Show all stacks, not just current
	Reverse       bool                              // Reverse order (trunk at bottom)
	CommitsFn     func(branch string) []CommitInfo  // Optional: resolve commits per branch
}

// CommitInfo holds info about a single commit for rendering.
type CommitInfo struct {
	ShortSHA string
	Subject  string
}

// RenderTree produces a Graphite-style tree visualization of the stacks.
//
// Output style (tips at top, trunk at bottom, forks shown with connectors):
//
//	◉ feat-c ←
//	│
//	│ ◯ feat-b2
//	├─┘
//	◯ feat-b
//	│
//	◯ feat-a
//	│
//	◯ main (trunk)
func (g *Graph) RenderTree(opts RenderOpts) string {
	trunk := g.TrunkName()
	if trunk == "" {
		return "No trunk branch found\n"
	}

	children := g.ChildrenOf(trunk)
	if len(children) == 0 {
		return g.formatBranch(trunk, opts) + "\n"
	}

	// Filter to current stack when not showing all.
	if !opts.ShowAll && !g.IsTrunk(opts.CurrentBranch) {
		var relevant []string
		for _, child := range children {
			if g.containsBranch(child, opts.CurrentBranch) {
				relevant = append(relevant, child)
			}
		}
		if len(relevant) > 0 {
			children = relevant
		}
	}

	// Build a temporary graph node for trunk with only the filtered children
	// so renderNode renders the right subtrees.
	origChildren := g.Branches[trunk].Children
	g.Branches[trunk].Children = children
	defer func() { g.Branches[trunk].Children = origChildren }()

	var lines []string
	g.renderNode(trunk, opts, &lines)

	return strings.Join(lines, "\n") + "\n"
}

// renderNode recursively renders a branch and its subtree.
// Children appear above the branch (tips at top, trunk at bottom).
// When a branch has multiple children, the primary child (containing the
// current branch) gets the straight │ line; siblings branch off with ├─┘.
func (g *Graph) renderNode(name string, opts RenderOpts, lines *[]string) {
	children := g.ChildrenOf(name)

	if len(children) == 0 {
		// Leaf — just render the branch and its commits.
		*lines = append(*lines, g.formatBranch(name, opts))
		g.appendCommits(name, opts, lines)
		return
	}

	// Pick the primary child (the one containing the current branch).
	primary := g.pickPrimary(children, opts.CurrentBranch)
	others := without(children, primary)

	// 1. Render primary subtree (appears at the top of output).
	g.renderNode(primary, opts, lines)
	*lines = append(*lines, "│")

	// 2. Render side branches with │ prefix and ├─┘ connector.
	for _, child := range others {
		var sideLines []string
		g.renderNode(child, opts, &sideLines)
		for _, sl := range sideLines {
			*lines = append(*lines, "│ "+sl)
		}
		*lines = append(*lines, "├─┘")
	}

	// 3. Render this node.
	*lines = append(*lines, g.formatBranch(name, opts))
	g.appendCommits(name, opts, lines)
}

// pickPrimary returns the child that contains the current branch,
// falling back to the first child.
func (g *Graph) pickPrimary(children []string, current string) string {
	for _, c := range children {
		if g.containsBranch(c, current) {
			return c
		}
	}
	return children[0]
}

// containsBranch returns true if target is root or any of its descendants.
func (g *Graph) containsBranch(root, target string) bool {
	if root == target {
		return true
	}
	for _, child := range g.ChildrenOf(root) {
		if g.containsBranch(child, target) {
			return true
		}
	}
	return false
}

func (g *Graph) formatBranch(name string, opts RenderOpts) string {
	marker := "◯"
	if name == opts.CurrentBranch {
		marker = "◉"
	}

	suffix := ""
	if g.IsTrunk(name) {
		suffix = " (trunk)"
	}
	b := g.Branches[name]
	if b != nil && b.Frozen {
		suffix += " [frozen]"
	}

	pointer := ""
	if name == opts.CurrentBranch {
		pointer = " ←"
	}

	return fmt.Sprintf("%s %s%s%s", marker, name, suffix, pointer)
}

func (g *Graph) appendCommits(name string, opts RenderOpts, lines *[]string) {
	if opts.CommitsFn == nil || g.IsTrunk(name) {
		return
	}
	commits := opts.CommitsFn(name)
	for _, c := range commits {
		*lines = append(*lines, fmt.Sprintf("│   %s %s", c.ShortSHA, c.Subject))
	}
}

func without(s []string, val string) []string {
	result := make([]string, 0, len(s))
	for _, v := range s {
		if v != val {
			result = append(result, v)
		}
	}
	return result
}
