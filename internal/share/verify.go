package share

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// End-to-end check of the WebRTC peer, independent of the signal server.
//
// The share link carries a random secret after "#", which browsers and the
// desktop joiner never send to the server. Each side sends, beside its SDP,
// HMAC-SHA256(secret, role + "\n" + its DTLS fingerprints) and checks the
// other side's. A signal server that swaps the SDP to sit in the middle
// would have to swap the fingerprints too, and can't produce the matching
// MAC without the secret. The same functions exist in
// cmd/keel/web/js/lib/sharemac.js (desktop joiner) and
// internal/signal/web/sharemac.js (browser join page).

const (
	roleHost = "host"
	roleJoin = "join"
)

func newShareSecret() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// sdpFingerprints returns the SDP's a=fingerprint values, normalized
// (lowercase, single spaces), sorted and de-duplicated.
func sdpFingerprints(sdp string) []string {
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(sdp, "\n") {
		line = strings.TrimSpace(line)
		v, ok := strings.CutPrefix(line, "a=fingerprint:")
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

// sdpMAC is the MAC a peer in role sends with sdp. Empty when the SDP has
// no fingerprint (nothing to vouch for).
func sdpMAC(secret, role, sdp string) string {
	fps := sdpFingerprints(sdp)
	if secret == "" || len(fps) == 0 {
		return ""
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(role + "\n" + strings.Join(fps, "\n")))
	return hex.EncodeToString(m.Sum(nil))
}

// checkSDPMAC reports whether mac vouches for sdp from a peer in role.
func checkSDPMAC(secret, role, sdp, mac string) bool {
	want := sdpMAC(secret, role, sdp)
	return want != "" && hmac.Equal([]byte(want), []byte(mac))
}
