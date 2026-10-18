package shellmark

import "testing"

// The parser reads OSCs the way xterm.js's VT500 parser does, so the
// marks keel serve records are the marks the terminal draws.
func TestMatchesXtermOSCRules(t *testing.T) {
	c1osc, c1st := "\xc2\x9d", "\xc2\x9c"
	cases := []struct {
		name, in, kinds string
	}{
		{"C1 introducer and BEL", c1osc + "133;A" + bel, "A"},
		{"C1 introducer and C1 ST", c1osc + "133;D;4" + c1st, "D"},
		{"ESC ] and C1 ST", esc + "]133;B" + c1st, "B"},
		{"C0 inside is dropped", esc + "]133;\x01A\x1f" + bel, "A"},
		{"any ESC ends it", esc + "]133;A" + esc + "x" + osc("133;C"), "AC"},
		{"ESC ] ends one and starts another", esc + "]133;A" + esc + "]133;C" + bel, "AC"},
		{"C1 OSC inside one restarts it", esc + "]133;Z" + c1osc + "133;C" + bel, "C"},
		{"ESC then C1 OSC", esc + c1osc + "133;A" + bel, "A"},
		{"leading zeros in the id", osc("0133;A") + osc("007;file://h/x"), "A7"},
		{"text with C2 around", "\xc2\xa9 " + osc("133;A") + "\xc2\xb0", "A"},
	}
	for _, c := range cases {
		got := collect(c.in)
		if kinds(got) != c.kinds {
			t.Errorf("%s: %q", c.name, kinds(got))
		}
		for i := 0; i <= len(c.in); i++ {
			if s := collect(c.in[:i], c.in[i:]); !equal(s, got) {
				t.Errorf("%s: split at %d = %+v, want %+v", c.name, i, s, got)
			}
		}
	}
	// The ESC that ends an OSC is not part of it: End stops before it.
	if ms := collect(esc + "]133;C" + esc + "x"); len(ms) != 1 || ms[0].End != 7 {
		t.Errorf("end = %+v", ms)
	}
	if ms := collect(c1osc + "133;C" + c1st + "out"); len(ms) != 1 || ms[0].Start != 0 || ms[0].End != 9 {
		t.Errorf("C1 offsets = %+v", ms)
	}
}
