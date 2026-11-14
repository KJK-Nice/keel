package keelclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// injectServer is a /ws/session that says it is ready with a role and answers an approval request as told.
type injectServer struct {
	mu     sync.Mutex
	frames []map[string]any // every text frame received, in order
	answer func(ws *websocket.Conn, m map[string]any)
	// onInject, when set, answers an inject frame (keel serve's agent-type for a proposal).
	onInject func(ws *websocket.Conn, m map[string]any)
	// onOther, when set, sees every text frame (the suggestion frames answer from it).
	onOther func(ws *websocket.Conn, m map[string]any)
}

func (s *injectServer) all() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.frames...)
}

func (s *injectServer) types() (out []string) {
	for _, m := range s.all() {
		out = append(out, m["type"].(string))
	}
	return out
}

// dialInject starts the server with status and role in its ready frame and returns a connection that is being read.
func dialInject(t *testing.T, status, role string, answer func(ws *websocket.Conn, m map[string]any)) (*Conn, *injectServer) {
	t.Helper()
	s := &injectServer{answer: answer}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		ready, _ := json.Marshal(map[string]any{"type": "ready", "id": "s1", "status": status,
			"you": map[string]any{"id": 2, "role": role}, "driverId": 2, "ownerId": 2, "attached": []any{map[string]any{"id": 2, "role": role}}})
		ws.WriteMessage(websocket.TextMessage, ready)
		for {
			typ, d, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if typ != websocket.TextMessage {
				continue
			}
			var m map[string]any
			json.Unmarshal(d, &m)
			s.mu.Lock()
			s.frames = append(s.frames, m)
			s.mu.Unlock()
			if m["type"] == "inject-challenge" && s.answer != nil {
				s.answer(ws, m)
			}
			if m["type"] == "inject" && s.onInject != nil {
				s.onInject(ws, m)
			}
			if s.onOther != nil {
				s.onOther(ws, m)
			}
		}
	}))
	t.Cleanup(srv.Close)
	c, err := dialWS("ws"+strings.TrimPrefix(srv.URL, "http"), tok)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	go io.Copy(io.Discard, c)
	deadline := time.Now().Add(2 * time.Second)
	for c.Snapshot().Status == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	return c, s
}

func nonceAnswer(ws *websocket.Conn, _ map[string]any) {
	ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"inject-challenge","nonce":"n-1"}`))
}

func TestApprovedInjectSendsTheChallengeThenTheSameBytesWithTheNonce(t *testing.T) {
	c, s := dialInject(t, "ready", "driver", nonceAnswer)
	cmd := "\x15echo hi\n"
	if err := c.ApprovedInject(context.Background(), cmd, nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(s.types()) < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := s.types(); len(got) != 2 || got[0] != "inject-challenge" || got[1] != "inject" {
		t.Fatalf("frames %v", got)
	}
	for _, m := range s.all() {
		if m["command"] != cmd {
			t.Fatalf("%v carries %q, not the approved bytes", m["type"], m["command"])
		}
	}
	if s.all()[1]["nonce"] != "n-1" {
		t.Fatalf("inject nonce %v", s.all()[1]["nonce"])
	}
	for _, m := range s.all() {
		if _, ok := m["direct"]; ok || m["type"] == "type-check" {
			t.Fatalf("a direct or type-check frame: %v", m)
		}
	}
}

func TestApprovedInjectWithNoNonceSendsNoInjectAndDoesNotRetry(t *testing.T) {
	c, s := dialInject(t, "ready", "driver", nil) // keel serve says nothing
	c.wait = 80 * time.Millisecond
	err := c.ApprovedInject(context.Background(), "ls\n", nil)
	if !errors.Is(err, ErrNoTicket) || err.Error() != "Session is not connected" {
		t.Fatalf("got %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	if got := s.types(); len(got) != 1 || got[0] != "inject-challenge" {
		t.Fatalf("frames %v: an inject went out, or the request was repeated", got)
	}
}

func TestApprovedInjectRefusedChallengeTypesNothing(t *testing.T) {
	// keel serve answers a refused challenge with an empty nonce, or an error frame: both mean nothing is typed.
	for name, answer := range map[string]func(*websocket.Conn, map[string]any){
		"empty nonce": func(ws *websocket.Conn, _ map[string]any) {
			ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"inject-challenge","nonce":""}`))
		},
		"error frame": func(ws *websocket.Conn, _ map[string]any) {
			ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","message":"nope"}`))
		},
	} {
		t.Run(name, func(t *testing.T) {
			c, s := dialInject(t, "ready", "driver", answer)
			c.wait = 80 * time.Millisecond
			if err := c.ApprovedInject(context.Background(), "ls\n", nil); !errors.Is(err, ErrNoTicket) {
				t.Fatalf("got %v", err)
			}
			time.Sleep(100 * time.Millisecond)
			if got := s.types(); len(got) != 1 {
				t.Fatalf("frames %v", got)
			}
		})
	}
}

func TestApprovedInjectFollowerAndNotReadySendNoFrameAtAll(t *testing.T) {
	for _, c := range []struct {
		status, role string
		want         error
	}{{"ready", "follower", ErrNotDriver}, {"ready", "owner", ErrNotDriver}, {"connecting", "driver", ErrNotReady}, {"error", "driver", ErrNotReady}} {
		conn, s := dialInject(t, c.status, c.role, nonceAnswer)
		if err := conn.ApprovedInject(context.Background(), "ls\n", nil); !errors.Is(err, c.want) {
			t.Fatalf("%s/%s: got %v, want %v", c.status, c.role, err, c.want)
		}
		time.Sleep(60 * time.Millisecond)
		if got := s.types(); len(got) != 0 {
			t.Fatalf("%s/%s sent %v", c.status, c.role, got)
		}
	}
	if ErrNotDriver.Error() != "You are following — take or request control to type" {
		t.Fatalf("the error words changed: %q", ErrNotDriver)
	}
}

func TestApprovedInjectOneAtATimeNeverQueues(t *testing.T) {
	release := make(chan struct{})
	c, s := dialInject(t, "ready", "driver", func(ws *websocket.Conn, m map[string]any) {
		<-release
		nonceAnswer(ws, m)
	})
	first := make(chan error, 1)
	go func() { first <- c.ApprovedInject(context.Background(), "one\n", nil) }()
	deadline := time.Now().Add(2 * time.Second)
	for len(s.types()) < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if err := c.ApprovedInject(context.Background(), "two\n", nil); !errors.Is(err, ErrInjectBusy) {
		t.Fatalf("second call: %v", err)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	for _, m := range s.all() {
		if m["command"] == "two\n" {
			t.Fatal("the second call reached keel serve")
		}
	}
	// And the next one after the first is done works again.
	if err := c.ApprovedInject(context.Background(), "three\n", nil); err != nil {
		t.Fatal(err)
	}
}

func TestApprovedInjectStopsWhenItsContextEnds(t *testing.T) {
	c, s := dialInject(t, "ready", "driver", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.ApprovedInject(ctx, "ls\n", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	for _, ty := range s.types() {
		if ty == "inject" {
			t.Fatal("an inject went out")
		}
	}
}
