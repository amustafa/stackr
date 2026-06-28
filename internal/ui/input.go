package ui

import (
	"fmt"
	"os"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type inputModel struct {
	prompt    string
	textInput textinput.Model
	value     string
	done      bool
	cancelled bool
}

func newInputModel(prompt string) inputModel {
	ti := textinput.New()
	ti.Focus()
	ti.CharLimit = 200
	ti.Width = 60

	return inputModel{
		prompt:    prompt,
		textInput: ti,
	}
}

func (m inputModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m inputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			m.value = m.textInput.Value()
			m.done = true
			return m, tea.Quit
		case "esc", "ctrl+c":
			m.cancelled = true
			m.done = true
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m inputModel) View() string {
	return titleStyle.Render(m.prompt) + "\n\n" + m.textInput.View() + "\n" +
		normalStyle.Render("  enter submit • esc cancel")
}

// Input presents an interactive single-line text input and returns the entered text.
func Input(prompt string) (string, error) {
	m := newInputModel(prompt)
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))
	result, err := p.Run()
	if err != nil {
		return "", fmt.Errorf("input error: %w", err)
	}

	final := result.(inputModel)
	if final.cancelled {
		return "", ErrCancelled
	}
	return final.value, nil
}
