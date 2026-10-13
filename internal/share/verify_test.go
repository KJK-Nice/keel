package share

import "testing"

const sampleSDP = "v=0\r\no=- 1 2 IN IP4 127.0.0.1\r\n" +
	"a=fingerprint:sha-256 AB:CD:EF:01\r\n" +
	"m=application 9 UDP/DTLS/SCTP webrtc-datachannel\r\n" +
	"a=fingerprint:sha-256  ab:cd:ef:01\r\n" +
	"a=fingerprint:sha-512 99:88\r\n"

func TestSDPFingerprints(t *testing.T) {
	got := sdpFingerprints(sampleSDP)
	want := []string{"sha-256 ab:cd:ef:01", "sha-512 99:88"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("fingerprints = %q, want %q", got, want)
	}
	if fps := sdpFingerprints("v=0\r\n"); len(fps) != 0 {
		t.Fatalf("no fingerprint lines, got %q", fps)
	}
}

// Fixed vector shared with cmd/keel/web/js/test/sharemac.test.mjs, so the
// Go host and both JS joiners agree byte for byte.
const vectorSecret = "00112233445566778899aabbccddeeff"
const vectorHostMAC = "bfbfbdb60dc9081a69052a881a101013c4e0d64a153a6cecee59689822f0df5a"

func TestSDPMAC(t *testing.T) {
	mac := sdpMAC(vectorSecret, roleHost, sampleSDP)
	if mac != vectorHostMAC {
		t.Fatalf("mac %q, want the shared vector %q", mac, vectorHostMAC)
	}
	if !checkSDPMAC(vectorSecret, roleHost, sampleSDP, mac) {
		t.Fatal("own MAC rejected")
	}
	for name, bad := range map[string]func() bool{
		"wrong secret": func() bool { return checkSDPMAC("ff"+vectorSecret[2:], roleHost, sampleSDP, mac) },
		"wrong role":   func() bool { return checkSDPMAC(vectorSecret, roleJoin, sampleSDP, mac) },
		"swapped fingerprint": func() bool {
			return checkSDPMAC(vectorSecret, roleHost, "a=fingerprint:sha-256 00:00\r\n", mac)
		},
		"empty mac":    func() bool { return checkSDPMAC(vectorSecret, roleHost, sampleSDP, "") },
		"empty secret": func() bool { return checkSDPMAC("", roleHost, sampleSDP, sdpMAC("", roleHost, sampleSDP)) },
		"no fingerprint": func() bool {
			return checkSDPMAC(vectorSecret, roleHost, "v=0\r\n", sdpMAC(vectorSecret, roleHost, "v=0\r\n"))
		},
	} {
		if bad() {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestNewShareSecret(t *testing.T) {
	a, err := newShareSecret()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := newShareSecret()
	if len(a) != 32 || a == b {
		t.Fatalf("secrets %q %q", a, b)
	}
}
