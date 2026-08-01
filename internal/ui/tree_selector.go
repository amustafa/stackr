package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// TreeItem is one line of a tree picker.
//
// Line is the pre-rendered tree line, connectors included. Value is the branch
// the line stands for, empty for connector and commit lines, which are drawn
// but never selectable. Reason is why the branch cannot be chosen; empty means
// it can be.
type TreeItem struct {
	Line   string
	Value  string
	Reason string
}

func (it TreeItem) selectable() bool { return it.Value != "" && it.Reason == "" }

// TreeSelectOpts configures a tree picker.
type TreeSelectOpts struct {
	Title string
	Items []TreeItem

	// SkipWhenSingle returns the only selectable item immediately, without
	// drawing anything. Correct for navigational choices — checkout, up — where
	// picking is just a way to get somewhere and there is only one somewhere to
	// go. Wrong for destructive ones: the whole reason a move picker opened is
	// that the user had not decided yet, and silently performing a rebase they
	// never confirmed is not a shortcut. Leave it false unless the selection is
	// purely navigational.
	SkipWhenSingle bool
}

// viewRow is one rendered row of the current view, which is either the full
// tree (no filter) or a flat list of matching branches (filter active).
type viewRow struct {
	text       string
	value      string
	reason     string
	selectable bool
}

type treeModel struct {
	title    string
	input    textinput.Model
	all      []TreeItem
	view     []viewRow
	cursor   int
	offset   int
	height   int
	selected string
}

func (m treeModel) Init() tea.Cmd { return textinput.Blink }

// rebuild recomputes the view for the current filter.
//
// With no filter the full tree is shown, connectors and all. With a filter the
// tree collapses to a flat list of matching branches in tree order: a pruned
// tree cannot keep its connectors honest, and re-rooting matches under trunk
// would draw a parent relationship that does not exist — unacceptable on the
// one screen whose entire purpose is choosing a parent.
func (m *treeModel) rebuild() {
	prev := ""
	if m.cursor < len(m.view) {
		prev = m.view[m.cursor].value
	}

	q := strings.ToLower(m.input.Value())
	m.view = m.view[:0]
	for _, it := range m.all {
		if q == "" {
			m.view = append(m.view, viewRow{
				text: it.Line, value: it.Value, reason: it.Reason, selectable: it.selectable(),
			})
			continue
		}
		if it.Value == "" || !strings.Contains(strings.ToLower(it.Value), q) {
			continue
		}
		m.view = append(m.view, viewRow{
			text: it.Value, value: it.Value, reason: it.Reason, selectable: it.selectable(),
		})
	}

	// Keep the cursor on the same branch across a filter change when we can,
	// otherwise land it on the first selectable row.
	m.cursor = 0
	m.offset = 0
	if len(m.view) == 0 {
		return
	}
	if prev != "" {
		for i, r := range m.view {
			if r.value == prev && r.selectable {
				m.cursor = i
				break
			}
		}
	}
	if !m.view[m.cursor].selectable {
		m.cursor = m.seek(-1, +1)
	}
	m.scrollToCursor()
}

// seek returns the index of the next selectable row strictly after from in
// direction dir, or from itself when there is none.
func (m treeModel) seek(from, dir int) int {
	for i := from + dir; i >= 0 && i < len(m.view); i += dir {
		if m.view[i].selectable {
			return i
		}
	}
	if from >= 0 && from < len(m.view) && m.view[from].selectable {
		return from
	}
	// Nothing selectable in that direction; fall back to the first one anywhere.
	for i, r := range m.view {
		if r.selectable {
			return i
		}
	}
	return 0
}

func (m *treeModel) scrollToCursor() {
	if m.height <= 0 {
		m.height = 20
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+m.height {
		m.offset = m.cursor - m.height + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m treeModel) anySelectable() bool {
	for _, r := range m.view {
		if r.selectable {
			return true
		}
	}
	return false
}

func (m treeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Leave room for the title, filter, blank line and help footer.
		m.height = msg.Height - 5
		if m.height < 3 {
			m.height = 3
		}
		m.scrollToCursor()
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if m.cursor < len(m.view) && m.view[m.cursor].selectable {
				m.selected = m.view[m.cursor].value
				return m, tea.Quit
			}
			return m, nil
		case "esc", "ctrl+c":
			return m, tea.Quit
		case "up", "ctrl+p":
			m.cursor = m.seek(m.cursor, -1)
			m.scrollToCursor()
			return m, nil
		case "down", "ctrl+n":
			m.cursor = m.seek(m.cursor, +1)
			m.scrollToCursor()
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.rebuild()
	return m, cmd
}

func (m treeModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.title))
	b.WriteString("\n")
	b.WriteString(m.input.View())
	b.WriteString("\n\n")

	end := m.offset + m.height
	if end > len(m.view) {
		end = len(m.view)
	}
	for i := m.offset; i < end; i++ {
		r := m.view[i]
		gutter := "  "
		if i == m.cursor && r.selectable {
			gutter = "> "
		}
		line := gutter + r.text
		switch {
		case i == m.cursor && r.selectable:
			b.WriteString(selectedStyle.Render(line))
		case r.selectable:
			b.WriteString(normalStyle.Render(line))
		default:
			b.WriteString(dimStyle.Render(line))
		}
		if r.reason != "" {
			b.WriteString("  " + dimStyle.Render(r.reason))
		}
		b.WriteString("\n")
	}

	if len(m.view) == 0 {
		b.WriteString(dimStyle.Render("  (no matches)\n"))
	} else if !m.anySelectable() {
		b.WriteString(dimStyle.Render("  (nothing here can be selected)\n"))
	}
	if m.offset > 0 || end < len(m.view) {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  … %d–%d of %d\n", m.offset+1, end, len(m.view))))
	}

	b.WriteString(dimStyle.Render("\n  type to filter • ↑/↓ move • enter select • esc cancel"))
	return b.String()
}

// TreeSelect presents a scrolling, filterable tree picker and returns the
// chosen Value. Rows that cannot be chosen are drawn greyed with their reason
// and skipped by the cursor, so every enter press lands on something valid.
// Returns ErrCancelled if the user escapes.
func TreeSelect(opts TreeSelectOpts) (string, error) {
	var selectable []string
	for _, it := range opts.Items {
		if it.selectable() {
			selectable = append(selectable, it.Value)
		}
	}
	if len(selectable) == 0 {
		return "", fmt.Errorf("no selectable items")
	}
	if opts.SkipWhenSingle && len(selectable) == 1 {
		return selectable[0], nil
	}

	ti := textinput.New()
	ti.Placeholder = "filter…"
	ti.Focus()

	m := treeModel{title: opts.Title, input: ti, all: opts.Items, height: 20}
	m.rebuild()

	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))
	res, err := p.Run()
	if err != nil {
		return "", fmt.Errorf("selector error: %w", err)
	}
	final := res.(treeModel)
	if final.selected == "" {
		return "", ErrCancelled
	}
	return final.selected, nil
}
