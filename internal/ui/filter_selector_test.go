package ui

import "testing"

func TestFilterItemMatches(t *testing.T) {
	it := FilterItem{Value: "feat-auth", Label: "feat-auth", Detail: "awaiting-input — JWT or sessions?"}
	cases := map[string]bool{
		"":         true,  // empty query matches all
		"feat":     true,  // label substring
		"AUTH":     true,  // case-insensitive
		"jwt":      true,  // detail substring
		"sessions": true,  // detail substring
		"nomatch":  false, // no match
	}
	for q, want := range cases {
		if got := it.matches(q); got != want {
			t.Errorf("matches(%q) = %v, want %v", q, got, want)
		}
	}
}

func TestRefilterKeepsCursorInBounds(t *testing.T) {
	m := filterModel{all: []FilterItem{
		{Label: "alpha"}, {Label: "beta"}, {Label: "gamma"},
	}}
	m.cursor = 2
	m.input.SetValue("a") // matches alpha, beta(no), gamma -> alpha, gamma
	m.refilter()
	if m.cursor >= len(m.filtered) {
		t.Fatalf("cursor %d out of bounds for %d filtered", m.cursor, len(m.filtered))
	}
	m.input.SetValue("zzz")
	m.refilter()
	if len(m.filtered) != 0 || m.cursor != 0 {
		t.Fatalf("empty filter should reset cursor to 0, got cursor=%d filtered=%d", m.cursor, len(m.filtered))
	}
}
