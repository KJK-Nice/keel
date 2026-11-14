package randid

import "testing"

func TestHex(t *testing.T) {
	a, b := Hex(8), Hex(8)
	if len(a) != 16 || a == b {
		t.Fatalf("got %q and %q", a, b)
	}
}
