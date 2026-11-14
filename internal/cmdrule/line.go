package cmdrule

import (
	"unicode"
	"unicode/utf8"
)

// DirectLine reports whether command stays one line on a shell prompt, as
// typed: not empty, valid UTF-8, and no control character (C0, which holds
// newline, CR, tab and ESC; DEL; C1), no line or paragraph separator, and
// no invisible format character (zero-width and bidi controls), which
// could show the user something else than what the shell reads.
func DirectLine(command string) bool {
	if command == "" || !utf8.ValidString(command) {
		return false
	}
	for _, r := range command {
		switch {
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
			return false
		case r == ' ', r == ' ':
			return false
		case unicode.Is(unicode.Cf, r):
			return false
		}
	}
	return true
}
