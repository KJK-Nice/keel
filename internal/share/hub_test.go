package share

import (
	"io"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"

	"keel/internal/runtime"
	"keel/internal/session"
	"keel/internal/signal"
	"keel/internal/store"
)

type fakeBridge struct {
	inR  *io.PipeReader
	inW  *io.PipeWriter
	outR *io.PipeReader
	outW *io.PipeWriter
	once sync.Once
}

func newFakeBridge() *fakeBridge {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	return &fakeBridge{inR: inR, inW: inW, outR: outR, outW: outW}
}

func (f *fakeBridge) Read(p []byte) (int, error)  { return f.outR.Read(p) }
func (f *fakeBridge) Write(p []byte) (int, error) { return f.inW.Write(p) }
func (f *fakeBridge) Resize(int, int) error       { return nil }
func (f *fakeBridge) Close() error {
	f.once.Do(func() {
		_ = f.inW.Close()
		_ = f.outW.Close()
		_ = f.inR.Close()
		_ = f.outR.Close()
	})
	return nil
}

func TestShareDataChannel(t *testing.T) {
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	st, err := store.New(t.TempDir() + "/keel.json")
	if err != nil {
		t.Fatal(err)
	}
	fb := newFakeBridge()
	rt := runtime.New(st, func(store.Host, int, int) (session.Bridge, error) {
		return fb, nil
	})
	hosts, err := st.ListHosts()
	if err != nil {
		t.Fatal(err)
	}
	info, err := rt.Spawn(hosts[0].ID, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if rt.List()[0].Status == runtime.StatusReady {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	sig := httptest.NewServer(signal.New().Handler())
	t.Cleanup(sig.Close)
	hub := New(sig.URL, rt)
	t.Cleanup(func() { hub.Stop(info.ID) })
	join, err := hub.Share(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(join)
	if err != nil {
		t.Fatal(err)
	}
	room := u.Query().Get("room")
	wsURL := "ws" + strings.TrimPrefix(sig.URL, "http") + "/ws?role=join&room=" + room + "&name=bob"

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	got := make(chan string, 8)
	opened := make(chan struct{})
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		dc.OnOpen(func() { close(opened) })
		dc.OnMessage(func(m webrtc.DataChannelMessage) {
			if m.IsString {
				return
			}
			select {
			case got <- string(m.Data):
			default:
			}
		})
	})

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// pion calls OnICECandidate on its own goroutine while this one writes
	// the answer; gorilla allows one writer at a time.
	var writeMu sync.Mutex
	writeJSON := func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(v)
	}
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		init := c.ToJSON()
		msg := map[string]any{"type": "signal", "kind": "ice", "candidate": init.Candidate}
		if init.SDPMid != nil {
			msg["sdpMid"] = *init.SDPMid
		}
		_ = writeJSON(msg)
	})

	_ = conn.SetReadDeadline(time.Now().Add(8 * time.Second))
	for {
		var msg sigMsg
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatal(err)
		}
		if msg.Type != "signal" {
			continue
		}
		if msg.Kind == "offer" {
			if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: msg.SDP}); err != nil {
				t.Fatal(err)
			}
			ans, err := pc.CreateAnswer(nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := pc.SetLocalDescription(ans); err != nil {
				t.Fatal(err)
			}
			if err := writeJSON(sigMsg{Type: "signal", Kind: "answer", SDP: ans.SDP}); err != nil {
				t.Fatal(err)
			}
			break
		}
	}

	go func() {
		for {
			var msg sigMsg
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			if msg.Kind == "ice" && msg.Candidate != "" {
				var mid *string
				if msg.SDPMid != "" {
					m := msg.SDPMid
					mid = &m
				}
				_ = pc.AddICECandidate(webrtc.ICECandidateInit{Candidate: msg.Candidate, SDPMid: mid})
			}
		}
	}()

	select {
	case <-opened:
	case <-time.After(8 * time.Second):
		t.Fatal("datachannel did not open")
	}
	if _, err := fb.outW.Write([]byte("hello-webrtc")); err != nil {
		t.Fatal(err)
	}
	select {
	case s := <-got:
		if !strings.Contains(s, "hello-webrtc") {
			t.Fatalf("got %q", s)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("did not receive PTY bytes over datachannel")
	}
}

func TestFromEnvFallsBackToDefault(t *testing.T) {
	t.Setenv("KEEL_SIGNAL", "")
	old := DefaultSignal
	DefaultSignal = "https://signal.example"
	t.Cleanup(func() { DefaultSignal = old })
	h := FromEnv(nil)
	if h.URL() != "https://signal.example" {
		t.Fatalf("got %q", h.URL())
	}
}

func TestFromEnvOverridesDefault(t *testing.T) {
	t.Setenv("KEEL_SIGNAL", "https://override.example")
	old := DefaultSignal
	DefaultSignal = "https://signal.example"
	t.Cleanup(func() { DefaultSignal = old })
	h := FromEnv(nil)
	if h.URL() != "https://override.example" {
		t.Fatalf("got %q", h.URL())
	}
}
