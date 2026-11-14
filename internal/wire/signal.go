package wire

// Signal is a message on keel-signal's socket: the envelope the signal
// server relays between a share's host and its joiners. The server reads
// Type and PeerID and passes the rest on; it never sees the link's secret.
type Signal struct {
	Type      string `json:"type"`
	PeerID    string `json:"peerId,omitempty"`
	Name      string `json:"name,omitempty"`
	Kind      string `json:"kind,omitempty"`
	SDP       string `json:"sdp,omitempty"`
	Candidate string `json:"candidate,omitempty"`
	SDPMid    string `json:"sdpMid,omitempty"`
	SDPLine   *int   `json:"sdpMLineIndex,omitempty"`
	// Mac vouches for the SDP's DTLS fingerprints with the secret in the
	// join link's #fragment.
	Mac string `json:"mac,omitempty"`
}

// Signal.Type.
const (
	SignalPeer    = "peer"   // server → host: a joiner arrived (PeerID, Name)
	SignalLeave   = "leave"  // server → host: that joiner left; server → joiner: the host left
	SignalMessage = "signal" // relayed between the host and one joiner; Kind says what it is
)

// Signal.Kind of a SignalMessage.
const (
	SignalPending = "pending" // host → joiner: the host was asked to let you in
	SignalDenied  = "denied"  // host → joiner: not let in
	SignalOffer   = "offer"   // host → joiner: SDP, Mac
	SignalAnswer  = "answer"  // joiner → host: SDP, Mac
	SignalICE     = "ice"     // either way: Candidate, SDPMid, SDPLine
)

// The role= parameter of the signal socket's URL.
const (
	SignalRoleHost = "host"
	SignalRoleJoin = "join"
)
