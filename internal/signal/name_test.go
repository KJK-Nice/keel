package signal

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"

	"slime/internal/wire"
)

// #157: the joiner's name is passed to the host capped.
func TestJoinerNameIsCapped(t *testing.T) {
	srv := httptest.NewServer(New().Handler())
	t.Cleanup(srv.Close)
	_, room := postRoom(t, srv.URL, "")
	ws := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?room=" + room.ID
	host, _, err := websocket.DefaultDialer.Dial(ws+"&role=host&key="+room.HostKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { host.Close() })
	join, _, err := websocket.DefaultDialer.Dial(ws+"&role=join&name="+url.QueryEscape(strings.Repeat("x", 8000)), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { join.Close() })
	_ = host.SetReadDeadline(time.Now().Add(2 * time.Second))
	var msg wire.Signal
	if err := host.ReadJSON(&msg); err != nil || msg.Type != wire.SignalPeer {
		t.Fatalf("host got %#v, %v", msg, err)
	}
	if n := utf8.RuneCountInString(msg.Name); n > 32 {
		t.Fatalf("host was sent a %d-rune name", n)
	}
}
