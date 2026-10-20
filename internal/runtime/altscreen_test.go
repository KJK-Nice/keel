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
		{"too long param", []string{"\x1b[?" + strings.Repeat("1;", 20) + "1049h"}, false},
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
