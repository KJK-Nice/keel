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
