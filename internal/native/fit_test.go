package native

import (
	"fmt"
	"math"
	"testing"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/layout"
)

// splitStep is one "split target in direction d"; the new pane gets the next number.
type splitStep struct {
	target string
	d      layout.Dir
}

const (
	R = layout.Row
	C = layout.Col
)

var fitTrees = []struct {
	name  string
	steps []splitStep
}{
	{"two in a row", []splitStep{{"1", R}}},
	{"three in a row", []splitStep{{"1", R}, {"2", R}}},
	{"four in a row", []splitStep{{"1", R}, {"2", R}, {"3", R}}},
	{"three in a column", []splitStep{{"1", C}, {"2", C}}},
	{"2x2 grid", []splitStep{{"1", R}, {"1", C}, {"2", C}}},
	{"row in column in row", []splitStep{{"1", R}, {"2", C}, {"3", R}, {"4", C}}},
	{"column in row in column", []splitStep{{"1", C}, {"2", R}, {"3", C}, {"4", R}}},
}

// rectOf is the box a subtree fills: the union of its tiles.
func rectOf(n *layout.Node, tiles map[string]ui.Rect) ui.Rect {
	if n.A == nil {
		return tiles[n.Pane]
	}
	a, b := rectOf(n.A, tiles), rectOf(n.B, tiles)
	x0, y0 := min(a.X, b.X), min(a.Y, b.Y)
	x1, y1 := max(a.X+a.W, b.X+b.W), max(a.Y+a.H, b.Y+b.H)
	return ui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// assertFits checks that the tiles of the active tab tile the stage: inside it, apart, and in each split's ratio.
func assertFits(t *testing.T, ws *workspace, tt *ui.Tester, w, h float32) {
	t.Helper()
	sidebar := ws.sidebar
	tb := ws.tabs[ws.active]
	tiles := map[string]ui.Rect{}
	for id := range tb.panes {
		rc, ok := tt.Find("pane " + id)
		if !ok {
			t.Fatalf("pane %s not found", id)
		}
		tiles[id] = rc
	}
	x0, y0, x1, y1 := float32(0), float32(sizeTitlebar), w, h-statusbarSize
	if sidebar {
		x0 = sizeSidebar
	}
	const eps = 1
	for id, rc := range tiles {
		if rc.X < x0-eps || rc.Y < y0-eps || rc.X+rc.W > x1+eps || rc.Y+rc.H > y1+eps {
			t.Errorf("pane %s %v lies outside the stage [%v %v %v %v]", id, rc, x0, y0, x1, y1)
		}
		for id2, r2 := range tiles {
			if id < id2 && rc.X < r2.X+r2.W-eps && r2.X < rc.X+rc.W-eps && rc.Y < r2.Y+r2.H-eps && r2.Y < rc.Y+rc.H-eps {
				t.Errorf("panes %s %v and %s %v overlap", id, rc, id2, r2)
			}
		}
	}
	var walk func(n *layout.Node)
	walk = func(n *layout.Node) {
		if n.A == nil {
			return
		}
		a, b, all := rectOf(n.A, tiles), rectOf(n.B, tiles), rectOf(n, tiles)
		got, total, gap := a.W, all.W-space1_5, b.X-(a.X+a.W)
		if n.Dir == layout.Col {
			got, total, gap = a.H, all.H-space1_5, b.Y-(a.Y+a.H)
		}
		if math.Abs(float64(gap-space1_5)) > 2 {
			t.Errorf("split %v: gap %v, want %v", n.Dir, gap, float32(space1_5))
		}
		if math.Abs(float64(got-n.Ratio*total)) > 2 {
			t.Errorf("split %v: first side %v of %v, want ratio %v", n.Dir, got, total, n.Ratio)
		}
		walk(n.A)
		walk(n.B)
	}
	walk(tb.root)
	if all := rectOf(tb.root, tiles); math.Abs(float64(all.X-(x0+space1_5))) > 2 || math.Abs(float64(all.X+all.W-(x1-space1_5))) > 2 ||
		math.Abs(float64(all.Y-(y0+space1_5))) > 2 || math.Abs(float64(all.Y+all.H-(y1-space1_5))) > 2 {
		t.Errorf("tiles fill %v, want the stage inside a %v inset [%v %v %v %v]", all, float32(space1_5), x0, y0, x1, y1)
	}
}

func TestSplitsTileTheStage(t *testing.T) {
	for _, sz := range [][2]float32{{1280, 800}, {900, 600}} {
		for _, sidebar := range []bool{true, false} {
			for _, tc := range fitTrees {
				alpha := float32(1)
				if sidebar && sz[0] == 900 { // the translucent stage is built differently: cover it once
					alpha = windowOpacity
				}
				t.Run(fmt.Sprintf("%s %vx%v sidebar=%v alpha=%v", tc.name, sz[0], sz[1], sidebar, alpha), func(t *testing.T) {
					r := newRig(t, okBackend())
					r.w.alpha = alpha
					r.tt = ui.NewTester(r.w.view, int(sz[0]), int(sz[1]))
					r.tt.Frame()
					r.click(t, "host api-1")
					tb := r.w.tabAt(0)
					for _, s := range tc.steps {
						r.w.split(tb, s.target, s.d)
						r.tt.Frame()
					}
					if !sidebar {
						r.w.sidebar = false
						r.tt.Frame()
					}
					r.tt.Frame()
					assertFits(t, r.w, r.tt, sz[0], sz[1])
				})
			}
		}
	}
}

func TestDraggingANestedGapChangesOnlyThatSplit(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.click(t, "host api-1")
	tb := r.w.tabAt(0)
	r.w.split(tb, "1", R)
	r.w.split(tb, "2", R)
	r.tt.Frame()
	outer, inner := tb.root, tb.root.B
	if inner.A == nil {
		t.Fatalf("want the second split nested in the right side")
	}
	before := outer.Ratio
	p2, _ := r.tt.Find("pane 2") // the inner gap lies right of tile 2, in the middle of its height
	x, y := p2.X+p2.W+space1_5/2, p2.Y+p2.H/2
	r.tt.Press(x, y)
	r.tt.Move(x+40, y)
	r.tt.Move(x+40, y)
	r.tt.Release(x+40, y)
	r.tt.Frame()
	if outer.Ratio != before || inner.Ratio <= 0.5 {
		t.Errorf("outer ratio %v (was %v), inner %v after dragging the inner gap right", outer.Ratio, before, inner.Ratio)
	}
}
