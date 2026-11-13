package cmdrule

import (
	"regexp"
	"strconv"
)

// The families of a redaction placeholder: [secret:1], [ip:2], [host:1].
const (
	PlaceholderSecret = "secret"
	PlaceholderIP     = "ip"
	PlaceholderHost   = "host"
)

// Placeholder is the text that stands for the n-th value of a family.
func Placeholder(family string, n int) string {
	return "[" + family + ":" + strconv.Itoa(n) + "]"
}

// placeholderRe is the one syntax: redact writes it with Placeholder, and the
// service and the window read it with HasPlaceholder.
var placeholderRe = regexp.MustCompile(`\[(secret|ip|host):\d+\]`)

// HasPlaceholder reports whether text holds a redaction placeholder such
// as [secret:1]: a shell glob, never the value. Nothing that holds one is
// typed (§6 rule 9): the one gate refuses it, and keel serve refuses a
// notebook fence that holds one.
func HasPlaceholder(s string) bool { return placeholderRe.MatchString(s) }
