package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func key(k string) tea.Msg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

func specialKey(t tea.KeyType) tea.Msg {
	return tea.KeyMsg{Type: t}
}

func sampleFields() []FormField {
	return []FormField{
		{Key: "name", Label: "Name", Kind: FieldText, Value: "Alice"},
		{Key: "email", Label: "Email", Kind: FieldText, Value: "alice@test.com"},
		{Key: "readme", Label: "Create README", Kind: FieldToggle, Toggle: true},
	}
}

func TestFormNavigation(t *testing.T) {
	m := newFormModel("Test", sampleFields())

	if m.cursor != 0 {
		t.Fatalf("expected cursor at 0, got %d", m.cursor)
	}

	// Tab moves down.
	result, _ := m.Update(specialKey(tea.KeyTab))
	m = result.(formModel)
	if m.cursor != 1 {
		t.Fatalf("expected cursor at 1 after tab, got %d", m.cursor)
	}

	// Down arrow also moves down.
	result, _ = m.Update(specialKey(tea.KeyDown))
	m = result.(formModel)
	if m.cursor != 2 {
		t.Fatalf("expected cursor at 2 after down, got %d", m.cursor)
	}

	// Shift-tab moves up.
	result, _ = m.Update(specialKey(tea.KeyShiftTab))
	m = result.(formModel)
	if m.cursor != 1 {
		t.Fatalf("expected cursor at 1 after shift-tab, got %d", m.cursor)
	}

	// Up arrow moves up.
	result, _ = m.Update(specialKey(tea.KeyUp))
	m = result.(formModel)
	if m.cursor != 0 {
		t.Fatalf("expected cursor at 0 after up, got %d", m.cursor)
	}
}

func TestFormNavigationWraps(t *testing.T) {
	m := newFormModel("Test", sampleFields())
	total := len(sampleFields()) + 1 // 3 fields + confirm button

	// Up from 0 wraps to confirm button.
	result, _ := m.Update(specialKey(tea.KeyUp))
	m = result.(formModel)
	if m.cursor != total-1 {
		t.Fatalf("expected cursor to wrap to %d, got %d", total-1, m.cursor)
	}

	// Tab from confirm wraps to 0.
	result, _ = m.Update(specialKey(tea.KeyTab))
	m = result.(formModel)
	if m.cursor != 0 {
		t.Fatalf("expected cursor to wrap to 0, got %d", m.cursor)
	}
}

func TestFormToggle(t *testing.T) {
	m := newFormModel("Test", sampleFields())

	// Move to toggle field (index 2).
	m.cursor = 2
	m.updateFocus()

	if !m.fields[2].def.Toggle {
		t.Fatal("expected toggle initially true")
	}

	// Space toggles.
	result, _ := m.Update(key(" "))
	m = result.(formModel)
	if m.fields[2].def.Toggle {
		t.Fatal("expected toggle to be false after space")
	}

	// Enter also toggles on a toggle field.
	result, _ = m.Update(specialKey(tea.KeyEnter))
	m = result.(formModel)
	if !m.fields[2].def.Toggle {
		t.Fatal("expected toggle to be true after enter")
	}
}

func TestFormConfirm(t *testing.T) {
	m := newFormModel("Test", sampleFields())

	// Move to confirm button.
	m.cursor = len(m.fields)

	result, cmd := m.Update(specialKey(tea.KeyEnter))
	m = result.(formModel)
	if !m.done {
		t.Fatal("expected done after enter on confirm button")
	}
	if m.cancelled {
		t.Fatal("should not be cancelled")
	}
	if cmd == nil {
		t.Fatal("expected quit command")
	}

	r := m.result()
	if r.Values["name"] != "Alice" {
		t.Fatalf("expected name 'Alice', got %q", r.Values["name"])
	}
	if r.Values["email"] != "alice@test.com" {
		t.Fatalf("expected email 'alice@test.com', got %q", r.Values["email"])
	}
	if !r.Toggles["readme"] {
		t.Fatal("expected readme toggle to be true")
	}
}

func TestFormCtrlSConfirm(t *testing.T) {
	m := newFormModel("Test", sampleFields())

	// ctrl+s confirms from any position.
	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = result.(formModel)
	if !m.done {
		t.Fatal("expected done after ctrl+s")
	}
	if cmd == nil {
		t.Fatal("expected quit command")
	}
}

func TestFormCancel(t *testing.T) {
	m := newFormModel("Test", sampleFields())

	result, cmd := m.Update(specialKey(tea.KeyEsc))
	m = result.(formModel)
	if !m.cancelled {
		t.Fatal("expected cancelled after esc")
	}
	if !m.done {
		t.Fatal("expected done after esc")
	}
	if cmd == nil {
		t.Fatal("expected quit command")
	}
}

func TestFormRequiredValidation(t *testing.T) {
	fields := []FormField{
		{Key: "branch", Label: "Default branch", Kind: FieldText, Value: "", Required: true},
		{Key: "readme", Label: "Create README", Kind: FieldToggle, Toggle: true},
	}
	m := newFormModel("Test", fields)

	// Move to confirm and press enter.
	m.cursor = len(m.fields)
	result, _ := m.Update(specialKey(tea.KeyEnter))
	m = result.(formModel)

	// Should NOT be done — required field is empty.
	if m.done {
		t.Fatal("should not confirm with empty required field")
	}
	// Cursor should jump to the required field.
	if m.cursor != 0 {
		t.Fatalf("expected cursor to jump to required field (0), got %d", m.cursor)
	}
	if m.err == "" {
		t.Fatal("expected validation error message")
	}
}

func TestFormRequiredPassesWhenFilled(t *testing.T) {
	fields := []FormField{
		{Key: "branch", Label: "Default branch", Kind: FieldText, Value: "main", Required: true},
	}
	m := newFormModel("Test", fields)

	// Move to confirm and press enter.
	m.cursor = len(m.fields)
	result, cmd := m.Update(specialKey(tea.KeyEnter))
	m = result.(formModel)

	if !m.done {
		t.Fatal("should confirm when required field has value")
	}
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	r := m.result()
	if r.Values["branch"] != "main" {
		t.Fatalf("expected branch 'main', got %q", r.Values["branch"])
	}
}

func TestFormTextFocus(t *testing.T) {
	m := newFormModel("Test", sampleFields())

	// First text field should be focused.
	if !m.fields[0].input.Focused() {
		t.Fatal("first text field should be focused initially")
	}

	// Move to second field.
	result, _ := m.Update(specialKey(tea.KeyTab))
	m = result.(formModel)

	if m.fields[0].input.Focused() {
		t.Fatal("first field should be blurred after moving away")
	}
	if !m.fields[1].input.Focused() {
		t.Fatal("second field should be focused after tab")
	}
}

func TestFormView(t *testing.T) {
	m := newFormModel("Test Form", sampleFields())
	view := m.View()

	if view == "" {
		t.Fatal("view should not be empty")
	}
	// Basic checks — view should contain field labels and confirm button.
	for _, label := range []string{"Name", "Email", "Create README", "Confirm"} {
		if !contains(view, label) {
			t.Fatalf("view should contain %q", label)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
