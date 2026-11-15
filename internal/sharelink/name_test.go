package sharelink

import (
	"strings"
	"testing"
)

func TestSanitizeName(t *testing.T) {
	const rlo, lri, rlm = "\xe2\x80\xae", "\xe2\x81\xa6", "\xe2\x80\x8f" // U+202E, U+2066, U+200F
	for in, want := range map[string]string{
		"  bob ":                   "bob",
		"":                         "guest",
		rlo + lri + rlm + "\x07\t": "guest",
		"al" + rlo + "ice\x1b[31m": "alice[31m",
		"línea\r\nfake: approved":  "líneafake: approved",
		strings.Repeat("é", 40):    strings.Repeat("é", 32),
	} {
		if got := SanitizeName(in); got != want {
			t.Errorf("SanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}
