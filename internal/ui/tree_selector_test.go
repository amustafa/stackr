package ui

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// moveLikeItems mirrors what the move picker feeds in: a rendered tree where
// the branches above the source are cycle-greyed, the parent is greyed as a
// no-op, and only trunk and a branch on another stack can be chosen.
//
//	◯ c        would create a cycle
//	│
//	◉ b        can't move a branch onto itself
//	│
//	◯ a        already the parent
//	│
//	│ ◯ x      (selectable)
//	├─┘
//	◯ main     (selectable)
func moveLikeItems() []TreeItem {
	return []TreeItem{
		{Line: "◯ c", Value: "c", Reason: "would create a cycle"},
		{Line: "│"},
		{Line: "◉ b", Value: "b", Reason: "can't move a branch onto itself"},
		{Line: "│"},
		{Line: "◯ a", Value: "a", Reason: "already the parent"},
		{Line: "│"},
		{Line: "│ ◯ x", Value: "x"},
		{Line: "├─┘"},
		{Line: "◯ main", Value: "main"},
	}
}

func newTestModel(items []TreeItem) treeModel {
	ti := textinput.New()
	m := treeModel{title: "Move b onto:", input: ti, all: items, height: 20}
	m.rebuild()
	return m
}

func press(t *testing.T, m treeModel, key tea.KeyType) treeModel {
	t.Helper()
	next, _ := m.Update(tea.KeyMsg{Type: key})
	return next.(treeModel)
}

func TestCursorStartsOnFirstSelectableRow(t *testing.T) {
	m := newTestModel(moveLikeItems())
	if got := m.view[m.cursor].value; got != "x" {
		t.Errorf("cursor started on %q, want x — greyed and connector rows must be skipped", got)
	}
}

func TestCursorSkipsGreyedAndConnectorRows(t *testing.T) {
	m := newTestModel(moveLikeItems())

	m = press(t, m, tea.KeyDown)
	if got := m.view[m.cursor].value; got != "main" {
		t.Errorf("after down, cursor on %q, want main", got)
	}
	// At the end it should stay put rather than walking onto a connector.
	m = press(t, m, tea.KeyDown)
	if got := m.view[m.cursor].value; got != "main" {
		t.Errorf("after down at the end, cursor on %q, want main", got)
	}

	m = press(t, m, tea.KeyUp)
	if got := m.view[m.cursor].value; got != "x" {
		t.Errorf("after up, cursor on %q, want x", got)
	}
	m = press(t, m, tea.KeyUp)
	if got := m.view[m.cursor].value; got != "x" {
		t.Errorf("after up at the start, cursor on %q, want x — it must not land on the greyed rows above", got)
	}
}

func TestEnterSelectsTheCursorRow(t *testing.T) {
	m := newTestModel(moveLikeItems())
	m = press(t, m, tea.KeyDown)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := next.(treeModel).selected; got != "main" {
		t.Errorf("selected %q, want main", got)
	}
}

// Filtering collapses the tree to a flat list: connectors are dropped and each
// row becomes the branch name.
func TestFilterCollapsesToFlatList(t *testing.T) {
	m := newTestModel(moveLikeItems())
	m.input.SetValue("ma")
	m.rebuild()

	if len(m.view) != 1 {
		t.Fatalf("filtered view has %d rows, want 1: %+v", len(m.view), m.view)
	}
	if m.view[0].text != "main" {
		t.Errorf("row text = %q, want the bare branch name main", m.view[0].text)
	}
	if !m.view[0].selectable {
		t.Error("main should still be selectable under a filter")
	}
}

// Greyed branches still appear under a filter — the whole point of greying
// rather than omitting is that a branch someone looked for is never absent.
func TestFilterKeepsIneligibleRowsVisible(t *testing.T) {
	m := newTestModel(moveLikeItems())
	m.input.SetValue("a")
	m.rebuild()

	var texts []string
	for _, r := range m.view {
		texts = append(texts, r.text)
	}
	if len(texts) != 2 || texts[0] != "a" || texts[1] != "main" {
		t.Fatalf("filtered rows = %v, want [a main]", texts)
	}
	if m.view[0].reason != "already the parent" {
		t.Errorf("reason for a = %q, want it preserved under filtering", m.view[0].reason)
	}
	if got := m.view[m.cursor].value; got != "main" {
		t.Errorf("cursor on %q, want main — it must skip the greyed match", got)
	}
}

func TestFilterWithNoMatchesIsEmptyAndSafe(t *testing.T) {
	m := newTestModel(moveLikeItems())
	m.input.SetValue("zzz")
	m.rebuild() // must not panic on an empty view

	if len(m.view) != 0 {
		t.Errorf("view has %d rows, want 0", len(m.view))
	}
	if m.anySelectable() {
		t.Error("anySelectable on an empty view")
	}
	// Rendering and keypresses must survive the empty view too.
	_ = m.View()
	m = press(t, m, tea.KeyDown)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := next.(treeModel).selected; got != "" {
		t.Errorf("selected %q from an empty view", got)
	}
}

func TestTreeSelectRejectsNothingSelectable(t *testing.T) {
	items := []TreeItem{
		{Line: "◯ a", Value: "a", Reason: "already the parent"},
		{Line: "│"},
	}
	if _, err := TreeSelect(TreeSelectOpts{Title: "t", Items: items}); err == nil {
		t.Error("TreeSelect succeeded with no selectable items")
	}
}

// SkipWhenSingle is the opt-in that navigational pickers want and destructive
// ones must not have. Move leaves it false so a lone candidate is still shown.
func TestSkipWhenSingleOnlyAppliesWhenOptedIn(t *testing.T) {
	items := []TreeItem{
		{Line: "◯ a", Value: "a", Reason: "already the parent"},
		{Line: "◯ main", Value: "main"},
	}
	got, err := TreeSelect(TreeSelectOpts{Title: "t", Items: items, SkipWhenSingle: true})
	if err != nil {
		t.Fatalf("TreeSelect: %v", err)
	}
	if got != "main" {
		t.Errorf("auto-selected %q, want main", got)
	}
}
