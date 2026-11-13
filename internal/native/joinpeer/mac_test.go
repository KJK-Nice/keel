package joinpeer

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/pion/webrtc/v4"

	"keel/internal/sharelink"
	"keel/internal/wire"
)

// key is a made-up link key; it opens nothing. The MAC itself is sharelink's and is pinned there.
const key = "00112233445566778899aabbccddeeff"

const sampleSDP = "v=0\r\no=- 1 2 IN IP4 127.0.0.1\r\n" +
	"a=fingerprint:sha-256 AB:CD:EF:01\r\n" +
	"m=application 9 UDP/DTLS/SCTP webrtc-datachannel\r\n" +
	"a=fingerprint:sha-256  ab:cd:ef:01\r\n" +
	"a=fingerprint:sha-512 99:88\r\n"

// hostMAC and joinMAC are what each side sends with its SDP.
func hostMAC(k, sdp string) string { return sharelink.MAC(k, wire.SignalRoleHost, sdp) }
func joinMAC(k, sdp string) string { return sharelink.MAC(k, wire.SignalRoleJoin, sdp) }

// fakePC is the JS test's fakePeer.
type fakePC struct {
	calls  []string
	answer string
	local  *webrtc.SessionDescription
}

func (f *fakePC) SetRemoteDescription(d webrtc.SessionDescription) error {
	f.calls = append(f.calls, "remote")
	return nil
}
func (f *fakePC) CreateAnswer(*webrtc.AnswerOptions) (webrtc.SessionDescription, error) {
	return webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: f.answer}, nil
}
func (f *fakePC) SetLocalDescription(d webrtc.SessionDescription) error {
	f.calls = append(f.calls, "local")
	f.local = &d
	return nil
}
func (f *fakePC) LocalDescription() *webrtc.SessionDescription { return f.local }
func (f *fakePC) AddICECandidate(c webrtc.ICECandidateInit) error {
	f.calls = append(f.calls, "ice "+c.Candidate)
	return nil
}

func TestAnswerOfferAnswersAVerifiedOfferWithTheJoinersMAC(t *testing.T) {
	answerSDP := "v=0\r\na=fingerprint:sha-256 11:22\r\n"
	pc := &fakePC{answer: answerSDP}
	got, err := answerOffer(pc, key, offer{sampleSDP, hostMAC(key, sampleSDP)}, nil)
	if err != nil || got != (answer{answerSDP, joinMAC(key, answerSDP)}) {
		t.Fatalf("answer %+v %v", got, err)
	}
	if pc.calls[0] != "remote" {
		t.Errorf("calls %v", pc.calls)
	}
}

func TestAnswerOfferRefusesAnOfferWithoutAMatchingMACAndNeverTouchesThePeer(t *testing.T) {
	for _, o := range []offer{
		{sampleSDP, ""},
		{sampleSDP, hostMAC("ffeeddccbbaa99887766554433221100", sampleSDP)},
		{sampleSDP, joinMAC(key, sampleSDP)},
	} {
		pc := &fakePC{answer: "x"}
		_, err := answerOffer(pc, key, o, nil)
		if !errors.Is(err, ErrUnverified) || !strings.Contains(err.Error(), "could not be verified") {
			t.Errorf("err %v", err)
		}
		if len(pc.calls) != 0 {
			t.Errorf("the peer was touched: %v", pc.calls)
		}
		if strings.Contains(err.Error(), key) {
			t.Error("the error shows the key")
		}
	}
}

func TestICEQueueHoldsCandidatesUntilTheOfferIsApplied(t *testing.T) {
	pc := &fakePC{answer: "v=0\r\na=fingerprint:sha-256 11:22\r\n"}
	q := &iceQueue{pc: pc}
	q.add(webrtc.ICECandidateInit{Candidate: "early"})
	if len(pc.calls) != 0 {
		t.Fatal("added before the offer")
	}
	if _, err := answerOffer(pc, key, offer{sampleSDP, hostMAC(key, sampleSDP)}, q); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(pc.calls, []string{"remote", "ice early", "local"}) {
		t.Fatalf("calls %v", pc.calls)
	}
	q.add(webrtc.ICECandidateInit{Candidate: "late"})
	if pc.calls[len(pc.calls)-1] != "ice late" {
		t.Errorf("calls %v", pc.calls)
	}
	// At most 64 early candidates are kept.
	q = &iceQueue{pc: pc}
	for i := 0; i < 100; i++ {
		q.add(webrtc.ICECandidateInit{Candidate: "c"})
	}
	if len(q.early) != maxEarlyCandidates {
		t.Errorf("kept %d early candidates", len(q.early))
	}
}
