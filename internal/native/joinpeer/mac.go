package joinpeer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"sort"
	"strings"

	"github.com/pion/webrtc/v4"
)

// The end-to-end check of a share's WebRTC peer, independent of the signal server: lib/sharemac.js, which mirrors
// internal/share/verify.go. Each side sends HMAC-SHA256(key, role + "\n" + its DTLS fingerprints) with its SDP and checks
// the other side's, so a signal server that swaps fingerprints to sit in the middle is detected. The JS writes SHA-256
// by hand because crypto.subtle needs a secure context; Go has crypto/sha256 and crypto/hmac.

// Fingerprints returns the SDP's a=fingerprint values, normalized (lower case, single spaces), sorted and
// de-duplicated.
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

// MAC is the MAC a peer in role ("host" or "join") sends with sdp; "" when there is no key or no fingerprint to vouch
// for.
func MAC(key, role, sdp string) string {
	fps := Fingerprints(sdp)
	if key == "" || len(fps) == 0 {
		return ""
	}
	return hmacHex(key, role+"\n"+strings.Join(fps, "\n"))
}

// CheckMAC reports whether mac vouches for sdp from a peer in role. The comparison takes the same time whatever
// the input.
func CheckMAC(key, role, sdp, mac string) bool {
	want := MAC(key, role, sdp)
	return want != "" && hmac.Equal([]byte(want), []byte(mac))
}

// iceAdder is what the ICE queue needs of a peer connection.
type iceAdder interface {
	AddICECandidate(webrtc.ICECandidateInit) error
}

// iceQueue holds the host's ICE candidates until the offer has been applied (adding one fails before there is a
// remote description); flush, which answerOffer calls, releases them (lib/sharemac.js createIceQueue).
type iceQueue struct {
	pc    iceAdder
	ready bool
	early []webrtc.ICECandidateInit
}

const maxEarlyCandidates = 64

func (q *iceQueue) add(c webrtc.ICECandidateInit) {
	if !q.ready {
		if len(q.early) < maxEarlyCandidates {
			q.early = append(q.early, c)
		}
		return
	}
	_ = q.pc.AddICECandidate(c) // a bad candidate is not fatal
}

func (q *iceQueue) flush() {
	q.ready = true
	early := q.early
	q.early = nil
	for _, c := range early {
		_ = q.pc.AddICECandidate(c)
	}
}

// answerer is what answerOffer needs of a peer connection (pion's *webrtc.PeerConnection).
type answerer interface {
	SetRemoteDescription(webrtc.SessionDescription) error
	CreateAnswer(*webrtc.AnswerOptions) (webrtc.SessionDescription, error)
	SetLocalDescription(webrtc.SessionDescription) error
	LocalDescription() *webrtc.SessionDescription
}

// ErrUnverified is the refusal of an offer the key does not vouch for. Its words are the app's.
var ErrUnverified = errors.New("host could not be verified: the link is wrong or the signal server tampered with the connection")

// offer is the host's offer with its MAC; answer is what the joiner sends back.
type offer struct{ SDP, MAC string }
type answer struct{ SDP, MAC string }

// answerOffer is the joiner's half of the exchange: it refuses an offer whose MAC does not match the key (a signal
// server in the middle) before it touches the peer, then answers and returns the answer to send, MAC included
// (lib/sharemac.js answerOffer).
func answerOffer(pc answerer, key string, o offer, ice *iceQueue) (answer, error) {
	if !CheckMAC(key, "host", o.SDP, o.MAC) {
		return answer{}, ErrUnverified
	}
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: o.SDP}); err != nil {
		return answer{}, err
	}
	if ice != nil {
		ice.flush()
	}
	a, err := pc.CreateAnswer(nil)
	if err != nil {
		return answer{}, err
	}
	if err := pc.SetLocalDescription(a); err != nil {
		return answer{}, err
	}
	sdp := a.SDP
	if ld := pc.LocalDescription(); ld != nil && ld.SDP != "" {
		sdp = ld.SDP
	}
	return answer{SDP: sdp, MAC: MAC(key, "join", sdp)}, nil
}

// hmacHex is HMAC-SHA256(key, message) as hex (lib/sharemac.js hmacSha256Hex); MAC is built on it.
func hmacHex(key, message string) string {
	m := hmac.New(sha256.New, []byte(key))
	io.WriteString(m, message)
	return hex.EncodeToString(m.Sum(nil))
}
