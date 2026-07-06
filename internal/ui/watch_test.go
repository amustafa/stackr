package ui

import "testing"

func TestOrderRowsAwaitingFirst(t *testing.T) {
	in := []WatchRow{
		{Branch: "a", Awaiting: false},
		{Branch: "b", Awaiting: true},
		{Branch: "c", Awaiting: false},
		{Branch: "d", Awaiting: true},
	}
	ordered, n := orderRows(in)
	if n != 2 {
		t.Fatalf("nAwait = %d, want 2", n)
	}
	if !ordered[0].Awaiting || !ordered[1].Awaiting {
		t.Fatalf("awaiting rows should come first: %+v", ordered)
	}
	if ordered[2].Awaiting || ordered[3].Awaiting {
		t.Fatalf("non-awaiting rows should follow: %+v", ordered)
	}
}

func TestRowAtLine(t *testing.T) {
	m := watchModel{
		rows:   []WatchRow{{Branch: "b", Awaiting: true}, {Branch: "d", Awaiting: true}, {Branch: "a"}, {Branch: "c"}},
		nAwait: 2,
	}
	// Layout: title(0) blank(1) AWAITING hdr(2) await0(3) await1(4) blank(5) ALL hdr(6) rest0(7) rest1(8)
	if got := m.rowAtLine(3); got != 0 {
		t.Errorf("line 3 -> %d, want 0", got)
	}
	if got := m.rowAtLine(4); got != 1 {
		t.Errorf("line 4 -> %d, want 1", got)
	}
	if got := m.rowAtLine(7); got != 2 {
		t.Errorf("line 7 -> %d, want 2", got)
	}
	if got := m.rowAtLine(2); got != -1 { // header line, not a row
		t.Errorf("header line -> %d, want -1", got)
	}
}
