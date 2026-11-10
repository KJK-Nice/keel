package native

import (
	"context"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
	"keel/internal/native/layout"
)

const (
	lockedMessage = "The vault is locked. Unlock it in Keel, then reopen this window."
	callTimeout   = 15 * time.Second
	startCols     = 80 // the size a session starts at; the terminal view resizes it
	startRows     = 24
)

// session is what a pane needs of a keel serve connection (*keelclient.Conn, or a fake in tests).
type session interface {
	io.ReadWriteCloser
	Resize(cols, rows int) error
	SessionID() string
	Snapshot() keelclient.State
	TakeControl() error
	RequestControl() error
	OnChange(fn func())
}

// backend is keel serve as the workspace uses it.
type backend interface {
	Vault(ctx context.Context) (keelclient.Vault, error)
	Hosts(ctx context.Context) ([]keelclient.Host, error)
	Sessions(ctx context.Context) ([]keelclient.Session, error)
	Spawn(hostID string, cols, rows int) (session, error)
	Attach(sessionID string, cols, rows int) (session, error)
	Kill(ctx context.Context, id string) error
	Reconnect(ctx context.Context, id string) error
	TrustHostKey(ctx context.Context, k keelclient.HostKey, replace bool) error
}

// clientBackend adapts *keelclient.Client, whose Spawn and Attach return *Conn.
type clientBackend struct{ *keelclient.Client }

func (b clientBackend) Spawn(hostID string, cols, rows int) (session, error) {
	c, err := b.Client.Spawn(hostID, cols, rows)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (b clientBackend) Attach(id string, cols, rows int) (session, error) {
	c, err := b.Client.Attach(id, cols, rows)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// pane is one terminal of a tab.
type pane struct {
	id   string // local id
	host keelclient.Host
	conn session
	sid  string // session id when known before the ready frame (an attached session)

	// Set by workspace.mkBody: a live terminal, or a placeholder in tests.
	view    func(c *ui.Context) ui.Element
	size    func() (cols, rows int)
	release func()

	handle ui.Handle // the body's identity, for keyboard focus

	// Set by workspace.newPane and mkBody: the terminal font size to start at, what a zoom of the terminal
	// (the wheel) asks the workspace for, and the terminal's own search (nil for a placeholder).
	fontSize float32
	zoom     func(steps int, reset bool)
	search   *termSearch
	setFont  func(size float32)

	declined keelclient.HostKey // the host-key offer the user cancelled or answered: its dialog stays closed
}

func (p *pane) sessionID() string {
	if id := p.conn.SessionID(); id != "" {
		return id
	}
	return p.sid
}

// status is the session's last reported state; empty until the server says.
func (p *pane) status() string { return p.conn.Snapshot().Status }

type tab struct {
	root    *layout.Node
	panes   map[string]*pane
	focused string
	zoomed  string // the pane that fills the tab (focus view), or ""
	title   string // the name the user gave the tab, or ""

	h ui.Handle // the tab in the strip, for where a dragged tab lands
}

func (t *tab) name() string {
	if t.title != "" {
		return t.title
	}
	if p := t.panes[t.focused]; p != nil {
		return p.host.Name
	}
	return ""
}

type workspace struct {
	be backend
	// work runs blocking calls off the UI thread; post runs fn on it and redraws. Both run
	// fn at once in tests.
	work   func(fn func())
	post   func(fn func())
	mkBody func(p *pane)

	hosts     []keelclient.Host
	tabs      []*tab
	active    int
	filter    string
	sidebar   bool
	collapsed map[string]bool
	err       string // last user-facing error, shown in the status bar
	blocked   string // a message that replaces the workspace (locked vault, no keel serve)
	closed    bool

	seq       int
	wantFocus string                      // pane to give the keyboard to once it is on screen
	splits    map[*layout.Node]*ui.Handle // each split's container, for its size while dragging
	later     []func()                    // actions queued by the view, run when the frame is built

	alpha float32 // the window's opacity, 0–1; grounds are painted at it (see ground)

	dataDir  string // where keel-native.json and keel-native-workspace.json live; "" keeps nothing
	termSize int    // the terminal font size of every pane, termSizeMin–termSizeMax

	recent     []closedEntry // recently closed tabs and panes, newest first (⌘⇧T)
	renaming   *tab          // the tab whose name is being edited
	renameText string
	renameSeen bool // the rename field has had the keyboard
	renameH    ui.Handle

	findPane   *pane // the pane whose find bar is open
	findText   string
	findDone   string // the text the terminal's search was last given
	findFocus  bool
	findHandle ui.Handle

	saved saveState
}

// ground is c at the window's alpha: what every ground the view paints goes through. At 1 it is c.
func (w *workspace) ground(c ui.Color) ui.Color {
	if w.alpha >= 1 {
		return c
	}
	return c.Alpha(w.alpha)
}

func newWorkspace(be backend) *workspace {
	return &workspace{
		be: be, alpha: 1, sidebar: true, collapsed: map[string]bool{}, termSize: termSizeDefault,
		splits: map[*layout.Node]*ui.Handle{},
		work:   func(fn func()) { go fn() },
	}
}

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), callTimeout)
}

func (w *workspace) tab() *tab {
	if w.active < 0 || w.active >= len(w.tabs) {
		return nil
	}
	return w.tabs[w.active]
}

func (w *workspace) focusedPane() *pane {
	if t := w.tab(); t != nil {
		return t.panes[t.focused]
	}
	return nil
}

func (w *workspace) paneCount() (n int) {
	for _, t := range w.tabs {
		n += len(t.panes)
	}
	return n
}

// fail shows an error in the status bar.
func (w *workspace) fail(what string, err error) { w.err = what + ": " + err.Error() }

// start asks keel serve what exists. The vault answers first: a locked vault stops here.
func (w *workspace) start() {
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		v, err := w.be.Vault(c)
		if err != nil {
			w.post(func() { w.blocked = "Cannot reach keel serve: " + err.Error() })
			return
		}
		if v.Locked || v.NeedsSetup || v.FirstRun {
			w.post(func() { w.blocked = lockedMessage })
			return
		}
		hosts, err := w.be.Hosts(c)
		if err != nil {
			w.post(func() { w.fail("hosts", err) })
			return
		}
		sessions, err := w.be.Sessions(c)
		if err != nil {
			w.post(func() { w.hosts = hosts; w.fail("sessions", err) })
			return
		}
		type attached struct {
			host keelclient.Host
			conn session
			sid  string
		}
		var live []attached
		var failed error
		for _, s := range sessions {
			cols, rows := s.Cols, s.Rows
			if cols <= 0 || rows <= 0 {
				cols, rows = startCols, startRows
			}
			conn, err := w.be.Attach(s.ID, cols, rows)
			if err != nil {
				failed = err
				continue
			}
			h := keelclient.Host{ID: s.HostID, Name: s.Title, Color: s.Color}
			for _, k := range hosts {
				if k.ID == s.HostID {
					h = k
				}
			}
			live = append(live, attached{h, conn, s.ID})
		}
		doc := loadSaved(w.dataDir)
		w.post(func() {
			if w.closed {
				for _, a := range live {
					a.conn.Close()
				}
				return
			}
			w.hosts = hosts
			panes := make([]*pane, len(live))
			for i, a := range live {
				panes[i] = w.newPane(a.host, a.conn)
				panes[i].sid = a.sid
			}
			w.restore(doc, panes)
			if failed != nil {
				w.fail("attach", failed)
			}
			w.saved.ready = true
		})
	})
}

func (w *workspace) newPane(h keelclient.Host, conn session) *pane {
	w.seq++
	p := &pane{id: strconv.Itoa(w.seq), host: h, conn: conn, fontSize: float32(w.termSize)}
	p.zoom = func(steps int, reset bool) {
		if reset {
			w.setTermSize(termSizeDefault)
		} else {
			w.setTermSize(w.termSize + steps)
		}
	}
	conn.OnChange(func() { w.post(func() {}) }) // redraw the dot and the bar
	w.mkBody(p)
	return p
}

// addTab puts p alone in a new tab and shows it.
func (w *workspace) addTab(p *pane) {
	w.tabs = append(w.tabs, &tab{root: layout.Leaf(p.id), panes: map[string]*pane{p.id: p}, focused: p.id})
	w.selectTab(len(w.tabs) - 1)
}

func (w *workspace) selectTab(i int) {
	if i < 0 || i >= len(w.tabs) {
		return
	}
	w.active = i
	w.wantFocus = w.tabs[i].focused
}

// openHost starts a session on h in a new tab.
func (w *workspace) openHost(h keelclient.Host) {
	w.work(func() {
		conn, err := w.be.Spawn(h.ID, startCols, startRows)
		w.post(func() {
			switch {
			case err != nil:
				w.fail("open "+h.Name, err)
			case w.closed:
				conn.Close()
			default:
				w.addTab(w.newPane(h, conn))
			}
		})
	})
}

// newTab is the + button: the New tab page is phase N4, so it opens the first local host.
func (w *workspace) newTab() {
	for _, h := range w.hosts {
		if h.AuthType == "local" {
			w.openHost(h)
			return
		}
	}
	w.err = "No local host to open: pick a host in the sidebar"
}

// split starts another session to the same host as target and inserts it after target.
func (w *workspace) split(t *tab, target string, d layout.Dir) {
	p := t.panes[target]
	if p == nil {
		return
	}
	w.work(func() {
		conn, err := w.be.Spawn(p.host.ID, startCols, startRows)
		w.post(func() {
			if err != nil {
				w.fail("split "+p.host.Name, err)
				return
			}
			if w.closed || t.panes[target] == nil { // the window or the pane went away meanwhile
				conn.Close()
				return
			}
			np := w.newPane(p.host, conn)
			t.root = t.root.Split(target, np.id, d)
			t.panes[np.id] = np
			t.focused = np.id
			t.zoomed = "" // a split leaves focus view (workspace.js splitPane)
			if t == w.tab() {
				w.wantFocus = np.id
			}
		})
	})
}

func (w *workspace) splitFocused(d layout.Dir) {
	if t := w.tab(); t != nil {
		w.split(t, t.focused, d)
	}
}

// closePane ends that pane's session (Kill) and removes it; the last pane closes the tab.
func (w *workspace) closePane(t *tab, id string) {
	p := t.panes[id]
	if p == nil {
		return
	}
	kind := "pane"
	if len(t.panes) == 1 {
		kind = "tab" // the last pane is recorded as the tab (term.js closePane)
	}
	w.recordClosed(p, kind)
	w.dropPane(p)
	delete(t.panes, id)
	t.root = t.root.Remove(id)
	if t.zoomed == id {
		t.zoomed = ""
	}
	if t.root == nil {
		w.removeTab(t)
		return
	}
	if t.focused == id {
		t.focused = t.root.Leaves()[0]
		if t == w.tab() {
			w.wantFocus = t.focused
		}
	}
}

// reconnect asks keel serve to start p's session again; the socket reports the new status.
func (w *workspace) reconnect(p *pane) {
	sid := p.sessionID()
	if sid == "" {
		return
	}
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		if err := w.be.Reconnect(c, sid); err != nil {
			w.post(func() { w.fail("reconnect", err) })
		}
	})
}

// trustHostKey is the host key dialog's trust button and nothing else: it sends back exactly the offer the
// server made for p's session, then reconnects.
func (w *workspace) trustHostKey(p *pane, k keelclient.HostKey) {
	p.declined = k // the dialog closes now; a new offer opens it again
	sid := p.sessionID()
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		err := w.be.TrustHostKey(c, k, k.Kind == "host-key-mismatch")
		if err == nil && sid != "" {
			err = w.be.Reconnect(c, sid)
		}
		if err != nil {
			w.post(func() { w.fail("trust host key", err) })
		}
	})
}

func (w *workspace) kill(sid string) {
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		if err := w.be.Kill(c, sid); err != nil {
			w.post(func() { w.fail("close pane", err) })
		}
	})
}

// killWhenNamed ends a session that was closed before the server's ready frame named it: it kills the
// session once the id arrives and then closes the connection. A connection that never learns its id
// is closed after callTimeout.
func (w *workspace) killWhenNamed(conn session) {
	var once sync.Once
	finish := func() {
		once.Do(func() {
			id := conn.SessionID()
			if id == "" {
				conn.Close()
				return
			}
			w.work(func() {
				c, cancel := ctx()
				defer cancel()
				if err := w.be.Kill(c, id); err != nil {
					w.post(func() { w.fail("close pane", err) })
				}
				conn.Close()
			})
		})
	}
	conn.OnChange(func() {
		if conn.SessionID() != "" {
			finish()
		}
	})
	if conn.SessionID() != "" {
		finish()
		return
	}
	time.AfterFunc(callTimeout, finish)
}

func (w *workspace) closeFocused() {
	if t := w.tab(); t != nil {
		w.closePane(t, t.focused)
	}
}

func (w *workspace) release(p *pane) {
	if p.release != nil {
		p.release()
	}
	p.conn.Close()
}

// closeWindow detaches: every connection closes and no session is killed.
func (w *workspace) closeWindow() {
	w.saveNow()
	w.closed = true
	w.saved.stop()
	for _, t := range w.tabs {
		for _, p := range t.panes {
			w.release(p)
		}
	}
}

// hostGroup is one group of the host tree.
type hostGroup struct {
	name  string
	hosts []keelclient.Host
}

const ungrouped = "Ungrouped"

// groups is the host tree for the current filter: groups and hosts by name, no group last.
func (w *workspace) groups() []hostGroup {
	q := strings.ToLower(strings.TrimSpace(w.filter))
	by := map[string][]keelclient.Host{}
	for _, h := range w.hosts {
		if q != "" && !strings.Contains(strings.ToLower(h.Name+"\x00"+h.Address+"\x00"+h.Username), q) {
			continue
		}
		g := h.Group
		if g == "" {
			g = ungrouped
		}
		by[g] = append(by[g], h)
	}
	var out []hostGroup
	for g, hs := range by {
		sort.Slice(hs, func(i, j int) bool { return strings.ToLower(hs[i].Name) < strings.ToLower(hs[j].Name) })
		out = append(out, hostGroup{g, hs})
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].name == ungrouped) != (out[j].name == ungrouped) {
			return out[j].name == ungrouped
		}
		return strings.ToLower(out[i].name) < strings.ToLower(out[j].name)
	})
	return out
}

// panesOn counts the open panes on a host.
func (w *workspace) panesOn(hostID string) (n int) {
	for _, t := range w.tabs {
		for _, p := range t.panes {
			if p.host.ID == hostID {
				n++
			}
		}
	}
	return n
}

func (w *workspace) splitHandle(n *layout.Node) *ui.Handle {
	h := w.splits[n]
	if h == nil {
		h = new(ui.Handle)
		w.splits[n] = h
	}
	return h
}

// act queues a change the view asks for; it runs once the frame is built, never mid-tree.
func (w *workspace) act(fn func()) { w.later = append(w.later, fn) }
