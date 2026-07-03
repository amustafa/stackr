package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// FieldKind distinguishes text input fields from boolean toggles.
type FieldKind int

const (
	FieldText   FieldKind = iota
	FieldToggle
)

// FormField describes a single field in a Form.
type FormField struct {
	Key      string
	Label    string
	Kind     FieldKind
	Value    string // pre-fill for text fields
	Toggle   bool   // initial state for toggle fields
	Required bool   // text field must be non-empty to confirm
}

// FormResult holds the values collected from a completed form.
type FormResult struct {
	Values  map[string]string
	Toggles map[string]bool
}

type formField struct {
	def   FormField
	input textinput.Model // only used for FieldText
}

type formModel struct {
	title     string
	fields    []formField
	cursor    int // index into fields; len(fields) = confirm button
	done      bool
	cancelled bool
	err       string // validation error message
}

var (
	labelStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	focusedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	toggleOnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	errStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	btnStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	btnFocusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
)

func newFormModel(title string, fields []FormField) formModel {
	ff := make([]formField, len(fields))
	for i, def := range fields {
		var ti textinput.Model
		if def.Kind == FieldText {
			ti = textinput.New()
			ti.SetValue(def.Value)
			ti.CharLimit = 200
			ti.Width = 50
			if i == 0 {
				ti.Focus()
			}
		}
		ff[i] = formField{def: def, input: ti}
	}
	return formModel{title: title, fields: ff}
}

func (m formModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m formModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		m.err = ""
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			m.done = true
			return m, tea.Quit

		case "tab", "down":
			return m.moveCursor(1), nil

		case "shift+tab", "up":
			return m.moveCursor(-1), nil

		case "enter":
			if m.onConfirmButton() {
				return m.tryConfirm()
			}
			if m.cursor < len(m.fields) && m.fields[m.cursor].def.Kind == FieldToggle {
				m.fields[m.cursor].def.Toggle = !m.fields[m.cursor].def.Toggle
				return m, nil
			}
			return m.moveCursor(1), nil

		case " ":
			if m.cursor < len(m.fields) && m.fields[m.cursor].def.Kind == FieldToggle {
				m.fields[m.cursor].def.Toggle = !m.fields[m.cursor].def.Toggle
				return m, nil
			}

		case "ctrl+s":
			return m.tryConfirm()
		}
	}

	if m.cursor < len(m.fields) && m.fields[m.cursor].def.Kind == FieldText {
		var cmd tea.Cmd
		m.fields[m.cursor].input, cmd = m.fields[m.cursor].input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m formModel) moveCursor(delta int) formModel {
	total := len(m.fields) + 1 // fields + confirm button
	m.cursor = (m.cursor + delta + total) % total
	m.updateFocus()
	return m
}

func (m formModel) onConfirmButton() bool {
	return m.cursor == len(m.fields)
}

func (m *formModel) updateFocus() {
	for i := range m.fields {
		if m.fields[i].def.Kind == FieldText {
			if i == m.cursor {
				m.fields[i].input.Focus()
			} else {
				m.fields[i].input.Blur()
			}
		}
	}
}

func (m formModel) tryConfirm() (tea.Model, tea.Cmd) {
	for i, f := range m.fields {
		if f.def.Kind == FieldText && f.def.Required && strings.TrimSpace(f.input.Value()) == "" {
			m.cursor = i
			m.updateFocus()
			m.err = fmt.Sprintf("%s is required", f.def.Label)
			return m, nil
		}
	}
	m.done = true
	return m, tea.Quit
}

func (m formModel) result() *FormResult {
	r := &FormResult{
		Values:  make(map[string]string),
		Toggles: make(map[string]bool),
	}
	for _, f := range m.fields {
		switch f.def.Kind {
		case FieldText:
			r.Values[f.def.Key] = f.input.Value()
		case FieldToggle:
			r.Toggles[f.def.Key] = f.def.Toggle
		}
	}
	return r
}

func (m formModel) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render(m.title))
	b.WriteString("\n\n")

	for i, f := range m.fields {
		focused := i == m.cursor
		b.WriteString(m.renderField(f, focused))
		b.WriteString("\n")
	}

	// Confirm button
	if m.onConfirmButton() {
		b.WriteString(btnFocusStyle.Render("  ▸ [ Confirm ]"))
	} else {
		b.WriteString(btnStyle.Render("    [ Confirm ]"))
	}
	b.WriteString("\n")

	if m.err != "" {
		b.WriteString("\n")
		b.WriteString(errStyle.Render("  ⚠ " + m.err))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  tab/↑↓ navigate • enter select • ctrl+s confirm • esc cancel"))
	return b.String()
}

func (m formModel) renderField(f formField, focused bool) string {
	prefix := "    "
	if focused {
		prefix = focusedStyle.Render("  ▸ ")
	}

	label := labelStyle.Render(f.def.Label)

	switch f.def.Kind {
	case FieldText:
		return prefix + label + "\n    " + f.input.View()
	case FieldToggle:
		val := dimStyle.Render("[no]")
		if f.def.Toggle {
			val = toggleOnStyle.Render("[yes]")
		}
		return prefix + label + "  " + val
	}
	return ""
}

// Form presents a multi-field interactive form and returns the collected values.
func Form(title string, fields []FormField) (*FormResult, error) {
	m := newFormModel(title, fields)
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))
	result, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("form error: %w", err)
	}

	final := result.(formModel)
	if final.cancelled {
		return nil, ErrCancelled
	}
	return final.result(), nil
}
