package joinpeer

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/pion/webrtc/v4"
)

// The vectors of cmd/keel/web/js/test/sharemac.test.mjs and internal/share/verify_test.go: the same inputs, the same
// outputs, byte for byte.
const sampleSDP = "v=0\r\no=- 1 2 IN IP4 127.0.0.1\r\n" +
	"a=fingerprint:sha-256 AB:CD:EF:01\r\n" +
	"m=application 9 UDP/DTLS/SCTP webrtc-datachannel\r\n" +
	"a=fingerprint:sha-256  ab:cd:ef:01\r\n" +
	"a=fingerprint:sha-512 99:88\r\n"

const hostVector = "bfbfbdb60dc9081a69052a881a101013c4e0d64a153a6cecee59689822f0df5a"

func TestFingerprintsNormalizeSortAndDeduplicate(t *testing.T) {
	if got := Fingerprints(sampleSDP); !slices.Equal(got, []string{"sha-256 ab:cd:ef:01", "sha-512 99:88"}) {
		t.Errorf("fingerprints %q", got)
	}
	if got := Fingerprints("v=0\r\n"); len(got) != 0 {
		t.Errorf("fingerprints %q", got)
	}
}

func TestMACMatchesTheGoHostAndTheJSVector(t *testing.T) {
	if got := MAC(key, "host", sampleSDP); got != hostVector {
		t.Fatalf("MAC %q, want the shared vector %q", got, hostVector)
	}
}

// HMAC-SHA256 as node:crypto computes it (the JS test's hmacSha256Hex rows, 3 keys by 3 messages, computed by node):
// MAC is HMAC over "role\nfingerprints", so the same primitive is checked through the key and message the JS used.
func TestHMACRowsOfTheJSTest(t *testing.T) {
	long := strings.Repeat("x", 100)
	rows := []struct{ key, msg, want string }{
		{"k", "", "8bb990c40a7d61cb97597a942125025be50ac8beb74436e3735b98893a7f6620"},
		{"k", "host\nsha-256 ab:cd", "fd87cb07ac950469c8e9e1e7c71c38e9c48a07f682357799190c777b88b0fed3"},
		{"k", strings.Repeat("ü", 70), "7e3fc9eec0d56c451f9905e9039aac001b8041e322d57ac5c88ce3f90ece500f"},
		{key, "", "49b8bfbda4ad9a46f3e2ad6edb8f75b356a3724a109ed6d7d6670d20978d5113"},
		{key, "host\nsha-256 ab:cd", "45d472dd25a7c4b1959501ef573251a3839366cea95d71035a84d889a57cfcc0"},
		{key, strings.Repeat("ü", 70), "2590033b3ecc07b3481a46a469440027b3168553e9f385ec58e81374b3443bfa"},
		{long, "", "f27e9d033f8afb8db9abbb1cee40470512ffbe9e76153cc2481ec7e812869b6f"},
		{long, "host\nsha-256 ab:cd", "fd1d3a2a1fc8f0960e163cbadcde7cb67d7290d98447050f660927899e5aaf74"},
		{long, strings.Repeat("ü", 70), "aa14918ef5d218e51d322089dde4b9172a14f2a5795d903f977d4e6d07845ce7"},
	}
	for _, r := range rows {
		if got := hmacHex(r.key, r.msg); got != r.want {
			t.Errorf("hmac(%d bytes key, %d bytes) = %s, want %s", len(r.key), len(r.msg), got, r.want)
		}
	}
}

func TestCheckMACRejectsTampering(t *testing.T) {
	mac := MAC(key, "join", sampleSDP)
	for name, tc := range map[string]struct {
		got  bool
		want bool
	}{
		"own":            {CheckMAC(key, "join", sampleSDP, mac), true},
		"wrong role":     {CheckMAC(key, "host", sampleSDP, mac), false},
		"wrong secret":   {CheckMAC("ff"+key[2:], "join", sampleSDP, mac), false},
		"swapped print":  {CheckMAC(key, "join", "a=fingerprint:sha-256 00:00\r\n", mac), false},
		"missing mac":    {CheckMAC(key, "join", sampleSDP, ""), false},
		"no secret":      {CheckMAC("", "join", sampleSDP, MAC("", "join", sampleSDP)), false},
		"no fingerprint": {CheckMAC(key, "join", "v=0\r\n", MAC(key, "join", "v=0\r\n")), false},
		"short mac":      {CheckMAC(key, "join", sampleSDP, mac[:len(mac)-1]), false},
		"long mac":       {CheckMAC(key, "join", sampleSDP, mac+"0"), false},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: %v, want %v", name, tc.got, tc.want)
		}
	}
}

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
	got, err := answerOffer(pc, key, offer{sampleSDP, MAC(key, "host", sampleSDP)}, nil)
	if err != nil || got != (answer{answerSDP, MAC(key, "join", answerSDP)}) {
		t.Fatalf("answer %+v %v", got, err)
	}
	if pc.calls[0] != "remote" {
		t.Errorf("calls %v", pc.calls)
	}
}

func TestAnswerOfferRefusesAnOfferWithoutAMatchingMACAndNeverTouchesThePeer(t *testing.T) {
	for _, o := range []offer{
		{sampleSDP, ""},
		{sampleSDP, MAC("ffeeddccbbaa99887766554433221100", "host", sampleSDP)},
		{sampleSDP, MAC(key, "join", sampleSDP)},
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
	if _, err := answerOffer(pc, key, offer{sampleSDP, MAC(key, "host", sampleSDP)}, q); err != nil {
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
