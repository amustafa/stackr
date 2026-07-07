package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// FilterItem is one selectable row: Value is returned on select, Label is the
// primary text, Detail is dimmed secondary text (both are searchable).
type FilterItem struct {
	Value  string
	Label  string
	Detail string
}

func (it FilterItem) matches(q string) bool {
	if q == "" {
		return true
	}
	q = strings.ToLower(q)
	return strings.Contains(strings.ToLower(it.Label), q) ||
		strings.Contains(strings.ToLower(it.Detail), q)
}

type filterModel struct {
	title    string
	input    textinput.Model
	all      []FilterItem
	filtered []FilterItem
	cursor   int
	selected string
	done     bool
}

func (m filterModel) Init() tea.Cmd { return textinput.Blink }

func (m *filterModel) refilter() {
	q := m.input.Value()
	m.filtered = m.filtered[:0]
	for _, it := range m.all {
		if it.matches(q) {
			m.filtered = append(m.filtered, it)
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m filterModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if len(m.filtered) > 0 {
				m.selected = m.filtered[m.cursor].Value
			}
			m.done = true
			return m, tea.Quit
		case "esc", "ctrl+c":
			m.done = true
			return m, tea.Quit
		case "up", "ctrl+p":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case "down", "ctrl+n":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.refilter()
	return m, cmd
}

func (m filterModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.title))
	b.WriteString("\n")
	b.WriteString(m.input.View())
	b.WriteString("\n\n")
	if len(m.filtered) == 0 {
		b.WriteString(dimStyle.Render("  (no matches)"))
	}
	for i, it := range m.filtered {
		line := "    " + it.Label
		if i == m.cursor {
			line = selectedStyle.Render("  > " + it.Label)
		} else {
			line = normalStyle.Render(line)
		}
		if it.Detail != "" {
			line += "  " + dimStyle.Render(it.Detail)
		}
		b.WriteString(line + "\n")
	}
	b.WriteString(dimStyle.Render("\n  type to filter • ↑/↓ move • enter select • esc cancel"))
	return b.String()
}

// FilterSelect presents a searchable selector and returns the chosen Value.
// With a single item it returns immediately. Empty list is an error.
func FilterSelect(title string, items []FilterItem) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("no items to select from")
	}
	if len(items) == 1 {
		return items[0].Value, nil
	}
	ti := textinput.New()
	ti.Placeholder = "filter…"
	ti.Focus()
	m := filterModel{title: title, input: ti, all: items}
	m.refilter()
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))
	res, err := p.Run()
	if err != nil {
		return "", fmt.Errorf("selector error: %w", err)
	}
	final := res.(filterModel)
	if final.selected == "" {
		return "", ErrCancelled
	}
	return final.selected, nil
}
