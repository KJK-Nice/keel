package joinpeer

import (
	"errors"

	"github.com/pion/webrtc/v4"

	"keel/internal/sharelink"
	"keel/internal/wire"
)

// The joiner's half of the end-to-end check of a share's WebRTC peer (the MAC itself is internal/sharelink's, shared
// with the host): the offer must carry a MAC the link's key vouches for before the peer is touched.

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
	if !sharelink.CheckMAC(key, wire.SignalRoleHost, o.SDP, o.MAC) {
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
	return answer{SDP: sdp, MAC: sharelink.MAC(key, wire.SignalRoleJoin, sdp)}, nil
}
