// Package randid makes random hex ids. It is a leaf: it imports no keel package.
package randid

import (
	"crypto/rand"
	"encoding/hex"
)

// Hex returns n random bytes as 2n lower-case hex digits. crypto/rand does
// not fail on supported platforms; if it ever does there is no safe id to
// fall back on, so it panics.
func Hex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("randid: " + err.Error())
	}
	return hex.EncodeToString(b)
}
