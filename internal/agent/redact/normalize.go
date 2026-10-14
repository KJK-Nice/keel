package redact

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	// CSI (colors, cursor moves), OSC (titles, hyperlinks), and the
	// two-byte and charset escapes a shell or full-screen program emits.
	ansiCSI     = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	ansiOSC     = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)?`)
	ansiCharset = regexp.MustCompile(`\x1b[()*+][0-9A-Za-z]`)
	ansiOther   = regexp.MustCompile(`\x1b[@-Z\\-_=>78]`)
)

// Normalize turns raw PTY output into plain text: escape sequences go,
// CRLF and lone CR become LF, backspace erases the character before it,
// and other control characters (except tab and newline) are dropped. The
// detectors then see a secret split by color codes or a redraw as one
// string. Text sent to a provider is this plain form, never raw bytes.
func Normalize(s string) string {
	if strings.IndexByte(s, 0x1b) >= 0 {
		s = ansiOSC.ReplaceAllString(s, "")
		s = ansiCSI.ReplaceAllString(s, "")
		s = ansiCharset.ReplaceAllString(s, "")
		s = ansiOther.ReplaceAllString(s, "")
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	b := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r == '\r':
			b = append(b, '\n')
		case r == '\b':
			if n := len(b); n > 0 && b[n-1] != '\n' {
				// Drop the last rune.
				i := n - 1
				for i > 0 && b[i]&0xC0 == 0x80 {
					i--
				}
				b = b[:i]
			}
		case r == '\n' || r == '\t':
			b = append(b, byte(r))
		case r < 0x20 || r == 0x7f:
			// other C0 controls
		case r >= 0x80 && r < 0xa0:
			// C1 controls
		default:
			b = utf8.AppendRune(b, r)
		}
	}
	return string(b)
}
