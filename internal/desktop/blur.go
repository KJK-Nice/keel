package desktop

import (
	"strconv"
	"strings"
)

// Settings · Appearance · Window blur (macOS): the page posts
// "keel:blur:<radius>" on the same channel as its theme (appearance.go),
// and the window blurs what is behind it by that radius. 0 is no blur: the
// desktop shows through clear, at the page's opacity.
const blurPrefix = "keel:blur:"

// maxBlurRadius bounds the radius the page may ask for (its slider's max).
const maxBlurRadius = 64

// parseBlur reads a page message. ok is false for anything that is not
// exactly a blur message with a whole radius in 0..maxBlurRadius.
func parseBlur(msg string) (radius int, ok bool) {
	s, found := strings.CutPrefix(msg, blurPrefix)
	if !found || s == "" || len(s) > 2 {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > maxBlurRadius || strconv.Itoa(n) != s {
		return 0, false
	}
	return n, true
}
