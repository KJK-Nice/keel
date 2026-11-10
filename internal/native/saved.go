package native

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"keel/internal/native/layout"
)

// The workspace comes back: the tabs, how each is split, the custom titles, the focused and zoomed pane
// of each and the active tab are kept in <dataDir>/keel-native-workspace.json. It names sessions by their
// keel serve id and holds nothing else: no host, address, user or secret, and no terminal content. It is
// read and written only in the data dir. The webview keeps the same idea in localStorage
// (cmd/keel/web/js/lib/savedworkspace.js).

const (
	savedFile    = "keel-native-workspace.json"
	savedVersion = 1
	saveDelay    = 400 * time.Millisecond
	savedMaxSize = 1 << 20
	savedDepth   = 64 // nesting a tree may have; deeper is not something this client wrote
)

type savedDoc struct {
	V      int        `json:"v"`
	Active int        `json:"active"` // index into Tabs
	Tabs   []savedTab `json:"tabs"`
	// Pages are the page tabs (Keychain, Logs…) by name, in strip order; they come back after the session tabs.
	// ActivePage names the page that was shown, when one was (Active is then not used). Nothing of a page's
	// content is kept: a page reads the server when it opens.
	Pages      []string `json:"pages,omitempty"`
	ActivePage string   `json:"activePage,omitempty"`
}

type savedTab struct {
	Title   string     `json:"title,omitempty"`
	Focused string     `json:"focusedSessionId,omitempty"`
	Zoomed  string     `json:"zoomedSessionId,omitempty"`
	Layout  *savedNode `json:"layout"`
}

// savedNode is a leaf (Session) or a split (Dir "row" or "col", Ratio, A and B).
type savedNode struct {
	Session string     `json:"sessionId,omitempty"`
	Dir     string     `json:"dir,omitempty"`
	Ratio   float32    `json:"ratio,omitempty"`
	A       *savedNode `json:"a,omitempty"`
	B       *savedNode `json:"b,omitempty"`
}

// saveState is what the workspace keeps to write the file.
type saveState struct {
	ready bool   // start has restored the tabs; nothing is written before
	last  []byte // the last snapshot taken (UI thread)

	mu      sync.Mutex // guards the rest
	pending []byte
	timer   *time.Timer
}

func (s *saveState) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timer != nil {
		s.timer.Stop()
	}
}

// snapshot is the workspace as it is saved. A pane whose session has no id yet is left out.
func (w *workspace) snapshot() savedDoc {
	d := savedDoc{V: savedVersion, Tabs: []savedTab{}}
	for i, t := range w.tabs {
		if t.pg != nil {
			d.Pages = append(d.Pages, t.pg.name)
			if i == w.active {
				d.ActivePage = t.pg.name
			}
			continue
		}
		if t.remote { // joined tabs are left out: the link is not kept, and nothing of the host's is
			continue
		}
		root := t.snapNode(t.root)
		if root == nil {
			continue
		}
		if i == w.active {
			d.Active = len(d.Tabs)
		}
		st := savedTab{Title: t.title, Layout: root}
		if p := t.panes[t.focused]; p != nil {
			st.Focused = p.sessionID()
		}
		if p := t.panes[t.zoomed]; p != nil {
			st.Zoomed = p.sessionID()
		}
		d.Tabs = append(d.Tabs, st)
	}
	return d
}

func (t *tab) snapNode(n *layout.Node) *savedNode {
	if n == nil {
		return nil
	}
	if n.A == nil {
		if p := t.panes[n.Pane]; p != nil && p.sessionID() != "" {
			return &savedNode{Session: p.sessionID()}
		}
		return nil
	}
	a, b := t.snapNode(n.A), t.snapNode(n.B)
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	}
	dir := "row"
	if n.Dir == layout.Col {
		dir = "col"
	}
	return &savedNode{Dir: dir, Ratio: n.Ratio, A: a, B: b}
}

// persist runs at the end of every frame: when the snapshot changed, the write is scheduled (debounced, off
// the UI thread).
func (w *workspace) persist() {
	if w.dataDir == "" || !w.saved.ready || w.closed {
		return
	}
	b, err := json.Marshal(w.snapshot())
	if err != nil || bytes.Equal(b, w.saved.last) {
		return
	}
	w.saved.last = b
	s := &w.saved
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = b
	if s.timer == nil {
		s.timer = time.AfterFunc(saveDelay, func() {
			w.work(func() {
				if err := w.flush(); err != nil {
					w.post(func() { w.fail("save workspace", err) })
				}
			})
		})
		return
	}
	s.timer.Reset(saveDelay)
}

// flush writes what persist scheduled, now.
func (w *workspace) flush() error {
	s := &w.saved
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.pending
	s.pending = nil
	if b == nil || w.dataDir == "" {
		return nil
	}
	return writeFileAtomic(w.dataDir, savedFile, b)
}

// saveNow writes the workspace as it is, on window close.
func (w *workspace) saveNow() {
	if w.dataDir == "" || !w.saved.ready {
		return
	}
	b, err := json.Marshal(w.snapshot())
	if err != nil {
		return
	}
	s := &w.saved
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timer != nil {
		s.timer.Stop()
	}
	s.pending = nil
	s.last = b
	writeFileAtomic(w.dataDir, savedFile, b)
}

// loadSaved reads the file; nil for a missing, unreadable, oversized or other-version one.
func loadSaved(dataDir string) *savedDoc {
	if dataDir == "" {
		return nil
	}
	f, err := os.Open(filepath.Join(dataDir, savedFile))
	if err != nil {
		return nil
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, savedMaxSize+1))
	if err != nil || len(b) > savedMaxSize {
		return nil
	}
	var d savedDoc
	if json.Unmarshal(b, &d) != nil || d.V != savedVersion {
		return nil
	}
	return &d
}

// restore makes the tabs from the saved ones and the sessions that exist (panes are attached already, each
// with its session id in sid). Leaves whose session is gone go and the tree closes up as Remove does; a
// tab left with no leaf is dropped; sessions in no saved tab become their own tabs after the saved ones.
// Without a saved workspace, that is every session.
func (w *workspace) restore(doc *savedDoc, panes []*pane) {
	bySID := map[string]*pane{}
	for _, p := range panes {
		bySID[p.sid] = p
	}
	used := map[string]bool{}
	active := -1
	if doc != nil {
		for i, st := range doc.Tabs {
			t := w.buildTab(st, bySID, used)
			if t == nil {
				continue
			}
			if i == doc.Active {
				active = len(w.tabs)
			}
			w.tabs = append(w.tabs, t)
		}
	}
	for _, p := range panes {
		if !used[p.sid] {
			w.tabs = append(w.tabs, &tab{root: layout.Leaf(p.id), panes: map[string]*pane{p.id: p}, focused: p.id})
		}
	}
	var pages []*tab
	if doc != nil {
		for _, name := range doc.Pages {
			if t := w.addPageTab(name); t != nil {
				pages = append(pages, t)
				if name == doc.ActivePage {
					active = len(w.tabs) - 1
				}
			}
		}
	}
	if active < 0 {
		active = len(w.tabs) - 1
	}
	w.selectTab(active) // reads the page when it is a page
	for _, t := range pages {
		if t != w.tab() {
			w.loadPage(t)
		}
	}
}

func (w *workspace) buildTab(st savedTab, bySID map[string]*pane, used map[string]bool) *tab {
	byID := map[string]*pane{}
	root := buildNode(st.Layout, bySID, used, byID, 0)
	if root == nil {
		return nil
	}
	t := &tab{root: root, panes: byID, title: st.Title}
	t.focused = root.Leaves()[0]
	if p := bySID[st.Focused]; p != nil && byID[p.id] == p {
		t.focused = p.id
	}
	if p := bySID[st.Zoomed]; p != nil && byID[p.id] == p && len(byID) > 1 {
		t.zoomed, t.focused = p.id, p.id
	}
	return t
}

func buildNode(n *savedNode, bySID map[string]*pane, used map[string]bool, byID map[string]*pane, depth int) *layout.Node {
	if n == nil || depth > savedDepth {
		return nil
	}
	if n.A == nil && n.B == nil {
		p := bySID[n.Session]
		if p == nil || used[n.Session] {
			return nil
		}
		used[n.Session] = true
		byID[p.id] = p
		return layout.Leaf(p.id)
	}
	a := buildNode(n.A, bySID, used, byID, depth+1)
	b := buildNode(n.B, bySID, used, byID, depth+1)
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	}
	dir, ratio := layout.Row, n.Ratio
	if n.Dir == "col" {
		dir = layout.Col
	}
	if !(ratio > 0 && ratio < 1) {
		ratio = 0.5
	}
	return &layout.Node{Dir: dir, Ratio: ratio, A: a, B: b}
}
