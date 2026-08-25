package graph

import (
	"fmt"
	"strings"
)

// RenderOpts controls log rendering.
type RenderOpts struct {
	CurrentBranch string
	ShowAll       bool                             // Show all stacks, not just current
	Reverse       bool                             // Reverse order (trunk at bottom)
	CommitsFn     func(branch string) []CommitInfo // Optional: resolve commits per branch

	// AnnotateFn appends caller-supplied text to a branch's line (PR number,
	// GitHub stack, ...). The graph package deliberately knows nothing about
	// pull requests; callers that do pass the formatting in here.
	AnnotateFn func(branch string) string

	// NeedsRestackFn reports whether a branch is no longer built on its
	// parent's tip. The graph package can't answer that itself — it takes
	// git ancestry, not recorded revisions — so callers pass the check in.
	NeedsRestackFn func(branch string) bool
}

// CommitInfo holds info about a single commit for rendering.
type CommitInfo struct {
	ShortSHA string
	Subject  string
}

// Row is one rendered line of the tree. Branch names the branch the line
// stands for, and is empty for connector and commit lines. Interactive
// callers need that association to map a cursor position back to a branch;
// RenderTree itself only ever uses Line.
type Row struct {
	Line   string
	Branch string
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
	rows := g.RenderTreeRows(opts)
	if len(rows) == 0 {
		return "No trunk branch found\n"
	}
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(r.Line)
		b.WriteString("\n")
	}
	return b.String()
}

// RenderTreeRows produces the same tree as RenderTree, but as one Row per
// line so callers can tell which branch (if any) each line belongs to.
// Returns nil when there is no trunk.
func (g *Graph) RenderTreeRows(opts RenderOpts) []Row {
	trunk := g.TrunkName()
	if trunk == "" {
		return nil
	}

	children := g.ChildrenOf(trunk)
	if len(children) == 0 {
		return []Row{{Line: g.formatBranch(trunk, opts), Branch: trunk}}
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

	var rows []Row
	g.renderNode(trunk, opts, &rows)

	return rows
}

// renderNode recursively renders a branch and its subtree.
// Children appear above the branch (tips at top, trunk at bottom).
// When a branch has multiple children, the primary child (containing the
// current branch) gets the straight │ line; siblings branch off with ├─┘.
func (g *Graph) renderNode(name string, opts RenderOpts, rows *[]Row) {
	children := g.ChildrenOf(name)

	if len(children) == 0 {
		// Leaf — just render the branch and its commits.
		*rows = append(*rows, Row{Line: g.formatBranch(name, opts), Branch: name})
		g.appendCommits(name, opts, rows)
		return
	}

	// Pick the primary child (the one containing the current branch).
	primary := g.pickPrimary(children, opts.CurrentBranch)
	others := without(children, primary)

	// 1. Render primary subtree (appears at the top of output).
	g.renderNode(primary, opts, rows)
	*rows = append(*rows, Row{Line: "│"})

	// 2. Render side branches with │ prefix and ├─┘ connector. Indenting
	//    rewrites the line but must preserve which branch it stands for.
	for _, child := range others {
		var side []Row
		g.renderNode(child, opts, &side)
		for _, sr := range side {
			*rows = append(*rows, Row{Line: "│ " + sr.Line, Branch: sr.Branch})
		}
		*rows = append(*rows, Row{Line: "├─┘"})
	}

	// 3. Render this node.
	*rows = append(*rows, Row{Line: g.formatBranch(name, opts), Branch: name})
	g.appendCommits(name, opts, rows)
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
	if opts.NeedsRestackFn != nil && opts.NeedsRestackFn(name) {
		suffix += " [needs restack]"
	}

	if opts.AnnotateFn != nil {
		if note := opts.AnnotateFn(name); note != "" {
			suffix += " " + note
		}
	}

	pointer := ""
	if name == opts.CurrentBranch {
		pointer = " ←"
	}

	return fmt.Sprintf("%s %s%s%s", marker, name, suffix, pointer)
}

func (g *Graph) appendCommits(name string, opts RenderOpts, rows *[]Row) {
	if opts.CommitsFn == nil || g.IsTrunk(name) {
		return
	}
	commits := opts.CommitsFn(name)
	for _, c := range commits {
		*rows = append(*rows, Row{Line: fmt.Sprintf("│   %s %s", c.ShortSHA, c.Subject)})
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
