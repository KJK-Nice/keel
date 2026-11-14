package joinpeer

import (
	"strings"
	"testing"
)

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
