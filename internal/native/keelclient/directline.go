package keelclient

import (
	"unicode"
	"unicode/utf8"
)

// DirectLine reports whether command stays one line on a shell prompt as typed: not empty, and with no C0 control
// (newline, CR, tab, ESC), no DEL, no C1 control, no line or paragraph separator and no invisible format character.
// It is the same set as keel serve's service.DirectLine (cmd/keel/web/js/lib/typedirect.js directLine). DirectType
// refuses anything else before it sends a frame, and the window's hint uses it too.
func DirectLine(command string) bool {
	if command == "" || !utf8.ValidString(command) { // JSON would turn a bad byte into U+FFFD: not the bytes given
		return false
	}
	for _, r := range command {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) || r == 0x2028 || r == 0x2029 || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}
