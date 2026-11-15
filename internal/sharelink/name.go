package sharelink

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// SanitizeName is how a joiner's self-chosen name is shown: no control or
// bidi characters (they could forge or reorder the Allow prompt), at most
// 32 runes, "guest" when nothing is left.
func SanitizeName(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" {
		return "guest"
	}
	if utf8.RuneCountInString(name) > 32 {
		runes := []rune(name)
		name = string(runes[:32])
	}
	return name
}
