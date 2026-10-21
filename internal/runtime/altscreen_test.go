package runtime

import (
	"strings"
	"testing"
)

func TestAltScreenFollowsPrivateModes(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want bool
	}{
		{"plain output", []string{"hello\r\n$ "}, false},
		{"vim enters 1049", []string{"\x1b[?1049h\x1b[22;0;0t"}, true},
		{"enter then leave", []string{"\x1b[?1049h", "text", "\x1b[?1049l"}, false},
		{"47 and 1047", []string{"\x1b[?47h"}, true},
		{"1047 leave", []string{"\x1b[?1047h", "\x1b[?1047l"}, false},
		{"several params", []string{"\x1b[?1;1049h"}, true},
		{"split over reads", []string{"\x1b", "[?10", "49h"}, true},
		{"other private mode", []string{"\x1b[?25l\x1b[?2004h"}, false},
		{"not private", []string{"\x1b[1049h"}, false},
		{"ESC inside resets", []string{"\x1b[?10\x1b[?1049h"}, true},
		// Too long to read: fails closed (xterm.js reads it).
		{"too long param", []string{"\x1b[?" + strings.Repeat("0;", 20) + "1049h"}, true},
		{"too long then leave", []string{"\x1b[?" + strings.Repeat("0;", 20) + "1049h", "\x1b[?1049l"}, false},
		// The 8-bit CSI, as UTF-8 (c2 9b), as xterm.js reads it.
		{"8-bit CSI", []string{"\u009b?1049h"}, true},
		{"8-bit CSI split", []string{"\xc2", "\x9b?1049h"}, true},
		{"8-bit CSI leave", []string{"\x1b[?1049h", "\u009b?1049l"}, false},
		{"other c2 char", []string{"\u00a0?1049h"}, false},
		// ':' sub-parameters and leading zeros, as xterm.js reads them.
		{"sub-parameter", []string{"\x1b[?1049:1h"}, true},
		{"sub-parameter leave", []string{"\x1b[?1049h", "\x1b[?1049:1l"}, false},
		{"leading zeros", []string{"\x1b[?01049h"}, true},
		// Anything unread in a sequence naming the alt screen fails closed.
		{"intermediate byte", []string{"\x1b[?1049$h"}, true},
		{"unread leave", []string{"\x1b[?1049h", "\x1b[?1049 l"}, true},
		{"unread other mode", []string{"\x1b[?25$h"}, false},
		// RIS resets to the normal screen.
		{"RIS", []string{"\x1b[?1049h", "\x1bc"}, false},
	}
	for _, c := range cases {
		var a altScreen
		for _, s := range c.in {
			a.Feed([]byte(s))
		}
		if a.on != c.want {
			t.Errorf("%s: on = %v, want %v", c.name, a.on, c.want)
		}
	}
}

func TestAltScreenReset(t *testing.T) {
	var a altScreen
	a.Feed([]byte("\x1b[?1049h"))
	a.Reset()
	if a.on {
		t.Fatal("a new shell starts on the normal screen")
	}
	a.Feed([]byte("[?1049h"))
	if a.on {
		t.Fatal("a reset drops a half-read sequence")
	}
}
