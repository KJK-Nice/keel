package share

import (
	"net/url"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

// #157: the Allow prompt shows the joiner's name without control or bidi
// characters, and at most 32 runes of it.
func TestJoinRequestNameSanitized(t *testing.T) {
	r := newShareRig(t)
	link, err := r.hub.Share(r.id)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	const rlo = "\xe2\x80\xae" // U+202E
	name := rlo + "evil\x1b[2J\n" + strings.Repeat("x", 100)
	ws := "ws" + strings.TrimPrefix(r.sig.URL, "http") + "/ws?role=join&room=" + url.QueryEscape(u.Query().Get("room")) + "&name=" + url.QueryEscape(name)
	conn, _, err := websocket.DefaultDialer.Dial(ws, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	want := "evil[2J" + strings.Repeat("x", 25)
	if got := r.event("join-request").Message; got != want {
		t.Fatalf("join-request name = %q, want %q", got, want)
	}
}
