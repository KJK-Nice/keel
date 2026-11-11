package native

import (
	"github.com/egoist/mygo/ui"

	"keel/internal/native/layout"
)

// Rearranging the panes of a tab by dragging a pane bar (ui/drag.js, lib/workspace.js movePane): a pane dragged by its bar
// and dropped on an edge of another pane of the same tab moves next to it, on the centre swaps the two. The tab leaves focus
// view and the moved pane has the focus. Nothing here opens, closes or types into a session. Not built from the app: a pane
// dropped on the tab strip becoming a tab of its own, and a whole tab docked beside a pane (extractPane and dockTab).

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
	if moved, ok := ui.Drop[*pane](tl); ok && moved != p && t.panes[moved.id] == moved {
		x, y, _ := tl.PointerPosition()
		if b := tl.Bounds(); b.W > 0 && b.H > 0 {
			edge := dropEdge(x/b.W, y/b.H)
			w.act(func() { w.movePane(t, moved.id, p.id, edge) })
		}
		return
	}
	from, over := ui.DragOver[*pane](tl)
	if !over || from == p || t.panes[from.id] != from {
		return
	}
	x, y, _ := tl.PointerPosition()
	b := tl.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	i, half := float32(paneDropInset), b.W/2
	box := ui.Box(c).Absolute().Radius(radiusSm).Background(brassInk.Alpha(paneDropAlpha)).Border(2, brassInk).Label("pane drop region")
	switch dropEdge(x/b.W, y/b.H) {
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
