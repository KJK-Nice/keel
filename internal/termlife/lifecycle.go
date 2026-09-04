// Package termlife is the attach/detach/remount contract for native WebGL
// panes. It mirrors cmd/keel/web/js/term.js (paneWantsWebgl, attachWebgl,
// dropWebgl, ensurePaneTerm, paintLayout, disposePane) so tests can catch
// remount bugs without scanning source strings.
package termlife

type Node struct {
	Kind   string
	ID     string
	Parent *Node
	Kids   []*Node
}

func (n *Node) Append(child *Node) {
	if child.Parent != nil {
		child.Parent.detach(child)
	}
	child.Parent = n
	n.Kids = append(n.Kids, child)
}

func (n *Node) RemoveSelf() {
	if n.Parent != nil {
		n.Parent.detach(n)
	}
}

func (n *Node) detach(child *Node) {
	out := n.Kids[:0]
	for _, k := range n.Kids {
		if k != child {
			out = append(out, k)
		}
	}
	n.Kids = out
	child.Parent = nil
}

type Addon struct {
	ID       int
	Disposed int
}

func (a *Addon) Dispose() {
	a.Disposed++
}

type Term struct {
	ID        int
	Element   bool
	OpenCount int
	Host      *Node
	WebGL     *Addon
	Disposed  int
	Loads     int
}

func (t *Term) Open(host *Node) {
	t.Element = true
	t.OpenCount++
	t.Host = host
}

func (t *Term) LoadAddon(a *Addon) {
	t.Loads++
	t.WebGL = a
}

func (t *Term) Dispose() {
	t.Disposed++
}

type Pane struct {
	ID   string
	Term *Term
	Host *Node
	Wrap *Node
}

type Tab struct {
	Native  bool
	Stage   *Node
	Panes   []*Pane
	Visible []*Pane
	Log     []string
	addons  int
}

func (t *Tab) note(op string) {
	t.Log = append(t.Log, op)
}

func NewPane(id string) *Pane {
	wrap := &Node{Kind: "wrap", ID: id}
	host := &Node{Kind: "host", ID: id + "-host"}
	wrap.Append(host)
	return &Pane{
		ID:   id,
		Term: &Term{},
		Host: host,
		Wrap: wrap,
	}
}

func NewTab(native bool, panes ...*Pane) *Tab {
	tab := &Tab{Native: native, Stage: &Node{Kind: "stage", ID: "stage"}}
	for _, p := range panes {
		tab.Panes = append(tab.Panes, p)
		tab.Visible = append(tab.Visible, p)
	}
	return tab
}

func (t *Tab) SetVisible(ids ...string) {
	t.Visible = t.Visible[:0]
	for _, id := range ids {
		for _, p := range t.Panes {
			if p.ID == id {
				t.Visible = append(t.Visible, p)
			}
		}
	}
}

func WantsWebGL(tab *Tab, pane *Pane) bool {
	if pane == nil || pane.Term == nil {
		return false
	}
	if !tab.Native {
		return true
	}
	return len(tab.Visible) <= 1
}

func AttachWebGL(tab *Tab, pane *Pane) {
	term := pane.Term
	if term == nil || term.WebGL != nil || !WantsWebGL(tab, pane) {
		return
	}
	tab.addons++
	addon := &Addon{ID: tab.addons}
	term.LoadAddon(addon)
	tab.note("attach:" + pane.ID)
}

func DropWebGL(tab *Tab, pane *Pane) {
	if pane == nil || pane.Term == nil || pane.Term.WebGL == nil {
		return
	}
	pane.Term.WebGL.Dispose()
	pane.Term.WebGL = nil
	tab.note("drop:" + pane.ID)
}

func EnsurePaneTerm(tab *Tab, pane *Pane) {
	if pane == nil || pane.Term == nil || pane.Wrap == nil || pane.Wrap.Parent == nil {
		return
	}
	if !pane.Term.Element {
		if pane.Host == nil {
			return
		}
		pane.Term.Open(pane.Host)
		tab.note("open:" + pane.ID)
	}
	if WantsWebGL(tab, pane) {
		AttachWebGL(tab, pane)
	} else {
		DropWebGL(tab, pane)
	}
}

func PaintLayout(tab *Tab) {
	if tab.Stage == nil {
		tab.Stage = &Node{Kind: "stage", ID: "stage"}
		tab.note("ensure-stage")
	}
	if tab.Native && len(tab.Visible) > 1 {
		for _, pane := range tab.Panes {
			DropWebGL(tab, pane)
		}
	}
	for _, pane := range tab.Panes {
		if pane.Wrap != nil && pane.Wrap.Parent != tab.Stage {
			tab.Stage.Append(pane.Wrap)
			tab.note("reparent:" + pane.ID)
		}
	}
	tab.note("layout")
	for _, pane := range tab.Visible {
		EnsurePaneTerm(tab, pane)
	}
}

func DisposePane(tab *Tab, pane *Pane) {
	DropWebGL(tab, pane)
	if pane.Term != nil {
		pane.Term.Dispose()
		tab.note("dispose-term:" + pane.ID)
	}
	if pane.Wrap != nil {
		pane.Wrap.RemoveSelf()
		tab.note("remove-wrap:" + pane.ID)
	}
}

func ContextLost(tab *Tab, pane *Pane) {
	if pane == nil || pane.Term == nil || pane.Term.WebGL == nil {
		return
	}
	pane.Term.WebGL.Dispose()
	pane.Term.WebGL = nil
	tab.note("context-lost:" + pane.ID)
	if WantsWebGL(tab, pane) {
		AttachWebGL(tab, pane)
	}
}
