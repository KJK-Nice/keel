package native

import (
	"context"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
	"keel/internal/native/layout"
)

type fakeConn struct {
	id       string
	closed   bool
	status   string
	onChange func()

	wrote int               // bytes written to the session
	st    *keelclient.State // when set, Snapshot returns it (its Status wins over status)
	took  int               // TakeControl calls
	asked int               // RequestControl calls

	granted []uint64           // GrantControl, the peer ids in order
	events  []keelclient.Event // what Events hands over once

	injected  []string               // what ApprovedInject was asked to type, in order
	injectErr error                  // what it answers
	agentRefs []*keelclient.AgentRef // the agent reference of each, nil for a snippet

	// DirectType, recorded: the bare command, the direct part and the agent reference of each call, in order.
	directs   []fakeDirect
	directRes keelclient.DirectResult // what it answers; the zero value is a refusal with no words, so a test sets it
	directErr error
	directGo  chan struct{} // when set, a DirectType waits for it to be closed

	// In-pane suggestions, recorded: the questions (seq and line), the answers keel serve "pushed", the inserts asked for
	// and what they answer, and the words restored.
	detects, asks []fakeAsk
	sugEvents     []keelclient.SuggestEvent
	sugInserts    []keelclient.SuggestionRef
	sugInsertErr  error
	restored      []string
}

// fakeAsk is one SuggestDetect or SuggestAsk call.
type fakeAsk struct {
	seq  int
	line string
}

// fakeDirect is one DirectType call.
type fakeDirect struct {
	command string
	ref     keelclient.DirectRef
	agent   *keelclient.AgentRef
}

func (c *fakeConn) state() keelclient.State {
	if c.st != nil {
		return *c.st
	}
	return keelclient.State{Status: c.status}
}

func (c *fakeConn) Read([]byte) (int, error)    { return 0, io.EOF }
func (c *fakeConn) Write(p []byte) (int, error) { c.wrote += len(p); return len(p), nil }
func (c *fakeConn) Close() error                { c.closed = true; return nil }
func (c *fakeConn) Resize(int, int) error       { return nil }
func (c *fakeConn) SessionID() string           { return c.id }
func (c *fakeConn) Snapshot() keelclient.State  { return c.state() }
func (c *fakeConn) RequestControl() error       { c.asked++; return nil }
func (c *fakeConn) TakeControl() error          { c.took++; return nil }
func (c *fakeConn) OnChange(fn func())          { c.onChange = fn }
func (c *fakeConn) GrantControl(id uint64) error {
	c.granted = append(c.granted, id)
	return nil
}
func (c *fakeConn) Events() []keelclient.Event {
	out := c.events
	c.events = nil
	return out
}
func (c *fakeConn) ApprovedInject(_ context.Context, command string, agent *keelclient.AgentRef) error {
	c.injected = append(c.injected, command)
	c.agentRefs = append(c.agentRefs, agent)
	return c.injectErr
}
func (c *fakeConn) DirectType(_ context.Context, command string, ref keelclient.DirectRef, agent *keelclient.AgentRef) (keelclient.DirectResult, error) {
	c.directs = append(c.directs, fakeDirect{command, ref, agent})
	if c.directGo != nil {
		<-c.directGo
	}
	return c.directRes, c.directErr
}

func (c *fakeConn) SuggestDetect(seq int, line string) error {
	c.detects = append(c.detects, fakeAsk{seq, line})
	return nil
}
func (c *fakeConn) SuggestAsk(seq int, line string) error {
	c.asks = append(c.asks, fakeAsk{seq, line})
	return nil
}
func (c *fakeConn) SuggestEvents() []keelclient.SuggestEvent {
	out := c.sugEvents
	c.sugEvents = nil
	return out
}
func (c *fakeConn) SuggestInsert(_ context.Context, ref keelclient.SuggestionRef) error {
	c.sugInserts = append(c.sugInserts, ref)
	return c.sugInsertErr
}
func (c *fakeConn) RestoreWords(words string) error {
	c.restored = append(c.restored, words)
	return nil
}

// fakeBackend records what the workspace asks of keel serve.
type fakeBackend struct {
	vault    keelclient.Vault
	hosts    []keelclient.Host
	sessions []keelclient.Session
	calls    []string
	spawned  []string // host ids
	attached []string // session ids
	killed   []string
	conns    []*fakeConn
	n        int

	reconnected []string
	trusted     []keelclient.HostKey
	replaced    []bool

	created []keelclient.NewHost // what CreateHost was asked for

	// The Unlock screen and the host dialog. unlockWant is the passphrase that opens the vault; the fake keeps
	// no passphrase.
	unlockWant  string
	unlockCalls int
	vaultCalls  int
	hostsCalls  int

	saved       []keelclient.HostDetail // SaveHost, as sent
	deleted     []string
	duplicated  []string
	tests       []keelclient.HostDetail // TestHost, as sent
	testReply   []keelclient.TestResult // answered in turn; the last repeats
	testWait    chan struct{}           // when set, TestHost waits for it or its context
	savedKey    keelclient.KnownKey
	forgot      []string
	readable    map[string]bool
	identities  []keelclient.Identity
	groups      []keelclient.Group
	saveErr     error
	nextHostSeq int

	// First run and the pages.
	setups       []string // SetupVault, the passphrases as sent (made-up test values)
	setupErr     error
	preview      keelclient.SSHPreview
	previewErr   error
	imports      []keelclient.ImportRequest
	importErr    error
	importRes    keelclient.ImportResult
	firstRunDone int
	identSaved   []keelclient.IdentityInput
	identDeleted []string
	identSaveErr error
	logs         []keelclient.LogEntry
	logsCalls    int
	logLimit     int
	nextIdentSeq int

	// Snippets.
	snippets     []keelclient.Snippet
	snipSaved    []keelclient.SnippetInput
	snipDeleted  []string
	snipSaveErr  error
	nextSnipSeq  int
	snippetCalls int

	// Port forwarding.
	tunnels     []keelclient.Tunnel
	tunSaved    []keelclient.TunnelInput
	tunStarted  []string
	tunStopped  []string
	tunDeleted  []string
	tunStartErr error // what StartTunnel answers
	tunSaveErr  error
	tunCalls    int

	// Settings.
	runtime      keelclient.Runtime
	signalSet    []string // SetSignal, as sent
	signalErr    error
	runtimeStops int
	stopErr      error

	noFirstRunAfterSetup bool // SetupVault leaves firstRun false

	agent fakeAgent
	convs fakeConvs
	mem   fakeMemory
	fold  fakeFolders

	// Share.
	shareURL     string   // what Share answers
	shareErr     error    // what Share answers instead
	shared       []string // Share, the session ids
	admitted     []string // AdmitShare, "<session> <peer> <allow>"
	admitErr     error
	stopped      []string // StopShare, the session ids
	shareStopErr error
}

func (f *fakeBackend) Share(_ context.Context, sid string) (string, error) {
	f.rec("share")
	f.shared = append(f.shared, sid)
	return f.shareURL, f.shareErr
}

func (f *fakeBackend) AdmitShare(_ context.Context, sid, peer string, allow bool) error {
	f.rec("admit")
	f.admitted = append(f.admitted, fmt.Sprintf("%s %s %v", sid, peer, allow))
	return f.admitErr
}

func (f *fakeBackend) StopShare(_ context.Context, sid string) error {
	f.rec("stop share")
	f.stopped = append(f.stopped, sid)
	return f.shareStopErr
}

func (f *fakeBackend) rec(s string) { f.calls = append(f.calls, s) }

func (f *fakeBackend) Vault(context.Context) (keelclient.Vault, error) {
	f.rec("vault")
	f.vaultCalls++
	return f.vault, nil
}
func (f *fakeBackend) Hosts(context.Context) ([]keelclient.Host, error) {
	f.rec("hosts")
	f.hostsCalls++
	return append([]keelclient.Host(nil), f.hosts...), nil
}
func (f *fakeBackend) Sessions(context.Context) ([]keelclient.Session, error) {
	f.rec("sessions")
	return f.sessions, nil
}
func (f *fakeBackend) Spawn(hostID string, _, _ int) (session, error) {
	f.n++
	c := &fakeConn{id: fmt.Sprintf("s%d", f.n), status: "ready"}
	f.spawned = append(f.spawned, hostID)
	f.conns = append(f.conns, c)
	return c, nil
}
func (f *fakeBackend) Attach(id string, _, _ int) (session, error) {
	c := &fakeConn{id: id, status: "ready"}
	f.attached = append(f.attached, id)
	f.conns = append(f.conns, c)
	return c, nil
}
func (f *fakeBackend) Kill(_ context.Context, id string) error {
	f.killed = append(f.killed, id)
	return nil
}
func (f *fakeBackend) Reconnect(_ context.Context, id string) error {
	f.calls = append(f.calls, "reconnect "+id)
	f.reconnected = append(f.reconnected, id)
	return nil
}
func (f *fakeBackend) TrustHostKey(_ context.Context, k keelclient.HostKey, replace bool) error {
	f.calls = append(f.calls, "trust")
	f.trusted = append(f.trusted, k)
	f.replaced = append(f.replaced, replace)
	return nil
}

func (f *fakeBackend) CreateHost(_ context.Context, h keelclient.NewHost) (keelclient.Host, error) {
	f.calls = append(f.calls, "create host")
	f.created = append(f.created, h)
	out := keelclient.Host{ID: fmt.Sprintf("h-new%d", len(f.created)), Name: h.Name, Group: h.Group, Address: h.Address, Username: h.Username, AuthType: h.AuthType, Color: h.Color, Port: h.Port}
	f.hosts = append(f.hosts, out)
	return out, nil
}

func (f *fakeBackend) Unlock(_ context.Context, pass string) error {
	f.rec("unlock")
	f.unlockCalls++
	if pass != f.unlockWant {
		return keelclient.ErrPassphrase
	}
	f.vault.Locked = false
	return nil
}

func (f *fakeBackend) SaveHost(_ context.Context, h keelclient.HostDetail) (keelclient.Host, error) {
	f.rec("save host")
	f.saved = append(f.saved, h)
	if f.saveErr != nil {
		return keelclient.Host{}, f.saveErr
	}
	out := keelclient.Host{ID: h.ID, Name: h.Name, GroupID: h.GroupID, Address: h.Address, Username: h.Username, AuthType: h.AuthType,
		Color: h.Color, Port: h.Port, Tags: h.Tags, JumpHost: h.JumpHost, StartupCommand: h.StartupCommand, KeepAliveSec: h.KeepAliveSec,
		NoAutoReconnect: h.NoAutoReconnect, IdentityID: h.IdentityID, AgentReadable: h.AgentReadable, Production: h.Production, AgentNotes: h.AgentNotes}
	if out.ID == "" {
		f.nextHostSeq++
		out.ID = fmt.Sprintf("h-saved%d", f.nextHostSeq)
		f.hosts = append(f.hosts, out)
		return out, nil
	}
	for i := range f.hosts {
		if f.hosts[i].ID == out.ID {
			f.hosts[i] = out
		}
	}
	return out, nil
}

func (f *fakeBackend) DeleteHost(_ context.Context, id string) error {
	f.rec("delete host")
	f.deleted = append(f.deleted, id)
	for i, h := range f.hosts {
		if h.ID == id {
			f.hosts = append(f.hosts[:i:i], f.hosts[i+1:]...)
			break
		}
	}
	return nil
}

func (f *fakeBackend) DuplicateHost(_ context.Context, id string) (keelclient.Host, error) {
	f.rec("duplicate host")
	f.duplicated = append(f.duplicated, id)
	for _, h := range f.hosts {
		if h.ID == id {
			h.ID, h.Name = id+"-copy", h.Name+" (copy)"
			f.hosts = append(f.hosts, h)
			return h, nil
		}
	}
	return keelclient.Host{}, fmt.Errorf("not found")
}

func (f *fakeBackend) TestHost(ctx context.Context, h keelclient.HostDetail) (keelclient.TestResult, error) {
	f.rec("test host")
	f.tests = append(f.tests, h)
	if f.testWait != nil {
		select {
		case <-f.testWait:
		case <-ctx.Done():
			return keelclient.TestResult{}, ctx.Err()
		}
	}
	if len(f.testReply) == 0 {
		return keelclient.TestResult{OK: true, Kind: "ok", LatencyMs: 12}, nil
	}
	r := f.testReply[0]
	if len(f.testReply) > 1 {
		f.testReply = f.testReply[1:]
	}
	return r, nil
}

func (f *fakeBackend) HostKey(context.Context, string) (keelclient.KnownKey, error) {
	f.rec("host key")
	return f.savedKey, nil
}

func (f *fakeBackend) ForgetHostKey(_ context.Context, id string) error {
	f.rec("forget host key")
	f.forgot = append(f.forgot, id)
	f.savedKey = keelclient.KnownKey{}
	return nil
}

func (f *fakeBackend) SetAgentReadable(_ context.Context, id string, v bool) error {
	f.rec("agent readable")
	if f.readable == nil {
		f.readable = map[string]bool{}
	}
	f.readable[id] = v
	return nil
}

func (f *fakeBackend) Identities(context.Context) ([]keelclient.Identity, error) {
	return append([]keelclient.Identity(nil), f.identities...), nil
}

func (f *fakeBackend) SetupVault(_ context.Context, pass string) error {
	f.rec("setup vault")
	f.setups = append(f.setups, pass)
	if f.setupErr != nil {
		return f.setupErr
	}
	f.vault.NeedsSetup, f.vault.Locked, f.vault.FirstRun = false, false, !f.noFirstRunAfterSetup
	return nil
}

func (f *fakeBackend) SSHConfigPreview(context.Context) (keelclient.SSHPreview, error) {
	f.rec("ssh preview")
	return f.preview, f.previewErr
}

func (f *fakeBackend) SSHConfigImport(_ context.Context, req keelclient.ImportRequest) (keelclient.ImportResult, error) {
	f.rec("ssh import")
	f.imports = append(f.imports, req)
	return f.importRes, f.importErr
}

func (f *fakeBackend) FirstRunDone(context.Context) error {
	f.rec("first run done")
	f.firstRunDone++
	f.vault.FirstRun = false
	return nil
}

func (f *fakeBackend) SaveIdentity(_ context.Context, in keelclient.IdentityInput) (keelclient.Identity, error) {
	f.rec("save identity")
	f.identSaved = append(f.identSaved, in)
	if f.identSaveErr != nil {
		return keelclient.Identity{}, f.identSaveErr
	}
	out := keelclient.Identity{ID: in.ID, Name: in.Name, Kind: in.Kind, Username: in.Username,
		HasPassword: in.Password != "", HasPrivateKey: in.PrivateKey != "", HasPassphrase: in.Passphrase != ""}
	if in.ID == "" {
		f.nextIdentSeq++
		out.ID = fmt.Sprintf("i-new%d", f.nextIdentSeq)
		f.identities = append(f.identities, out)
		return out, nil
	}
	for i := range f.identities {
		if f.identities[i].ID == in.ID {
			old := f.identities[i]
			out.HasPassword = out.HasPassword || old.HasPassword
			out.HasPrivateKey = out.HasPrivateKey || old.HasPrivateKey
			out.HasPassphrase = out.HasPassphrase || old.HasPassphrase
			out.CreatedAt = old.CreatedAt
			f.identities[i] = out
		}
	}
	return out, nil
}

func (f *fakeBackend) DeleteIdentity(_ context.Context, id string) error {
	f.rec("delete identity")
	f.identDeleted = append(f.identDeleted, id)
	for i, x := range f.identities {
		if x.ID == id {
			f.identities = append(f.identities[:i:i], f.identities[i+1:]...)
			break
		}
	}
	return nil
}

func (f *fakeBackend) Logs(_ context.Context, limit int) ([]keelclient.LogEntry, error) {
	f.rec("logs")
	f.logsCalls++
	f.logLimit = limit
	return append([]keelclient.LogEntry(nil), f.logs...), nil
}
func (f *fakeBackend) Tunnels(context.Context) ([]keelclient.Tunnel, error) {
	f.tunCalls++
	return append([]keelclient.Tunnel(nil), f.tunnels...), nil
}

func (f *fakeBackend) SaveTunnel(_ context.Context, in keelclient.TunnelInput) (keelclient.Tunnel, error) {
	f.rec("save tunnel")
	f.tunSaved = append(f.tunSaved, in)
	if f.tunSaveErr != nil {
		return keelclient.Tunnel{}, f.tunSaveErr
	}
	out := keelclient.Tunnel{ID: in.ID, Name: in.Name, HostID: in.HostID, Kind: in.Kind, BindAddr: in.BindAddr, BindPort: in.BindPort,
		DestHost: in.DestHost, DestPort: in.DestPort, Status: "stopped"}
	if in.ID == "" {
		out.ID = fmt.Sprintf("t-new%d", len(f.tunSaved))
		f.tunnels = append(f.tunnels, out)
		return out, nil
	}
	for i := range f.tunnels {
		if f.tunnels[i].ID == in.ID {
			out.Status, out.Error, out.ListenAddr = f.tunnels[i].Status, f.tunnels[i].Error, f.tunnels[i].ListenAddr
			f.tunnels[i] = out
		}
	}
	return out, nil
}

func (f *fakeBackend) DeleteTunnel(_ context.Context, id string) error {
	f.rec("delete tunnel")
	f.tunDeleted = append(f.tunDeleted, id)
	for i, t := range f.tunnels {
		if t.ID == id {
			f.tunnels = append(f.tunnels[:i:i], f.tunnels[i+1:]...)
			break
		}
	}
	return nil
}

func (f *fakeBackend) setTunnel(id, status string) keelclient.Tunnel {
	for i := range f.tunnels {
		if f.tunnels[i].ID == id {
			f.tunnels[i].Status = status
			return f.tunnels[i]
		}
	}
	return keelclient.Tunnel{}
}

func (f *fakeBackend) StartTunnel(_ context.Context, id string) (keelclient.Tunnel, error) {
	f.rec("start tunnel " + id)
	f.tunStarted = append(f.tunStarted, id)
	if f.tunStartErr != nil {
		return keelclient.Tunnel{}, f.tunStartErr
	}
	return f.setTunnel(id, "ready"), nil
}

func (f *fakeBackend) StopTunnel(_ context.Context, id string) (keelclient.Tunnel, error) {
	f.rec("stop tunnel " + id)
	f.tunStopped = append(f.tunStopped, id)
	return f.setTunnel(id, "stopped"), nil
}

func (f *fakeBackend) Runtime(context.Context) (keelclient.Runtime, error) {
	f.rec("runtime")
	r := f.runtime
	r.Sessions = len(f.conns)
	return r, nil
}

func (f *fakeBackend) SetSignal(_ context.Context, url string) (keelclient.Runtime, error) {
	f.rec("set signal")
	f.signalSet = append(f.signalSet, url)
	if f.signalErr != nil {
		return keelclient.Runtime{}, f.signalErr
	}
	f.runtime.SignalSaved, f.runtime.SignalURL, f.runtime.SignalSource = url, url, "settings"
	if url == "" {
		f.runtime.SignalSource = ""
	}
	return f.runtime, nil
}

func (f *fakeBackend) StopRuntime(context.Context) error {
	f.rec("stop runtime")
	f.runtimeStops++
	return f.stopErr
}

func (f *fakeBackend) Groups(context.Context) ([]keelclient.Group, error) { return f.groups, nil }

var testHosts = []keelclient.Host{
	{ID: "h-pg", Name: "pg-primary", Group: "production", Address: "10.0.0.3", Username: "dba", AuthType: "key"},
	{ID: "h-api", Name: "api-1", Group: "production", Address: "10.0.0.1", Username: "deploy", AuthType: "key"},
	{ID: "h-web", Name: "staging-web", Group: "staging", Address: "10.1.0.1", Username: "ci", AuthType: "password"},
	{ID: "h-local", Name: "Local Shell", Address: "localhost", Username: "me", AuthType: "local", Color: "var(--ansi-10)"},
}

// placeholder is a focusable body, as a terminal view is, so a click lands on it and takes keyboard focus.
func placeholder(released *int) func(p *pane) {
	return func(p *pane) {
		p.view = func(c *ui.Context) ui.Element {
			return ui.Box(c).Fill().Focusable().Label("body " + p.id).Children(func() { ui.Text(c, "placeholder").TextColor(ink) })
		}
		p.size = func() (int, int) { return 80, 24 }
		p.release = func() { *released++ }
	}
}

type rig struct {
	w        *workspace
	fb       *fakeBackend
	tt       *ui.Tester
	released int
}

// newRig starts a workspace on a fake keel serve; the fake runs work and posts at once.
func newRig(t *testing.T, fb *fakeBackend) *rig {
	t.Helper()
	r := &rig{fb: fb}
	r.w = newWorkspace(fb)
	r.w.work = func(fn func()) { fn() }
	r.w.post = func(fn func()) { fn() }
	r.w.mkBody = placeholder(&r.released)
	r.w.start()
	r.tt = ui.NewTester(r.w.view, 1280, 720)
	return r
}

func okBackend() *fakeBackend { return &fakeBackend{hosts: testHosts} }

// open clicks a host row and settles the frames.
func (r *rig) click(t *testing.T, name string) {
	t.Helper()
	if err := r.tt.Click(name); err != nil {
		t.Fatalf("click %q: %v", name, err)
	}
	r.tt.Frame()
}

func TestLockedOrUnsetVaultMakesNoOtherCall(t *testing.T) {
	// A locked vault that needs no setup shows the Unlock screen, whatever firstRun says; setup, and an unlocked
	// first run, show First run's step 1 (gate.js update). None of them reads the hosts or the sessions.
	for _, c := range []struct {
		v      keelclient.Vault
		unlock bool
	}{{keelclient.Vault{Locked: true}, true}, {keelclient.Vault{Locked: true, NeedsSetup: true}, false}, {keelclient.Vault{NeedsSetup: true}, false},
		{keelclient.Vault{FirstRun: true}, false}, {keelclient.Vault{Locked: true, FirstRun: true}, true}} {
		fb := &fakeBackend{vault: c.v, hosts: testHosts}
		r := newRig(t, fb)
		r.tt.Frame()
		if c.unlock != r.tt.HasText("Unlock your vault") || c.unlock == r.tt.HasText("Welcome to Keel") {
			t.Errorf("%+v: unlock screen %v, first run %v", c.v, r.tt.HasText("Unlock your vault"), r.tt.HasText("Welcome to Keel"))
		}
		if !slices.Equal(fb.calls, []string{"vault"}) {
			t.Errorf("%+v: calls %v", c.v, fb.calls)
		}
	}
}

func TestHostsGroupedSortedAndFiltered(t *testing.T) {
	r := newRig(t, okBackend())
	var got []string
	for _, g := range r.w.groups() {
		got = append(got, g.name)
		for _, h := range g.hosts {
			got = append(got, h.Name)
		}
	}
	want := []string{"production", "api-1", "pg-primary", "staging", "staging-web", ungrouped, "Local Shell"}
	if !slices.Equal(got, want) {
		t.Fatalf("tree %v, want %v", got, want)
	}
	r.tt.Frame()
	for _, s := range []string{"production", "api-1", "pg-primary", "staging-web", ungrouped, "Local Shell"} {
		if !r.tt.HasText(s) {
			t.Errorf("no text %q", s)
		}
	}
	// The filter matches name, address and username.
	for q, want := range map[string]string{"pg": "pg-primary", "10.1.0": "staging-web", "deploy": "api-1"} {
		r.w.filter = q
		gs := r.w.groups()
		if len(gs) != 1 || len(gs[0].hosts) != 1 || gs[0].hosts[0].Name != want {
			t.Errorf("filter %q gave %+v", q, gs)
		}
	}
	r.w.filter = "pg"
	r.tt.Frame()
	if r.tt.HasText("api-1") || !r.tt.HasText("pg-primary") {
		t.Error("the filtered tree still shows api-1, or lost pg-primary")
	}
}

func TestFilterBoxTakesTyping(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.click(t, "Filter hosts")
	r.tt.Type("stag")
	r.tt.Frame()
	if r.w.filter != "stag" || r.tt.HasText("api-1") {
		t.Errorf("filter %q, api-1 shown: %v", r.w.filter, r.tt.HasText("api-1"))
	}
}

func TestClickingAHostSpawnsOnceAndAddsATab(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.click(t, "host api-1")
	if !slices.Equal(r.fb.spawned, []string{"h-api"}) || len(r.w.tabs) != 1 {
		t.Fatalf("spawned %v, tabs %d", r.fb.spawned, len(r.w.tabs))
	}
	if !r.tt.HasText("deploy@10.0.0.1") {
		t.Error("the pane bar does not show user@address")
	}
	if !r.tt.HasText("keel serve · 1 session") || r.tt.HasText("keel serve · 1 sessions") {
		t.Error("status bar session count must be singular for one")
	}
	if !r.tt.Focused("body 1") {
		t.Error("the new pane does not have keyboard focus")
	}
	r.click(t, "Split right")
	if !r.tt.HasText("keel serve · 2 sessions") {
		t.Error("status bar session count must be plural for two")
	}
}

func TestPlusOpensANewTabPage(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.click(t, "host api-1") // fills the New tab the empty window showed
	r.click(t, "New tab")
	if len(r.fb.spawned) != 1 || len(r.w.tabs) != 2 || !r.w.tabs[1].isNew() || r.w.active != 1 {
		t.Fatalf("spawned %v, tabs %d", r.fb.spawned, len(r.w.tabs))
	}
}

func TestLiveSessionsBecomeTabsAttachedByID(t *testing.T) {
	fb := okBackend()
	fb.sessions = []keelclient.Session{
		{ID: "s-a", HostID: "h-api", Status: "ready", Cols: 100, Rows: 30},
		{ID: "s-b", HostID: "h-gone", Title: "old host", Status: "connecting"},
		{ID: "s-c", HostID: "h-api", Status: "exited"},
	}
	r := newRig(t, fb)
	r.tt.Frame()
	if !slices.Equal(fb.attached, []string{"s-a", "s-b", "s-c"}) || len(fb.spawned) != 0 {
		t.Fatalf("attached %v spawned %v", fb.attached, fb.spawned)
	}
	if len(r.w.tabs) != 3 || r.w.tabs[1].name() != "old host" {
		t.Fatalf("tabs %d", len(r.w.tabs))
	}
	if r.w.paneCount() != 3 {
		t.Errorf("panes %d", r.w.paneCount())
	}
}

// pixel samples the tester image at DIP coordinates.
func (r *rig) pixel(x, y float32) [3]uint8 {
	img := r.tt.Image()
	s := float64(img.Bounds().Dx()) / 1280
	o := img.PixOffset(int(float64(x)*s), int(float64(y)*s))
	return [3]uint8{img.Pix[o], img.Pix[o+1], img.Pix[o+2]}
}

func is(got [3]uint8, want ui.Color) bool {
	d := func(a, b uint8) bool { return int(a)-int(b) <= 2 && int(b)-int(a) <= 2 }
	return d(got[0], want.R) && d(got[1], want.G) && d(got[2], want.B)
}

func TestSinglePaneIsFullBleedAndSplitHasARingOnTheFocusedTileOnly(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.click(t, "host api-1")
	if r.tt.Image() == nil {
		t.Skip("Tester.Image gave no pixels")
	}
	one, ok := r.tt.Find("pane 1")
	if !ok {
		t.Fatal("pane 1 not found")
	}
	// No radius: the corner pixel of the bar is the bar's ground, not the area behind a rounded tile.
	if got := r.pixel(one.X+1, one.Y+1); !is(got, surface) {
		t.Errorf("single pane corner %v, want surface (no radius)", got)
	}
	if one.H < 600 {
		t.Errorf("single pane height %v is not full-bleed", one.H)
	}

	r.click(t, "Split right")
	if len(r.w.tabs[0].panes) != 2 {
		t.Fatalf("panes %d", len(r.w.tabs[0].panes))
	}
	a, _ := r.tt.Find("pane 1")
	b, _ := r.tt.Find("pane 2")
	my := b.Y + b.H/2
	// The new pane is focused: its ring (3px of focus at 30%) lies in the gap before it; the other tile has none.
	ring := focus.Alpha(focusRingAlpha).Over(chrome)
	if got := r.pixel(b.X-1.5, my); !is(got, ring) {
		t.Errorf("focused tile outer edge %v, want ring %v", got, ring)
	}
	if got := r.pixel(a.X+a.W+1, my); !is(got, chrome) {
		t.Errorf("unfocused tile outer edge %v, want chrome", got)
	}
	if !r.tt.HasText("2") {
		t.Error("no pane count badge 2 on the tab")
	}
}

func TestSplitRightSpawnsOnTheSameHost(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.click(t, "host staging-web")
	r.click(t, "Split right")
	r.click(t, "Split down")
	if !slices.Equal(r.fb.spawned, []string{"h-web", "h-web", "h-web"}) {
		t.Fatalf("spawned %v", r.fb.spawned)
	}
	tb := r.w.tabs[0]
	if len(tb.panes) != 3 || !slices.Equal(tb.root.Leaves(), []string{"1", "3", "2"}) {
		// "Split down" acts on the first pane's bar (the first match), so it lands between 1 and 2.
		t.Fatalf("panes %d leaves %v", len(tb.panes), tb.root.Leaves())
	}
}

func TestClosingPanesKillsTheirSessions(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.click(t, "host api-1")
	r.click(t, "Split right") // s1 and s2
	r.click(t, "Close pane")  // the first bar's button: pane 1 = s1
	if !slices.Equal(r.fb.killed, []string{"s1"}) || len(r.w.tabs[0].panes) != 1 {
		t.Fatalf("killed %v panes %d", r.fb.killed, len(r.w.tabs[0].panes))
	}
	if !r.fb.conns[0].closed || r.fb.conns[1].closed {
		t.Error("only the closed pane's connection should be closed")
	}
	if r.w.tabs[0].focused != "2" {
		t.Errorf("focus %q after closing pane 1", r.w.tabs[0].focused)
	}
	r.click(t, "Close pane")
	if len(r.w.tabs) != 1 || !r.w.tabs[0].isNew() || !slices.Equal(r.fb.killed, []string{"s1", "s2"}) {
		t.Fatalf("tabs %d killed %v", len(r.w.tabs), r.fb.killed)
	}
	if !r.tt.HasText("Where to?") {
		t.Error("the empty window does not show the New tab page")
	}
}

func TestClosingAPaneBeforeItIsNamedKillsItWhenTheIdArrives(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.click(t, "host api-1")
	c := r.fb.conns[0]
	c.id = "" // the ready frame has not arrived
	r.click(t, "Close pane")
	if len(r.fb.killed) != 0 || c.closed || len(r.w.queued) != 0 {
		t.Fatalf("killed %v closed %v toasts %v before the id", r.fb.killed, c.closed, r.w.queued)
	}
	c.id = "s-late"
	c.onChange()
	if !slices.Equal(r.fb.killed, []string{"s-late"}) || !c.closed {
		t.Errorf("killed %v closed %v after the id", r.fb.killed, c.closed)
	}
}

func TestClosingTheWindowDetaches(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.click(t, "host api-1")
	r.click(t, "host pg-primary")
	r.click(t, "Split right")
	r.w.closeWindow()
	for i, c := range r.fb.conns {
		if !c.closed {
			t.Errorf("conn %d still open", i)
		}
	}
	if len(r.fb.killed) != 0 {
		t.Errorf("window close killed %v", r.fb.killed)
	}
	if r.released != 3 {
		t.Errorf("released %d bodies, want 3", r.released)
	}
}

func TestDraggingAGapChangesThatSplitsRatioWithinTheClamp(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.click(t, "host api-1")
	r.click(t, "Split right")
	root := r.w.tabs[0].root
	a, _ := r.tt.Find("pane 1")
	g, ok := r.tt.Find("split handle")
	if !ok || g.W != space1_5 {
		t.Fatalf("gap %v %v", g, ok)
	}
	x, y := g.X+g.W/2, g.Y+g.H/2
	r.tt.Press(x, y)
	r.tt.Move(x+100, y)
	r.tt.Move(x+100, y)
	r.tt.Release(x+100, y)
	r.tt.Frame()
	a2, _ := r.tt.Find("pane 1")
	if a2.W < a.W+90 || root.Ratio <= 0.5 {
		t.Errorf("tile 1 width %v (was %v), ratio %v after dragging right by 100", a2.W, a.W, root.Ratio)
	}
	g, _ = r.tt.Find("split handle")
	x = g.X + g.W/2
	r.tt.Press(x, y)
	r.tt.Move(x-2000, y)
	r.tt.Move(x-2000, y)
	r.tt.Release(x-2000, y)
	r.tt.Frame()
	a3, _ := r.tt.Find("pane 1")
	if a3.W < minTileWidth-1 || a3.W > minTileWidth+2 {
		t.Errorf("tile 1 width %v at the far left, want about %v", a3.W, float32(minTileWidth))
	}
}

func TestStackedGapDragsVertically(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.click(t, "host api-1")
	r.click(t, "Split down")
	root := r.w.tabs[0].root
	if root.Dir != layout.Col {
		t.Fatalf("dir %v", root.Dir)
	}
	g, ok := r.tt.Find("split handle")
	if !ok || g.H != space1_5 {
		t.Fatalf("gap %v %v", g, ok)
	}
	x, y := g.X+g.W/2, g.Y+g.H/2
	r.tt.Press(x, y)
	r.tt.Move(x, y+60)
	r.tt.Move(x, y+60)
	r.tt.Release(x, y+60)
	r.tt.Frame()
	if root.Ratio <= 0.5 {
		t.Errorf("ratio %v after dragging down", root.Ratio)
	}
}

func TestShortcuts(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.click(t, "host api-1")
	r.click(t, "host pg-primary")
	r.click(t, "host staging-web")
	if r.w.active != 2 {
		t.Fatalf("active %d", r.w.active)
	}
	r.tt.Key(ui.Cmd, ui.Key2)
	r.tt.Frame()
	if r.w.active != 1 {
		t.Errorf("Cmd+2 -> active %d", r.w.active)
	}
	r.tt.Key(ui.Cmd, ui.Key1)
	r.tt.Frame()
	r.tt.Key(ui.Cmd, ui.Key9)
	r.tt.Frame()
	if r.w.active != 2 {
		t.Errorf("Cmd+9 -> active %d", r.w.active)
	}
	r.tt.Key(ui.Cmd|ui.Shift, ui.KeyD)
	r.tt.Frame()
	if len(r.w.tabs[2].panes) != 2 || r.fb.spawned[len(r.fb.spawned)-1] != "h-web" {
		t.Errorf("Cmd+Shift+D: panes %d spawned %v", len(r.w.tabs[2].panes), r.fb.spawned)
	}
	r.tt.Key(ui.Cmd|ui.Shift, ui.KeyE)
	r.tt.Frame()
	if len(r.w.tabs[2].panes) != 3 {
		t.Errorf("Cmd+Shift+E: panes %d", len(r.w.tabs[2].panes))
	}
	r.tt.Key(ui.Cmd, ui.KeyW)
	r.tt.Frame()
	if len(r.w.tabs[2].panes) != 2 || len(r.fb.killed) != 1 {
		t.Errorf("Cmd+W: panes %d killed %v", len(r.w.tabs[2].panes), r.fb.killed)
	}
	r.tt.Key(ui.Cmd, ui.KeyB)
	r.tt.Frame()
	if r.w.sidebar || r.tt.HasText("api-1") && r.tt.HasText("Filter hosts") {
		t.Error("Cmd+B did not hide the sidebar")
	}
}

func TestSelectingATabShowsItsPane(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.click(t, "host api-1")
	r.click(t, "host pg-primary")
	r.click(t, "tab api-1")
	if r.w.active != 0 || !r.tt.Focused("body 1") {
		t.Errorf("active %d, body 1 focused %v", r.w.active, r.tt.Focused("body 1"))
	}
}

func TestHostTint(t *testing.T) {
	if hostTint("var(--ansi-10)") != ansi[10] || hostTint("teal") != inkMuted || hostTint("var(--ansi-99)") != inkMuted {
		t.Error("hostTint")
	}
}

// A narrow pane bar keeps its tools whole: user@address and the host name give way, not the buttons.
func TestNarrowPaneBarKeepsItsTools(t *testing.T) {
	long := keelclient.Host{ID: "h-long", Name: "a-very-long-production-host-name", Address: "very-long-address.internal.example.com", Username: "deploy-user", AuthType: "key"}
	r := newRig(t, &fakeBackend{hosts: []keelclient.Host{long}})
	r.tt.Frame()
	r.click(t, "host "+long.Name)
	r.click(t, "Split right")
	g, _ := r.tt.Find("split handle")
	x, y := g.X+g.W/2, g.Y+g.H/2
	r.tt.Press(x, y)
	r.tt.Move(x-2000, y)
	r.tt.Move(x-2000, y)
	r.tt.Release(x-2000, y)
	r.tt.Frame()
	tile, _ := r.tt.Find("pane 1")
	for _, tool := range []string{"Split right", "Split down", "Focus view", "More pane actions", "Close pane"} {
		b, ok := r.tt.Find(tool)
		if !ok || b.W == 0 || b.X < tile.X || b.X+b.W > tile.X+tile.W {
			t.Errorf("%s %v is not inside the tile %v", tool, b, tile)
		}
	}
}

// wantPixel checks one pixel of a Tester image against c at alpha a. The image holds premultiplied
// alpha (color.RGBA): a pixel of c at 0.66 is {c.R*.66, c.G*.66, c.B*.66, 168}. A tile tinted twice
// would show 0.88 alpha, which is what these checks rule out.
func wantPixel(t *testing.T, what string, img *image.RGBA, x, y float32, c ui.Color, a float32) {
	t.Helper()
	got := img.RGBAAt(int(x), int(y))
	near := func(g uint8, w float32) bool { d := float32(g) - w; return d >= -2 && d <= 2 }
	if !near(got.R, float32(c.R)*a) || !near(got.G, float32(c.G)*a) || !near(got.B, float32(c.B)*a) || !near(got.A, 255*a) {
		t.Errorf("%s at (%v,%v): got %v, want %v at alpha %v (premultiplied %v %v %v %v)", what, x, y, got, c, a,
			float32(c.R)*a, float32(c.G)*a, float32(c.B)*a, 255*a)
	}
}

func TestGroundsFollowTheWindowAlphaWithoutStacking(t *testing.T) {
	for _, a := range []float32{0.66, 1} {
		r := newRig(t, okBackend())
		r.w.alpha = a
		r.tt = ui.NewTester(r.w.view, 1280, 720)
		r.tt.Frame()
		r.click(t, "host api-1")
		img := r.tt.Image()
		wantPixel(t, "title bar", img, 900, 10, chrome, a)
		wantPixel(t, "sidebar", img, 100, 400, chrome, a)
		// One pane: full bleed, its body is stage once.
		p, _ := r.tt.Find("pane 1")
		wantPixel(t, "single pane body", img, p.X+p.W/2, p.Y+p.H/2, stage, a)
		wantPixel(t, "single pane bar", img, p.X+p.W/2, p.Y+sizePanebar/2, surface, a)
		r.click(t, "Split right")
		img = r.tt.Image()
		g, _ := r.tt.Find("split handle")
		wantPixel(t, "gap", img, g.X+1, g.Y+g.H/2, chrome, a)
		wantPixel(t, "inset", img, g.X-100, 40, chrome, a)
		// Pane 2 holds the keyboard: not dimmed.
		p2, _ := r.tt.Find("pane 2")
		wantPixel(t, "tile body", img, p2.X+p2.W/2, p2.Y+p2.H/2, stage, a)
		wantPixel(t, "tile bar", img, p2.X+p2.W/2, p2.Y+sizePanebar/2, surface, a)
		// The area outside each rounded corner is chrome once; the corner's centre of curvature is the tile.
		// (The focused tile's ring covers its own corners, so the outside is read on the unfocused tile.)
		p1, _ := r.tt.Find("pane 1")
		l, rt, top, bot := p1.X+1, p1.X+p1.W-2, p1.Y+1, p1.Y+p1.H-2
		wantPixel(t, "corner outside top-left", img, l, top, chrome, a)
		wantPixel(t, "corner outside top-right", img, rt, top, chrome, a)
		wantPixel(t, "corner outside bottom-left", img, l, bot, chrome, a)
		wantPixel(t, "corner outside bottom-right", img, rt, bot, chrome, a)
		wantPixel(t, "corner centre top-left", img, p2.X+radiusMd, p2.Y+radiusMd, surface, a)
		wantPixel(t, "corner centre bottom-right", img, p2.X+p2.W-radiusMd, p2.Y+p2.H-radiusMd, stage, a)
	}
}

func TestLoadPrefs(t *testing.T) {
	def := prefs{Opacity: 66, Blur: 20, TermSize: 12, Theme: "system", UISize: 12, TermFont: "jetbrains"}
	for _, tc := range []struct {
		name, file string
		want       prefs
	}{
		{"missing", "", def},
		{"bad json", "{", def},
		{"opacity too low", `{"opacity":10,"blur":8}`, prefs{Opacity: 66, Blur: 8, TermSize: 12, Theme: "system", UISize: 12, TermFont: "jetbrains"}},
		{"opacity too high", `{"opacity":200,"blur":8}`, prefs{Opacity: 66, Blur: 8, TermSize: 12, Theme: "system", UISize: 12, TermFont: "jetbrains"}},
		{"blur negative", `{"opacity":50,"blur":-1}`, prefs{Opacity: 50, Blur: 20, TermSize: 12, Theme: "system", UISize: 12, TermFont: "jetbrains"}},
		{"blur too high", `{"opacity":50,"blur":99}`, prefs{Opacity: 50, Blur: 20, TermSize: 12, Theme: "system", UISize: 12, TermFont: "jetbrains"}},
		{"only blur", `{"blur":0}`, prefs{Opacity: 66, Blur: 0, TermSize: 12, Theme: "system", UISize: 12, TermFont: "jetbrains"}},
		{"valid", `{"opacity":100,"blur":64}`, prefs{Opacity: 100, Blur: 64, TermSize: 12, Theme: "system", UISize: 12, TermFont: "jetbrains"}},
	} {
		dir := t.TempDir()
		if tc.file != "" {
			if err := os.WriteFile(filepath.Join(dir, "keel-native.json"), []byte(tc.file), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if got := loadPrefs(dir); got != tc.want {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
	if got := (prefs{Opacity: 66}).alpha(); got != 0.66 {
		t.Errorf("alpha %v", got)
	}
}

func TestPrefsEnvOverridesAndRejects(t *testing.T) {
	t.Setenv("KEEL_NATIVE_OPACITY", "50")
	t.Setenv("KEEL_NATIVE_BLUR", "")
	p, err := prefs{Opacity: 66, Blur: 20}.withEnv()
	if err != nil || p != (prefs{Opacity: 50, Blur: 20}) {
		t.Errorf("got %+v, %v", p, err)
	}
	t.Setenv("KEEL_NATIVE_BLUR", "lots")
	if _, err := (prefs{}).withEnv(); err == nil || !strings.Contains(err.Error(), "KEEL_NATIVE_BLUR") {
		t.Errorf("err %v", err)
	}
	t.Setenv("KEEL_NATIVE_BLUR", "")
	t.Setenv("KEEL_NATIVE_OPACITY", "5")
	if _, err := (prefs{}).withEnv(); err == nil || !strings.Contains(err.Error(), "KEEL_NATIVE_OPACITY") {
		t.Errorf("err %v", err)
	}
}

func (f *fakeBackend) Snippets(context.Context) ([]keelclient.Snippet, error) {
	f.snippetCalls++
	return append([]keelclient.Snippet(nil), f.snippets...), nil
}

func (f *fakeBackend) SaveSnippet(_ context.Context, in keelclient.SnippetInput) (keelclient.Snippet, error) {
	f.rec("save snippet")
	f.snipSaved = append(f.snipSaved, in)
	if f.snipSaveErr != nil {
		return keelclient.Snippet{}, f.snipSaveErr
	}
	out := keelclient.Snippet{ID: in.ID, Name: in.Name, Command: in.Command}
	if in.ID == "" {
		f.nextSnipSeq++
		out.ID = fmt.Sprintf("sn-new%d", f.nextSnipSeq)
		f.snippets = append(f.snippets, out)
		return out, nil
	}
	for i := range f.snippets {
		if f.snippets[i].ID == in.ID {
			out.AgentUsable, out.CreatedAt = f.snippets[i].AgentUsable, f.snippets[i].CreatedAt // the server keeps them
			f.snippets[i] = out
		}
	}
	return out, nil
}

func (f *fakeBackend) DeleteSnippet(_ context.Context, id string) error {
	f.rec("delete snippet")
	f.snipDeleted = append(f.snipDeleted, id)
	for i, x := range f.snippets {
		if x.ID == id {
			f.snippets = append(f.snippets[:i:i], f.snippets[i+1:]...)
			break
		}
	}
	return nil
}
