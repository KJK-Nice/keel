// Package sharelink is what both halves of a share agree on: the join link
// (built by the host, read by the joiner), the end-to-end MAC that vouches
// for each side's WebRTC fingerprints, the signal server's socket URL and
// the STUN server. keel serve's share hub and the window's joinpeer both
// import it, so the two cannot drift. It is a leaf: it imports no keel
// package but wire (leaf_test.go checks).
//
// The MAC is the check of a peer that does not depend on the signal
// server. The link carries a random key after "#", which neither a browser
// nor the desktop joiner sends to the server. Each side sends, beside its
// SDP, HMAC-SHA256(key, role + "\n" + its DTLS fingerprints) and checks the
// other side's. A signal server that swaps the SDP to sit in the middle
// would have to swap the fingerprints too, and can't produce the matching
// MAC without the key.
package sharelink

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"sort"
	"strings"
)

// NewKey makes a share's secret: 16 random bytes as 32 lower-case hex
// digits, the shape ParseLink accepts.
func NewKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Fingerprints returns the SDP's a=fingerprint values, normalized (lower
// case, single spaces), sorted and de-duplicated.
func Fingerprints(sdp string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, line := range strings.Split(sdp, "\n") {
		v, ok := strings.CutPrefix(strings.TrimSpace(line), "a=fingerprint:")
		if !ok {
			continue
		}
		v = strings.ToLower(strings.Join(strings.Fields(v), " "))
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// MAC is the MAC a peer in role (wire.SignalRoleHost or SignalRoleJoin)
// sends with sdp; "" when there is no key or no fingerprint to vouch for.
func MAC(key, role, sdp string) string {
	fps := Fingerprints(sdp)
	if key == "" || len(fps) == 0 {
		return ""
	}
	return hmacHex(key, role+"\n"+strings.Join(fps, "\n"))
}

// CheckMAC reports whether mac vouches for sdp from a peer in role. The
// comparison takes the same time whatever the input.
func CheckMAC(key, role, sdp, mac string) bool {
	want := MAC(key, role, sdp)
	return want != "" && hmac.Equal([]byte(want), []byte(mac))
}

// hmacHex is HMAC-SHA256(key, message) as hex.
func hmacHex(key, message string) string {
	m := hmac.New(sha256.New, []byte(key))
	io.WriteString(m, message)
	return hex.EncodeToString(m.Sum(nil))
}
