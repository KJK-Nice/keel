package keelclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// shareKey is a made-up key for these tests; it opens nothing.
const shareKey = "00112233445566778899aabbccddeeff"

func TestShareAdmitAndStopUseTheShareAPI(t *testing.T) {
	r := newRec(t)
	ctx := context.Background()
	r.reply = `{"url":"http://sig.example/join?room=r1#k=` + shareKey + `"}`
	link, err := r.c.Share(ctx, "s 1")
	if err != nil || !strings.HasSuffix(link, "#k="+shareKey) {
		t.Fatalf("share %q %v", link, err)
	}
	if got, body := r.last(); got != "POST /api/share" || body != `{"sessionId":"s 1"}` {
		t.Fatalf("%s %s", got, body)
	}
	r.reply = ""
	for _, allow := range []bool{true, false} {
		if err := r.c.AdmitShare(ctx, "s1", "p1", allow); err != nil {
			t.Fatal(err)
		}
		if got, body := r.last(); got != "POST /api/share/s1/admit" || body != fmt.Sprintf(`{"allow":%v,"peerId":"p1"}`, allow) {
			t.Fatalf("%s %s", got, body)
		}
	}
	if err := r.c.StopShare(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.last(); got != "DELETE /api/share/s1" {
		t.Fatalf("got %s", got)
	}
}

func TestShareWithoutASignalServerShowsTheServersWordsAndNoLinkInErrors(t *testing.T) {
	r := newRec(t)
	r.status, r.reply = 503, `{"error":"set a signal server in Settings · Sharing to share"}`
	_, err := r.c.Share(context.Background(), "s1")
	var se *StatusError
	if !errors.As(err, &se) || se.Msg != "set a signal server in Settings · Sharing to share" || se.Code != 503 {
		t.Fatalf("err %v", err)
	}
	// A refusal that echoes a link must not carry it in Error().
	r.status, r.reply = 409, `{"error":"x"}`
	err = r.c.AdmitShare(context.Background(), "s1", "p1", true)
	if err == nil || strings.Contains(err.Error(), shareKey) || strings.Contains(err.Error(), "p1") {
		t.Fatalf("err %v", err)
	}
}

func TestJoinAndControlFramesBecomeEventsAndFollowersSendNothing(t *testing.T) {
	s := newScript(t)
	c, err := s.c.Spawn("h1", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	go io.Copy(io.Discard, c)
	s.out <- `{"type":"ready","id":"s1","status":"ready","driverId":1,"ownerId":1,"attached":[{"id":1,"name":"me","kind":"web","role":"driver","attachedAt":"2026-01-02T03:04:05Z"},{"id":2,"name":"b","kind":"webrtc","role":"follower"}],"you":{"id":1,"name":"me","kind":"web","role":"driver"}}`
	waitFor(t, func() bool { return c.Snapshot().Role == "driver" })
	if a := c.Snapshot().Attached; a[0].AttachedAt != "2026-01-02T03:04:05Z" || a[1].Kind != "webrtc" {
		t.Fatalf("attached %+v", a)
	}
	s.out <- `{"type":"join-request","id":"s1","peerId":"p9","message":"Ann"}`
	s.out <- `{"type":"join-resolved","id":"s1","peerId":"p9","message":"allowed"}`
	s.out <- `{"type":"join-failed","id":"s1","peerId":"p8","message":"Bob was not connected: failed verification"}`
	s.out <- `{"type":"control","message":"b asks for control","fromId":2,"driverId":1,"ownerId":1,"attached":[],"you":{"id":1,"role":"driver"}}`
	s.out <- `{"type":"control","message":"b has control","targetId":2,"driverId":2,"ownerId":1,"attached":[],"you":{"id":1,"role":"owner"}}`
	waitFor(t, func() bool { return c.Snapshot().Role == "owner" })
	got := c.Events()
	want := []Event{
		{Kind: EventJoinRequest, PeerID: "p9", Message: "Ann"},
		{Kind: EventJoinResolved, PeerID: "p9", Message: "allowed"},
		{Kind: EventJoinFailed, PeerID: "p8", Message: "Bob was not connected: failed verification"},
		{Kind: EventControlRequest, FromID: 2, Message: "b asks for control"},
		{Kind: EventControlNote, Message: "b has control"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("events %v, want %v", got, want)
	}
	if again := c.Events(); len(again) != 0 {
		t.Fatalf("events handed over twice: %v", again)
	}
	// Not driving now (owner): typing sends nothing and is counted.
	n, err := c.Write([]byte("ls\r"))
	if err != nil || n != 3 {
		t.Fatalf("write %d %v", n, err)
	}
	select {
	case f := <-s.got:
		t.Fatalf("a follower's keystrokes reached the socket: %q", f.data)
	case <-time.After(150 * time.Millisecond):
	}
	if b := c.Snapshot().Blocked; b != 1 {
		t.Fatalf("blocked %d", b)
	}
	// Driving again: it sends.
	s.out <- `{"type":"presence","driverId":1,"attached":[],"you":{"id":1,"role":"driver"}}`
	waitFor(t, func() bool { return c.Snapshot().Role == "driver" })
	if _, err := c.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	select {
	case f := <-s.got:
		if f.typ != websocket.BinaryMessage || string(f.data) != "x" {
			t.Fatalf("frame %v %q", f.typ, f.data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a driver's keystroke did not arrive")
	}
}
