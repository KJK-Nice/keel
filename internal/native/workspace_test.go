package native

import (
	"context"
	"fmt"
	"io"
	"slices"
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
}

func (c *fakeConn) Read([]byte) (int, error)        { return 0, io.EOF }
func (c *fakeConn) Write(p []byte) (int, error)     { return len(p), nil }
func (c *fakeConn) Close() error                    { c.closed = true; return nil }
func (c *fakeConn) Resize(int, int) error           { return nil }
func (c *fakeConn) SessionID() string               { return c.id }
func (c *fakeConn) State() (string, string, string) { return c.status, "", "" }
func (c *fakeConn) OnChange(fn func())              { c.onChange = fn }

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
}

func (f *fakeBackend) rec(s string) { f.calls = append(f.calls, s) }

func (f *fakeBackend) Vault(context.Context) (keelclient.Vault, error) {
	f.rec("vault")
	return f.vault, nil
}
func (f *fakeBackend) Hosts(context.Context) ([]keelclient.Host, error) {
	f.rec("hosts")
	return f.hosts, nil
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

func TestLockedVaultMakesNoOtherCall(t *testing.T) {
	for _, v := range []keelclient.Vault{{Locked: true}, {NeedsSetup: true}, {FirstRun: true}} {
		fb := &fakeBackend{vault: v, hosts: testHosts}
		r := newRig(t, fb)
		r.tt.Frame()
		if !r.tt.HasText(lockedMessage) {
			t.Errorf("%+v: no locked message", v)
		}
		if !slices.Equal(fb.calls, []string{"vault"}) {
			t.Errorf("%+v: calls %v", v, fb.calls)
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
	if !r.tt.HasText("keel serve · 1 sessions") {
		t.Error("status bar session count")
	}
	if !r.tt.Focused("body 1") {
		t.Error("the new pane does not have keyboard focus")
	}
}

func TestPlusOpensTheLocalHost(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.click(t, "New tab")
	if !slices.Equal(r.fb.spawned, []string{"h-local"}) {
		t.Fatalf("spawned %v", r.fb.spawned)
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
	if !slices.Equal(fb.attached, []string{"s-a", "s-b"}) || len(fb.spawned) != 0 {
		t.Fatalf("attached %v spawned %v", fb.attached, fb.spawned)
	}
	if len(r.w.tabs) != 2 || r.w.tabs[1].name() != "old host" {
		t.Fatalf("tabs %d", len(r.w.tabs))
	}
	if r.w.paneCount() != 2 {
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
	if len(r.w.tabs) != 0 || !slices.Equal(r.fb.killed, []string{"s1", "s2"}) {
		t.Fatalf("tabs %d killed %v", len(r.w.tabs), r.fb.killed)
	}
	if !r.tt.HasText("No sessions") {
		t.Error("no empty-state message")
	}
}

func TestClosingAPaneBeforeItIsNamedKillsItWhenTheIdArrives(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.click(t, "host api-1")
	c := r.fb.conns[0]
	c.id = "" // the ready frame has not arrived
	r.click(t, "Close pane")
	if len(r.fb.killed) != 0 || c.closed || r.w.err != "" {
		t.Fatalf("killed %v closed %v err %q before the id", r.fb.killed, c.closed, r.w.err)
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
	if hostTint("var(--ansi-10)") != tok(hexAnsi[10]) || hostTint("teal") != inkMuted || hostTint("var(--ansi-99)") != inkMuted {
		t.Error("hostTint")
	}
}
