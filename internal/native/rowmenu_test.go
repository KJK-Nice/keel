package native

import (
	"slices"
	"strings"
	"testing"
)

// A "…" button on a row was built only while the row was hovered, so moving the pointer onto its opened menu
// un-hovered the row, dropped the button and closed the menu. Rows have no such button; the right-click menu stays.
func TestAHostRowHasNoMoreButtonAndItsMenuStaysOpenUnderThePointer(t *testing.T) {
	r := hostRig(t)
	rc, _ := r.tt.Find("host api-1")
	r.tt.Move(rc.X+20, rc.Y+rc.H/2)
	r.tt.Frame()
	if _, ok := r.tt.Find("Actions for api-1"); ok {
		t.Fatal("the row still has a … button")
	}
	if err := r.tt.RightClick("host api-1"); err != nil {
		t.Fatal(err)
	}
	if len(r.tt.Menu()) == 0 {
		t.Fatal("the menu did not open")
	}
	ed, ok := r.tt.Find("Edit")
	if !ok {
		t.Fatalf("no Edit item to move onto; menu %v", r.tt.Menu())
	}
	r.tt.Move(ed.X+ed.W/2, ed.Y+ed.H/2)
	r.tt.Frame()
	if len(r.tt.Menu()) == 0 {
		t.Fatal("the menu closed when the pointer moved onto it")
	}
}

func TestEveryMenuEntryHasAnIconAndDeleteIsDestructive(t *testing.T) {
	for label, name := range menuIconFor {
		if _, ok := iconPaths[name]; !ok {
			t.Errorf("%q: no icon %q", label, name)
		}
	}
	var seen []string
	menuItemSeen = func(label, icon string) {
		seen = append(seen, label)
		if icon == "" {
			t.Errorf("menu entry %q has no icon", label)
		}
	}
	t.Cleanup(func() { menuItemSeen = nil })
	r := hostRig(t)
	if err := r.tt.RightClick("host api-1"); err != nil {
		t.Fatal(err)
	}
	want := []string{"Connect", "Open in split", "Edit", "Duplicate", "Move to folder…", "Agent can read", "Delete"}
	for _, w := range want {
		if !slices.Contains(seen, w) {
			t.Errorf("entry %q not built; built %v", w, seen)
		}
	}
	if strings.Join(slices.DeleteFunc(r.tt.Menu(), func(s string) bool { return s == "-" }), ",") == "" {
		t.Error("no menu")
	}
}
