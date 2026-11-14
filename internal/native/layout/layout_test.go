package layout

import (
	"reflect"
	"testing"
)

func TestSplitRightAndDown(t *testing.T) {
	r := Leaf("a").Split("a", "b", Row)
	if r.Dir != Row || r.Ratio != 0.5 || r.A.Pane != "a" || r.B.Pane != "b" {
		t.Fatalf("right: %+v", r)
	}
	d := Leaf("a").Split("a", "b", Col)
	if d.Dir != Col || !reflect.DeepEqual(d.Leaves(), []string{"a", "b"}) {
		t.Fatalf("down: %+v", d)
	}
}

func TestNestedSplitKeepsReadingOrder(t *testing.T) {
	r := Leaf("a").Split("a", "b", Row).Split("a", "c", Col).Split("b", "d", Col)
	if got := r.Leaves(); !reflect.DeepEqual(got, []string{"a", "c", "b", "d"}) {
		t.Fatalf("leaves %v", got)
	}
}

func TestSplitUnknownTargetChangesNothing(t *testing.T) {
	r := Leaf("a").Split("zz", "b", Row)
	if !reflect.DeepEqual(r.Leaves(), []string{"a"}) {
		t.Fatalf("leaves %v", r.Leaves())
	}
}

func TestRemoveCollapsesParent(t *testing.T) {
	r := Leaf("a").Split("a", "b", Row).Split("b", "c", Col)
	r = r.Remove("c")
	if r.A.Pane != "a" || r.B.Pane != "b" || r.B.A != nil {
		t.Fatalf("after removing c: %+v", r)
	}
	r = r.Remove("a")
	if r.Pane != "b" || r.A != nil {
		t.Fatalf("after removing a: %+v", r)
	}
}

func TestRemoveLastLeafIsNil(t *testing.T) {
	if Leaf("a").Remove("a") != nil {
		t.Fatal("want nil")
	}
	if (*Node)(nil).Leaves() != nil {
		t.Fatal("nil tree has no leaves")
	}
}

func TestSwapKeepsEachPlace(t *testing.T) {
	r := Leaf("a").Split("a", "b", Row).Split("b", "c", Col)
	r.B.Ratio = 0.3
	r.Swap("a", "c")
	if got := r.Leaves(); !reflect.DeepEqual(got, []string{"c", "b", "a"}) || r.B.Ratio != 0.3 || r.B.Dir != Col {
		t.Fatalf("after the swap: %v %+v", got, r.B)
	}
	r.Swap("a", "a")
	r.Swap("a", "zz") // an unknown pane renames the other one and nothing else is lost
	if len(r.Leaves()) != 3 {
		t.Fatalf("leaves %v", r.Leaves())
	}
}

func TestBesideMovesAPaneToAnEdgeOfAnother(t *testing.T) {
	three := func() *Node { return Leaf("a").Split("a", "b", Row).Split("b", "c", Col) } // a | (b over c)
	for _, tc := range []struct {
		pane, target string
		edge         Edge
		want         []string
		dir          Dir
		firstIsPane  bool
	}{
		{"a", "c", Right, []string{"b", "c", "a"}, Row, false},
		{"a", "c", Left, []string{"b", "a", "c"}, Row, true},
		{"c", "a", Top, []string{"c", "a", "b"}, Col, true},
		{"c", "a", Bottom, []string{"a", "c", "b"}, Col, false},
	} {
		got, ok := Beside(three(), tc.pane, tc.target, tc.edge)
		if !ok || !reflect.DeepEqual(got.Leaves(), tc.want) {
			t.Errorf("%s %s of %s: ok %v leaves %v, want %v", tc.pane, tc.edge, tc.target, ok, got.Leaves(), tc.want)
			continue
		}
		// The new split is the one that holds the target, at half and in the edge's direction.
		var holder *Node
		var find func(n *Node)
		find = func(n *Node) {
			if n.A != nil {
				if (n.A.Pane == tc.target || n.B.Pane == tc.target) && (n.A.Pane == tc.pane || n.B.Pane == tc.pane) {
					holder = n
				}
				find(n.A)
				find(n.B)
			}
		}
		find(got)
		if holder == nil || holder.Dir != tc.dir || holder.Ratio != 0.5 || (holder.A.Pane == tc.pane) != tc.firstIsPane {
			t.Errorf("%s %s of %s: %+v", tc.pane, tc.edge, tc.target, holder)
		}
	}
	// Nothing to do: the only pane, itself, an unknown pane or edge leave the tree as it is.
	for _, tc := range []struct {
		root         *Node
		pane, target string
		edge         Edge
	}{
		{Leaf("a"), "a", "b", Left}, {three(), "a", "a", Left}, {three(), "a", "zz", Left}, {three(), "zz", "a", Left}, {three(), "a", "b", "middle"},
	} {
		if got, ok := Beside(tc.root, tc.pane, tc.target, tc.edge); ok || !reflect.DeepEqual(got.Leaves(), tc.root.Leaves()) {
			t.Errorf("%+v moved: %v %v", tc, ok, got.Leaves())
		}
	}
}

func TestDockPutsAWholeTreeBesideAPane(t *testing.T) {
	dest := func() *Node { return Leaf("a").Split("a", "b", Row) }
	src := func() *Node { return Leaf("x").Split("x", "y", Col) }
	for _, tc := range []struct {
		edge Edge
		want []string
		dir  Dir
	}{
		{Right, []string{"a", "x", "y", "b"}, Row},
		{"center", []string{"a", "x", "y", "b"}, Row},
		{"", []string{"a", "x", "y", "b"}, Row},
		{Left, []string{"x", "y", "a", "b"}, Row},
		{Top, []string{"x", "y", "a", "b"}, Col},
		{Bottom, []string{"a", "x", "y", "b"}, Col},
	} {
		got, ok := Dock(dest(), src(), "a", tc.edge)
		if !ok || !reflect.DeepEqual(got.Leaves(), tc.want) {
			t.Errorf("%q: ok %v leaves %v, want %v", tc.edge, ok, got.Leaves(), tc.want)
			continue
		}
		if got.A.Dir != tc.dir || got.A.Ratio != 0.5 {
			t.Errorf("%q: the new split is %+v, want direction %v at half", tc.edge, got.A, tc.dir)
		}
	}
	// The tree goes in whole: its own split stays.
	got, _ := Dock(dest(), src(), "b", Right)
	if got.B.B.A == nil || got.B.B.A.Pane != "x" || got.B.B.B.Pane != "y" || got.B.B.Dir != Col {
		t.Errorf("the docked tree was taken apart: %+v", got.B.B)
	}
	// Nothing to do: an unknown target, no tree, a shared pane, an unknown edge.
	for _, tc := range []struct {
		in     *Node
		target string
		edge   Edge
	}{{src(), "zz", Right}, {nil, "a", Right}, {Leaf("b"), "a", Right}, {src(), "a", "middle"}} {
		if got, ok := Dock(dest(), tc.in, tc.target, tc.edge); ok || !reflect.DeepEqual(got.Leaves(), []string{"a", "b"}) {
			t.Errorf("%+v docked: %v %v", tc, ok, got.Leaves())
		}
	}
}

func TestClampRatio(t *testing.T) {
	if got := ClampRatio(0.01, 1000, 240); got != 0.24 {
		t.Errorf("low = %v", got)
	}
	if got := ClampRatio(0.99, 1000, 240); got != 1-0.24 {
		t.Errorf("high = %v", got)
	}
	if got := ClampRatio(0.4, 1000, 240); got != 0.4 {
		t.Errorf("inside = %v", got)
	}
	if got := ClampRatio(0.9, 400, 240); got != 0.5 {
		t.Errorf("tight = %v, want 0.5", got)
	}
}
