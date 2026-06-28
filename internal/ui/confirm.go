package ui

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

type confirmModel struct {
	prompt   string
	accepted bool
	done     bool
}

func (m confirmModel) Init() tea.Cmd {
	return func() tea.Msg { return nil }
}

func (m confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "Y", "enter":
			m.accepted = true
			m.done = true
			return m, tea.Quit
		case "n", "N", "esc", "ctrl+c":
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m confirmModel) View() string {
	return titleStyle.Render(m.prompt) + normalStyle.Render(" [y/n] ")
}

// Confirm presents an interactive yes/no prompt.
// Returns true for y/enter, false for n/esc.
func Confirm(prompt string) (bool, error) {
	m := confirmModel{prompt: prompt}
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))
	result, err := p.Run()
	if err != nil {
		return false, fmt.Errorf("confirm error: %w", err)
	}
	return result.(confirmModel).accepted, nil
}
