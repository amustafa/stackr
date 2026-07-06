package ui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// WatchRow is one sandbox line in the watch dashboard.
type WatchRow struct {
	Branch   string
	State    string
	Reason   string
	Awaiting bool
}

var (
	headerStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	awaitStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	paneStyle    = lipgloss.NewStyle().Padding(0, 2)
	detailKey    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
)

type watchModel struct {
	title  string
	fetch  func() []WatchRow
	attach func(string) *exec.Cmd
	rows   []WatchRow // awaiting first, then the rest
	nAwait int
	cursor int
	quit   bool
}

type watchTickMsg struct{}
type watchRowsMsg []WatchRow

func watchTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return watchTickMsg{} })
}

func (m watchModel) fetchCmd() tea.Cmd {
	return func() tea.Msg { return watchRowsMsg(m.fetch()) }
}

func (m watchModel) Init() tea.Cmd {
	return tea.Batch(m.fetchCmd(), watchTick())
}

// orderRows puts awaiting rows first and reports how many there are.
func orderRows(in []WatchRow) (ordered []WatchRow, nAwait int) {
	var await, rest []WatchRow
	for _, r := range in {
		if r.Awaiting {
			await = append(await, r)
		} else {
			rest = append(rest, r)
		}
	}
	return append(await, rest...), len(await)
}

func (m watchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case watchTickMsg:
		return m, tea.Batch(m.fetchCmd(), watchTick())
	case watchRowsMsg:
		m.rows, m.nAwait = orderRows([]WatchRow(msg))
		if m.cursor >= len(m.rows) {
			m.cursor = len(m.rows) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.quit = true
			return m, tea.Quit
		case "up", "k", "ctrl+p":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j", "ctrl+n":
			if m.cursor < len(m.rows)-1 {
				m.cursor++
			}
		case "a": // jump to first awaiting
			if m.nAwait > 0 {
				m.cursor = 0
			}
		case "enter":
			if len(m.rows) > 0 && m.attach != nil {
				return m, tea.ExecProcess(m.attach(m.rows[m.cursor].Branch), func(error) tea.Msg { return nil })
			}
		}
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress {
			if idx := m.rowAtLine(msg.Y); idx >= 0 && idx < len(m.rows) {
				m.cursor = idx
				if m.attach != nil {
					return m, tea.ExecProcess(m.attach(m.rows[m.cursor].Branch), func(error) tea.Msg { return nil })
				}
			}
		}
	}
	return m, nil
}

// rowAtLine maps a screen Y to a row index given the left-pane layout below.
// Layout: title(0), blank(1), "AWAITING INPUT"(2), await rows(3..), blank,
// "ALL"(hdr), rest rows. Best-effort for mouse clicks.
func (m watchModel) rowAtLine(y int) int {
	line := 3 // first awaiting row
	for i := 0; i < m.nAwait; i++ {
		if y == line {
			return i
		}
		line++
	}
	line += 2 // blank + "ALL" header
	for i := m.nAwait; i < len(m.rows); i++ {
		if y == line {
			return i
		}
		line++
	}
	return -1
}

func (m watchModel) View() string {
	if m.quit {
		return ""
	}
	var left strings.Builder
	left.WriteString(titleStyle.Render(m.title) + "\n\n")
	left.WriteString(headerStyle.Render("AWAITING INPUT") + "\n")
	if m.nAwait == 0 {
		left.WriteString(dimStyle.Render("  (none)") + "\n")
	}
	for i := 0; i < m.nAwait; i++ {
		left.WriteString(m.renderRow(i) + "\n")
	}
	left.WriteString("\n" + headerStyle.Render("ALL SANDBOXES") + "\n")
	for i := m.nAwait; i < len(m.rows); i++ {
		left.WriteString(m.renderRow(i) + "\n")
	}
	left.WriteString(dimStyle.Render("\n↑/↓ move • a → first awaiting • enter/click attach • q quit"))

	right := "\n\n"
	if len(m.rows) > 0 {
		r := m.rows[m.cursor]
		right = detailKey.Render("Branch  ") + r.Branch + "\n" +
			detailKey.Render("State   ") + r.State + "\n"
		if r.Reason != "" {
			right += detailKey.Render("Pending ") + wrap(r.Reason, 40) + "\n"
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		paneStyle.Width(40).Render(left.String()),
		paneStyle.Render(right))
}

func (m watchModel) renderRow(i int) string {
	r := m.rows[i]
	label := "  " + r.Branch
	if i == m.cursor {
		label = "> " + r.Branch
		if r.Awaiting {
			return awaitStyle.Render(label)
		}
		return selectedStyle.Render(label)
	}
	if r.Awaiting {
		return awaitStyle.Render(label)
	}
	return normalStyle.Render(label)
}

func wrap(s string, w int) string {
	if len(s) <= w {
		return s
	}
	var b strings.Builder
	for len(s) > w {
		b.WriteString(s[:w] + "\n        ")
		s = s[w:]
	}
	b.WriteString(s)
	return b.String()
}

// RunWatch launches the live two-pane watch dashboard. fetch is polled every
// second; attach returns the command to run (via the TUI's suspend) when a row
// is selected.
func RunWatch(title string, fetch func() []WatchRow, attach func(string) *exec.Cmd) error {
	m := watchModel{title: title, fetch: fetch, attach: attach}
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	if err != nil {
		return fmt.Errorf("watch error: %w", err)
	}
	return nil
}
