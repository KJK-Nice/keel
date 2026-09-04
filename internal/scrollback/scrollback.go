// Package scrollback is the read-path seam for PTY history. Read redacts
// secrets by default. Live terminal replay must keep using the raw ring.
package scrollback

import (
	"regexp"
)

const Mask = "[redacted]"

var (
	pemRe      = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)
	ageRe      = regexp.MustCompile(`AGE-SECRET-KEY-[0-9A-Z]+`)
	awsKeyRe   = regexp.MustCompile(`AKIA[0-9A-Z]{16}`)
	bearerRe   = regexp.MustCompile(`(?i)(bearer\s+)\S+`)
	secretEqRe = regexp.MustCompile(`(?i)((?:password|passwd|secret)\s*[=:]\s*)\S+`)
)

func Read(raw []byte) []byte {
	s := string(raw)
	s = pemRe.ReplaceAllString(s, Mask)
	s = ageRe.ReplaceAllString(s, Mask)
	s = awsKeyRe.ReplaceAllString(s, Mask)
	s = bearerRe.ReplaceAllString(s, `${1}`+Mask)
	s = secretEqRe.ReplaceAllString(s, `${1}`+Mask)
	return []byte(s)
}
