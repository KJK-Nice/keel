package session

import "testing"

func TestParseSize(t *testing.T) {
	cols, rows := ParseSize("120", "40")
	if cols != 120 || rows != 40 {
		t.Fatalf("got %d x %d", cols, rows)
	}
	cols, rows = ParseSize("", "")
	if cols != 80 || rows != 24 {
		t.Fatalf("defaults: %d x %d", cols, rows)
	}
}
