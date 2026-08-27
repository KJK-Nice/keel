package signal

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestRoomRelay(t *testing.T) {
	srv := httptest.NewServer(New().Handler())
	t.Cleanup(srv.Close)

	res, err := http.Post(srv.URL+"/rooms", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("rooms %d", res.StatusCode)
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil || body.ID == "" {
		t.Fatalf("room id: %v %#v", err, body)
	}

	ws := "ws" + strings.TrimPrefix(srv.URL, "http")
	host, _, err := websocket.DefaultDialer.Dial(ws+"/ws?role=host&room="+body.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	join, _, err := websocket.DefaultDialer.Dial(ws+"/ws?role=join&room="+body.ID+"&name=bob", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer join.Close()

	_ = host.SetReadDeadline(time.Now().Add(2 * time.Second))
	var peer map[string]any
	if err := host.ReadJSON(&peer); err != nil {
		t.Fatal(err)
	}
	if peer["type"] != "peer" {
		t.Fatalf("host first = %#v", peer)
	}
	peerID, _ := peer["peerId"].(string)
	if err := host.WriteJSON(map[string]any{"type": "signal", "peerId": peerID, "kind": "offer", "sdp": "v=0"}); err != nil {
		t.Fatal(err)
	}
	_ = join.SetReadDeadline(time.Now().Add(2 * time.Second))
	var toJoin map[string]any
	if err := join.ReadJSON(&toJoin); err != nil {
		t.Fatal(err)
	}
	if toJoin["kind"] != "offer" || toJoin["sdp"] != "v=0" {
		t.Fatalf("join got %#v", toJoin)
	}
}

func TestJoinPage(t *testing.T) {
	srv := httptest.NewServer(New().Handler())
	t.Cleanup(srv.Close)
	res, err := http.Get(srv.URL + "/join?room=x")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
}
