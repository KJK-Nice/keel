package sessionsock

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"keel/internal/runtime"
	"keel/internal/session"
	"keel/internal/store"
	"keel/internal/wire"
)

// fakeBridge is a PTY: what is written to the session comes out of inR.
type fakeBridge struct {
	inR, outR *io.PipeReader
	inW, outW *io.PipeWriter
	once      sync.Once
}

func newFakeBridge() *fakeBridge {
	f := &fakeBridge{}
	f.inR, f.inW = io.Pipe()
	f.outR, f.outW = io.Pipe()
	return f
}

func (f *fakeBridge) Read(p []byte) (int, error)  { return f.outR.Read(p) }
func (f *fakeBridge) Write(p []byte) (int, error) { return f.inW.Write(p) }
func (f *fakeBridge) Resize(int, int) error       { return nil }
func (f *fakeBridge) Close() error {
	f.once.Do(func() { f.inW.Close(); f.outW.Close(); f.inR.Close(); f.outR.Close() })
	return nil
}

// written collects every byte that reaches the PTY.
type written struct {
	mu  sync.Mutex
	buf []byte
}

func (w *written) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.buf)
}

func readySession(t *testing.T) (*runtime.Session, *written) {
	t.Helper()
	t.Setenv("KEEL_KEYRING", "off")
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	st, err := store.New(t.TempDir()+"/keel.json", store.TestVault())
	if err != nil {
		t.Fatal(err)
	}
	fb := newFakeBridge()
	rt := runtime.New(st, func(store.Host, int, int) (session.Bridge, error) { return fb, nil })
	t.Cleanup(rt.KillAll)
	hosts, err := st.ListHosts()
	if err != nil {
		t.Fatal(err)
	}
	info, err := rt.Spawn(hosts[0].ID, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := rt.Get(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(2 * time.Second); sess.Info().Status != runtime.StatusReady; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the session never became ready")
		}
	}
	w := &written{}
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := fb.inR.Read(buf)
			w.mu.Lock()
			w.buf = append(w.buf, buf[:n]...)
			w.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	return sess, w
}

// recorder is a Transport that keeps the text frames it was sent.
type recorder struct {
	mu     sync.Mutex
	frames []map[string]any
}

func (r *recorder) SendJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	r.mu.Lock()
	r.frames = append(r.frames, m)
	r.mu.Unlock()
	return nil
}

func (r *recorder) SendBytes([]byte) error { return nil }

func (r *recorder) since(n int) []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]map[string]any(nil), r.frames[n:]...)
}

// answers are the frames sent after the first n that answer the client,
// leaving out the session's own news (presence, control, status), which
// arrive when they arrive.
func (r *recorder) answers(n int) (out []map[string]any) {
	for _, f := range r.since(n) {
		switch f["type"] {
		case wire.TypePresence, wire.TypeControl, wire.TypeStatus:
		default:
			out = append(out, f)
		}
	}
	return out
}

func (r *recorder) count() int { return len(r.since(0)) }

// typist counts what it is asked. With real, an inject consumes the
// session's ticket, as keel serve's does.
type typist struct {
	c                        *Conn
	real                     bool
	injects, detects, sugges int
}

func (t *typist) Inject(ctl wire.Control) error {
	t.injects++
	if !t.real {
		return nil
	}
	return t.c.Session().InjectTicket(t.c.ClientID(), ctl.Command, ctl.Nonce)
}
func (t *typist) Detect(int, string)  { t.detects++ }
func (t *typist) Suggest(int, string) { t.sugges++ }

func frame(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// typingFrames is every frame by which a client can ask keel serve to type
// for it or ask the agent: the inject family and the agent family.
func typingFrames(cmd, nonce string) map[string]wire.Control {
	return map[string]wire.Control{
		"inject-challenge":       {Type: wire.TypeInjectChallenge, Command: cmd},
		"inject, no nonce":       {Type: wire.TypeInject, Command: cmd},
		"inject, a nonce":        {Type: wire.TypeInject, Command: cmd, Nonce: nonce},
		"inject, notebook":       {Type: wire.TypeInject, Command: cmd, Nonce: nonce, Notebook: true},
		"inject, direct snippet": {Type: wire.TypeInject, Command: cmd, Nonce: nonce, Direct: &wire.DirectType{Ref: "d1", Kind: wire.DirectSnippet, SnippetID: "s1"}},
		"inject, direct agent":   {Type: wire.TypeInject, Command: cmd, Nonce: nonce, Direct: &wire.DirectType{Ref: "d2", Kind: wire.DirectAgent}, Agent: &wire.AgentType{ConversationID: "c1", ProposalID: "p1"}},
		"inject, direct note":    {Type: wire.TypeInject, Command: cmd, Nonce: nonce, Direct: &wire.DirectType{Ref: "d3", Kind: wire.DirectNotebook, NotebookID: "n1"}},
		"inject, agent":          {Type: wire.TypeInject, Command: cmd, Nonce: nonce, Agent: &wire.AgentType{ConversationID: "c1", ProposalID: "p1", Shift: true}},
		"inject, group run":      {Type: wire.TypeInject, Command: cmd, Nonce: nonce, Agent: &wire.AgentType{GroupRunID: "g1", ProposalID: "p1"}},
		"inject, suggestion":     {Type: wire.TypeInject, Command: cmd, Nonce: nonce, Suggestion: &wire.AgentInsert{SuggestionID: "sg1", Shift: true}},
		"agent-detect":           {Type: wire.TypeAgentDetect, Seq: 1, Line: "show the files"},
		"agent-suggest":          {Type: wire.TypeAgentSuggest, Seq: 1, Line: "show the files"},
	}
}

// A joined peer can never make keel serve type for it or ask the agent:
// following or driving, every inject-family and agent-family frame is
// refused before a typist sees it, no nonce is issued, no answer frame of
// those families goes back, and nothing reaches the PTY. Its keystrokes
// reach it only while it drives, like anyone's.
func TestAJoinedPeerCannotMakeKeelTypeOrAskTheAgent(t *testing.T) {
	sess, pty := readySession(t)
	ownerT := &typist{real: true}
	ownerRec := &recorder{}
	owner := Attach(sess, ownerRec, Options{Name: "me", Kind: wire.KindWindow, Caps: Local, Typist: func(c *Conn) Typist { ownerT.c = c; return ownerT }})
	defer owner.Detach()
	go owner.Forward()

	// Even handed a typist, a joined peer's is never made or called.
	peerT := &typist{real: true}
	made := false
	rec := &recorder{}
	peer := Attach(sess, rec, Options{Name: "bob", Kind: wire.KindJoined, Caps: Joined, Typist: func(c *Conn) Typist { made = true; peerT.c = c; return peerT }})
	defer peer.Detach()
	go peer.Forward()
	if made || peer.typist != nil {
		t.Fatal("a joined peer was given a typist")
	}

	// A nonce the session really issued, for the same bytes, to the owner:
	// it is bound to the owner, so it is worth nothing to the peer.
	const cmd = "rm -rf /tmp/keel-joined\n"
	ownerNonce, err := sess.IssueInjectTicket(owner.ClientID(), cmd)
	if err != nil || ownerNonce == "" {
		t.Fatalf("the owner's nonce: %q, %v", ownerNonce, err)
	}

	refused := func(role string) {
		t.Helper()
		if got := sess.Role(peer.ClientID()); string(got) != role {
			t.Fatalf("the peer is %s, want %s", got, role)
		}
		for name, ctl := range typingFrames(cmd, ownerNonce) {
			n := rec.count()
			peer.Control(frame(t, ctl))
			got := rec.answers(n)
			if len(got) != 1 || got[0]["type"] != wire.TypeError || got[0]["message"] != runtime.ErrNotLocal.Error() {
				t.Errorf("%s as a joined %s → %v, want one refusal", name, role, got)
			}
			for _, f := range got {
				if f["nonce"] != nil {
					t.Errorf("%s as a joined %s was answered with a nonce", name, role)
				}
			}
		}
		// The refusals did not spend the owner's nonce either.
		if peerT.injects+peerT.detects+peerT.sugges != 0 {
			t.Fatalf("a typist was asked for a joined %s: %+v", role, peerT)
		}
		time.Sleep(50 * time.Millisecond)
		if got := pty.String(); strings.Contains(got, "keel-joined") {
			t.Fatalf("a joined %s made keel serve type %q", role, got)
		}
	}

	refused("follower")
	if err := peer.Keys([]byte("follower-keys")); err != nil {
		t.Fatal(err)
	}

	owner.Control(frame(t, wire.Control{Type: wire.TypeGrantControl, TargetID: peer.ClientID()}))
	if !sess.DrivenByJoined() {
		t.Fatal("the peer was not handed control")
	}
	refused("driver")
	// The session issues a joined driver no nonce, whoever asks for it.
	if nonce, err := sess.IssueInjectTicket(peer.ClientID(), cmd); err != runtime.ErrNotLocal || nonce != "" {
		t.Fatalf("a joined driver's nonce: %q, %v", nonce, err)
	}

	// What a joined driver can do: type its own keys.
	if err := peer.Keys([]byte("driver-keys")); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(2 * time.Second); !strings.Contains(pty.String(), "driver-keys"); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the joined driver's keys did not reach the PTY: %q", pty.String())
		}
	}
	if got := pty.String(); got != "driver-keys" {
		t.Fatalf("the PTY got %q, want only the joined driver's own keys", got)
	}

	// The same frames from the window do reach its typist, and the owner's
	// nonce, untouched by the peer's attempts, still writes once for the
	// owner when it drives again.
	owner.Control(frame(t, wire.Control{Type: wire.TypeTakeControl}))
	owner.Control(frame(t, wire.Control{Type: wire.TypeAgentDetect, Seq: 1, Line: "x"}))
	owner.Control(frame(t, wire.Control{Type: wire.TypeAgentSuggest, Seq: 1, Line: "x"}))
	owner.Control(frame(t, wire.Control{Type: wire.TypeInject, Command: cmd, Nonce: ownerNonce}))
	if ownerT.injects != 1 || ownerT.detects != 1 || ownerT.sugges != 1 {
		t.Fatalf("the window's typist was asked %+v", ownerT)
	}
	for deadline := time.Now().Add(2 * time.Second); !strings.Contains(pty.String(), "keel-joined"); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the owner's own approved inject was not written: %q", pty.String())
		}
	}
}

// A joined peer is told nothing of the host: no host id, and never the
// refused host key or its trust offer, in a state frame or an error frame.
func TestAJoinedPeerIsNotToldTheHostOrItsKeyOffer(t *testing.T) {
	sess, _ := readySession(t)
	localRec, joinedRec := &recorder{}, &recorder{}
	local := Attach(sess, localRec, Options{Name: "me", Kind: wire.KindWindow, Caps: Local, Typist: func(*Conn) Typist { return &typist{} }})
	defer local.Detach()
	joined := Attach(sess, joinedRec, Options{Name: "bob", Kind: wire.KindJoined, Caps: Joined})
	defer joined.Detach()
	if ready := localRec.since(0)[0]; ready["type"] != wire.TypeReady || ready["hostId"] == nil {
		t.Fatalf("the window's ready frame: %v", ready)
	}
	if ready := joinedRec.since(0)[0]; ready["type"] != wire.TypeReady || ready["hostId"] != nil || ready["you"].(map[string]any)["kind"] != wire.KindJoined {
		t.Fatalf("the joined peer's ready frame: %v", ready)
	}

	info := sess.Info()
	info.Status = runtime.StatusError
	info.HostKey = wire.HostKey{Kind: wire.HostKeyUnknown, Hostname: "box:22", Fingerprint: "SHA256:abc", KeyType: "ssh-ed25519", Token: "offer-token-1"}
	for name, c := range map[string]*Conn{"window": local, "joined": joined} {
		b, err := json.Marshal(c.state(wire.TypeStatus, "unknown host key", info, 0, 0))
		if err != nil {
			t.Fatal(err)
		}
		has := strings.Contains(string(b), "offer-token-1") && strings.Contains(string(b), `"hostId"`) && strings.Contains(string(b), `"trustToken"`)
		if name == "window" && !has {
			t.Errorf("the window's frame lacks the host or the offer: %s", b)
		}
		if name == "joined" && (strings.Contains(string(b), "offer-token-1") || strings.Contains(string(b), "hostId") || strings.Contains(string(b), "errorKind") || strings.Contains(string(b), "SHA256:abc")) {
			t.Errorf("the joined peer's frame names the host or its key: %s", b)
		}
	}
}

// The socket's write paths, by source: live keys go to Session.Write and
// nowhere else; a nonce is issued and an inject handed on only behind
// Caps.Inject; and nothing here writes for a client by itself or mints an
// approval from what a client sent.
func TestTheSocketsWritePathsBySource(t *testing.T) {
	src, err := os.ReadFile("sessionsock.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	if strings.Count(body, "c.sess.Write(c.cl.ID, p)") != 1 || strings.Count(body, ".Write(") != 1 {
		t.Fatal("live keys must go to Session.Write, once, and nothing else here may write")
	}
	for _, guarded := range []string{"c.sess.IssueInjectTicket(c.cl.ID, ctl.Command)", "c.typist.Inject(ctl)", "c.typist.Detect(", "c.typist.Suggest("} {
		i := strings.Index(body, guarded)
		if i < 0 || strings.Count(body, guarded) != 1 {
			t.Fatalf("%s must appear once", guarded)
		}
		before := body[:i]
		gate := strings.LastIndex(before, "if !c.caps.")
		if gate < 0 || strings.Count(before[gate:], "case wire.") != 0 || !strings.Contains(before[gate:], "err = runtime.ErrNotLocal\n\t\t\tbreak") {
			t.Fatalf("%s is not behind a capability check in its own case", guarded)
		}
	}
	for _, bad := range []string{".InjectTicket(", ".Inject(c.cl.ID", "inject.Approve()", "inject.Approval{"} {
		if strings.Contains(body, bad) {
			t.Fatalf("sessionsock.go holds %q: it never writes for a client itself", bad)
		}
	}
}
