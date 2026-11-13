package native

import (
	"slices"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/layout"
)

// Rearranging the panes of a tab by dragging a pane bar (ui/drag.js, lib/workspace.js movePane): a pane dragged by its bar
// and dropped on an edge of another pane of the same tab moves next to it, on the centre swaps the two. The tab leaves focus
// view and the moved pane has the focus. A pane dropped on the tab strip becomes a tab of its own there (extractPane, term.js
// extractPaneToTab), and a whole tab dropped on a pane of another tab docks beside it (dockTab, term.js dockTabIntoPane), the
// docked tab's sync flag kept if either had it. Nothing here opens, closes or types into a session: a pane keeps its
// connection wherever it goes.

// dragged is what is dragged to the tab strip and to a pane: a tab (by its pill) or a pane (by its bar). The engine lets an
// element take one type of value, and an interface is one type.
type dragged interface{ dragged() }

func (*tab) dragged()  {}
func (*pane) dragged() {}

// canDock is a tab that may be dropped on a pane of dest: two different session tabs of this machine's keel serve (not the New
// tab page, a page or a joined tab).
func canDock(src, dest *tab) bool {
	return src != nil && dest != nil && src != dest && src.root != nil && dest.root != nil && !src.remote && !dest.remote && src.page == nil && dest.page == nil && src.pg == nil && dest.pg == nil
}

// dockTab moves every pane of src beside pane target of another tab, on edge ("center" docks to the right). The tab leaves the
// strip without closing a session, focus view is off in both, the focus the source had stays, and the destination has sync on
// if either had it. The destination is shown.
func (w *workspace) dockTab(src *tab, target, edge string) {
	var dest *tab
	for _, t := range w.tabs {
		if t.panes[target] != nil {
			dest = t
		}
	}
	if !canDock(src, dest) || !slices.Contains(w.tabs, src) {
		return
	}
	root, ok := layout.Dock(dest.root, src.root, target, layout.Edge(edge))
	if !ok {
		return
	}
	dest.root = root
	for id, p := range src.panes {
		dest.panes[id] = p
	}
	dest.zoomed, src.zoomed = "", ""
	if src.panes[src.focused] != nil {
		dest.focused = src.focused
	} else {
		dest.focused = target
	}
	dest.sync = dest.sync || src.sync
	src.panes = map[string]*pane{} // the tab goes; the panes are the destination's now, and closing the tab kills nothing
	w.removeTab(src)
	if i := slices.Index(w.tabs, dest); i >= 0 {
		w.selectTab(i)
	}
	w.wantFocus = dest.focused
	w.publishSync()
}

// extractPane makes pane id of t a tab of its own at slot of the strip (counted as the strip is now), and shows it. A tab of
// one pane, a joined tab and a pane that is not in t stay as they are.
func (w *workspace) extractPane(t *tab, id string, slot int) {
	p := t.panes[id]
	if p == nil || len(t.panes) < 2 || t.remote || t.root == nil || !slices.Contains(w.tabs, t) {
		return
	}
	t.root = t.root.Remove(id)
	delete(t.panes, id)
	t.zoomed = ""
	if t.focused == id {
		t.focused = t.root.Leaves()[0]
	}
	nt := &tab{root: layout.Leaf(id), panes: map[string]*pane{id: p}, focused: id}
	slot = min(max(slot, 0), len(w.tabs))
	w.tabs = slices.Insert(w.tabs, slot, nt)
	w.selectTab(slot)
	w.publishSync()
}

// paneDropInset is .pane-drop-region's inset: 6 px from the pane's edge.
const paneDropInset = space1_5

// paneDropCentre is how far in a point must be, as a share of the pane, to be "centre" (drag.js dropEdgeForRect: 0.28).
const paneDropCentre = 0.28

// dropEdge is drag.js dropEdgeForRect for a point at relX, relY (0 to 1) of a pane: the nearest edge, or "center" when the
// point is well inside.
func dropEdge(relX, relY float32) string {
	edge, best := "left", relX
	for _, c := range []struct {
		name string
		d    float32
	}{{"right", 1 - relX}, {"top", relY}, {"bottom", 1 - relY}} {
		if c.d < best {
			edge, best = c.name, c.d
		}
	}
	if best > paneDropCentre {
		return "center"
	}
	return edge
}

// movePane is the drop: from lands on an edge of to (or swaps with it at the centre). Both must be panes of t.
func (w *workspace) movePane(t *tab, from, to string, edge string) {
	if from == to || t.panes[from] == nil || t.panes[to] == nil || t.root == nil {
		return
	}
	if edge == "center" {
		t.root.Swap(from, to)
	} else {
		root, ok := layout.Beside(t.root, from, to, layout.Edge(edge))
		if !ok {
			return
		}
		t.root = root
	}
	t.zoomed = ""
	t.focused = from
	if t == w.tab() {
		w.wantFocus = from
	}
}

// paneDropZone is the target side of the drag: while a pane of this tab is dragged over tile p, the region it would take is
// drawn (.pane-drop-region), and on the drop the pane moves. tl is the tile's element and r its box.
func (w *workspace) paneDropZone(c *ui.Context, t *tab, p *pane, tl ui.Element) {
	// An element takes one type of dragged value, so a pane and a tab are taken as a dragged and told apart here. A pane of this
	// tab moves; a tab of another dropped here docks beside this pane (the centre docks to the right).
	drop, dropped := ui.Drop[dragged](tl)
	switch m := drop.(type) {
	case *pane:
		if dropped && m != p && t.panes[m.id] == m {
			if b := tl.Bounds(); b.W > 0 && b.H > 0 {
				x, y, _ := tl.PointerPosition()
				edge := dropEdge(x/b.W, y/b.H)
				w.act(func() { w.movePane(t, m.id, p.id, edge) })
			}
			return
		}
	case *tab:
		if dropped && canDock(m, t) {
			if b := tl.Bounds(); b.W > 0 && b.H > 0 {
				x, y, _ := tl.PointerPosition()
				edge := dropEdge(x/b.W, y/b.H)
				w.act(func() { w.dockTab(m, p.id, edge) })
			}
			return
		}
	}
	var over, dock bool
	if v, ok := ui.DragOver[dragged](tl); ok {
		switch m := v.(type) {
		case *pane:
			over = m != p && t.panes[m.id] == m
		case *tab:
			dock = canDock(m, t)
		}
	}
	if !over && !dock {
		return
	}
	x, y, _ := tl.PointerPosition()
	b := tl.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	i, half := float32(paneDropInset), b.W/2
	box := ui.Box(c).Absolute().Radius(radiusSm).Background(accentInk.Alpha(paneDropAlpha)).Border(2, accentInk).Label("pane drop region")
	edge := dropEdge(x/b.W, y/b.H)
	if dock && edge == "center" {
		edge = "right"
	}
	switch edge {
	case "left":
		box.Top(i).Bottom(i).Left(i).Right(half)
	case "right":
		box.Top(i).Bottom(i).Left(half).Right(i)
	case "top":
		box.Left(i).Right(i).Top(i).Bottom(b.H / 2)
	case "bottom":
		box.Left(i).Right(i).Top(b.H / 2).Bottom(i)
	default:
		box.Left(b.W * 0.14).Right(b.W * 0.14).Top(b.H * 0.14).Bottom(b.H * 0.14)
	}
}
