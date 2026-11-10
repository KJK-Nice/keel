package joinpeer

import (
	"fmt"
	"strings"
	"testing"
)

// key is a made-up link key; it opens nothing.
const key = "00112233445566778899aabbccddeeff"

// The cases of cmd/keel/web/js/test/joinlink.test.mjs, one row for each assertion.
func TestParseLinkTheCasesOfJoinlinkTest(t *testing.T) {
	good, err := ParseLink(" https://sig.example/join?room=abc123#k=" + key + " ")
	if err != nil || good != (Link{Origin: "https://sig.example", Room: "abc123", Key: key}) {
		t.Fatalf("full link: %+v %v", good, err)
	}
	for _, tc := range []struct{ in, want string }{
		{"https://sig.example/join?room=abc123", "no key"},
		{"https://sig.example/join?room=abc123#k=short", "no key"},
		{"https://sig.example/join?room=abc123#k=" + strings.ToUpper(key), "no key"},
		{"", "paste a Share link"},
		{"https://sig.example/join#k=" + key, "link has no room"},
		{"0123456789abcdef", "paste the full Share link"},
	} {
		_, err := ParseLink(tc.in)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ParseLink(%q) = %v, want an error with %q", tc.in, err, tc.want)
		}
	}
}

func TestParseLinkOtherShapes(t *testing.T) {
	for in, want := range map[string]Link{
		"http://127.0.0.1:4000/join?room=r1#k=" + key:                   {"http://127.0.0.1:4000", "r1", key},
		"https://sig.example/join?x=1&room=r%201#a=b&k=" + key + "&c=d": {"https://sig.example", "r 1", key},
		"https://sig.example/base/join?room=r#k=" + key:                 {"https://sig.example", "r", key},
	} {
		if got, err := ParseLink(in); err != nil || got != want {
			t.Errorf("ParseLink(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, in := range []string{
		"ftp://sig.example/join?room=r#k=" + key, // not dialled
		"javascript:alert(1)",
		"https:///join?room=r#k=" + key,
		"https://sig.example/join?room=#k=" + key,
		"https://sig.example/join?room=r#k=" + key + "0",
		"https://sig.example/join?room=r#kk=" + key,
		"https://sig.example/join?room=r#k=" + key[:31] + "\n",
	} {
		if got, err := ParseLink(in); err == nil {
			t.Errorf("ParseLink(%q) = %+v, want an error", in, got)
		}
	}
}

func TestTheLinkAndItsErrorsNeverPrintTheKey(t *testing.T) {
	l, err := ParseLink("https://sig.example/join?room=r1#k=" + key)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []any{l, &l, struct{ L Link }{l}, []Link{l}, map[string]Link{"a": l}} {
		for _, f := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
			if out := fmt.Sprintf(f, v); strings.Contains(out, key) || strings.Contains(strings.ToLower(out), strings.ToLower(key)) {
				t.Errorf("%s of %T shows the key: %s", f, v, out)
			}
		}
	}
	for _, in := range []string{"https://sig.example/join#k=" + key, "https://sig.example/join?room=r#k=" + key + "x", "x://y?room=r#k=" + key} {
		if _, err := ParseLink(in); err != nil && strings.Contains(err.Error(), key) {
			t.Errorf("the error for %q shows the key: %v", in, err)
		}
	}
}

func TestSignalURLHasTheRoomAndTheNameAndNeverTheKey(t *testing.T) {
	for _, tc := range []struct{ origin, name, want string }{
		{"https://sig.example", "Ann", "wss://sig.example/ws?name=Ann&role=join&room=r+1"},
		{"http://127.0.0.1:4000", "", "ws://127.0.0.1:4000/ws?name=guest&role=join&room=r+1"},
	} {
		got := signalURL(tc.origin, "r 1", tc.name)
		if got != tc.want || strings.Contains(got, key) || strings.Contains(got, "#") {
			t.Errorf("signalURL = %q, want %q", got, tc.want)
		}
	}
}
