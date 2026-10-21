package runtime

import (
	"testing"

	"keel/internal/scrollback"
)

func TestLastLine(t *testing.T) {
	cases := map[string]string{
		"a\r\nb\r\n$ ":                     "$",
		"x\r\n\x1b[1;32mme@web\x1b[0m:~$ ": "me@web:~$",
		"\x1b]0;title\x07\x1b]133;A\x07$ ": "$",
		"Password: ":                       "Password:",
		"old line\rnew line":               "new line",
		"\r\nEnter passphrase for key '/home/me/.ssh/id_ed25519': ": "Enter passphrase for key '/home/me/.ssh/id_ed25519':",
		"\u009b1mbold\u009b0m> ":                                    "bold>",
	}
	for in, want := range cases {
		if got := LastLine(scrollback.Read([]byte(in))); got != want {
			t.Errorf("LastLine(%q) = %q, want %q", in, got, want)
		}
	}
}
