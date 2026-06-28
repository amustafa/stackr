package ui

import (
	"errors"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ErrCancelled is returned when the user cancels the selector.
var ErrCancelled = errors.New("selection cancelled")

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	normalStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
)

type selectorModel struct {
	title    string
	items    []string
	cursor   int
	selected string
	done     bool
}

func (m selectorModel) Init() tea.Cmd {
	// Return a no-op command to trigger the first render cycle.
	return func() tea.Msg { return nil }
}

func (m selectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "enter":
			m.selected = m.items[m.cursor]
			m.done = true
			return m, tea.Quit
		case "q", "esc", "ctrl+c":
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m selectorModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.title))
	b.WriteString("\n\n")
	for i, item := range m.items {
		if i == m.cursor {
			b.WriteString(selectedStyle.Render(fmt.Sprintf("  > %s", item)))
		} else {
			b.WriteString(normalStyle.Render(fmt.Sprintf("    %s", item)))
		}
		b.WriteString("\n")
	}
	b.WriteString(normalStyle.Render("\n  ↑/↓ navigate • enter select • esc cancel"))
	return b.String()
}

// Select presents an interactive selector and returns the chosen item.
// If there is only one item, it is returned immediately without showing a TUI.
func Select(title string, items []string) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("no items to select from")
	}
	if len(items) == 1 {
		return items[0], nil
	}

	m := selectorModel{title: title, items: items}
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))
	result, err := p.Run()
	if err != nil {
		return "", fmt.Errorf("selector error: %w", err)
	}

	final := result.(selectorModel)
	if final.selected == "" {
		return "", ErrCancelled
	}
	return final.selected, nil
}
