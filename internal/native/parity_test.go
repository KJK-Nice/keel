package native

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The differences with the app that the comparison in docs/native.md ("Compared with the app") marks fixed.

func TestTitleBarShowsTheBarStatusAndTheStatusBarDoesNot(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.click(t, "host api-1")
	// A session: the pane bar's line and whose hands are on it (term.js renderTabs → #bar-status), at the right of the
	// title bar.
	pane := r.w.tab().panes["1"]
	pane.conn.(*fakeConn).st = &keelclient.State{Status: "ready", Subtitle: "vm", Role: "driver"}
	r.tt.Frame()
	bar, ok := r.tt.Find("vm · driving")
	// At the right of the title bar, before the Share and Agent buttons: how far right depends on the width of their key caps
	// ("⌘I" on macOS, "Ctrl+⇧I" on Linux: 835 there, not 900), so the test asks for the right half and for the buttons' side.
	share, _ := r.tt.Find("Share")
	if !ok || bar.Y+bar.H > sizeTitlebar || bar.X < 640 || bar.X+bar.W > share.X {
		t.Errorf("bar status 'vm · driving' at %v (%v): %v", bar, ok, r.tt.Texts())
	}
	pane.conn.(*fakeConn).st = &keelclient.State{Status: "ready", Role: "follower"}
	r.tt.Frame()
	if !r.tt.HasText("deploy@10.0.0.1 · following") {
		t.Errorf("a follower's bar status: %v", r.tt.Texts())
	}
	r.click(t, "New tab")
	if got := r.w.barStatus(); got != "New tab" {
		t.Errorf("bar status %q on the New tab page", got)
	}
	if tx := r.tt.Texts(); len(tx) == 0 || !strings.HasPrefix(tx[len(tx)-1], "keel serve") { // the status bar is last, and has no "New tab"
		t.Errorf("the status bar ends with %v", tx[len(tx)-3:])
	}
	r.w.openPage("settings")
	if got := r.w.barStatus(); got != "" {
		t.Errorf("a page has no bar status, got %q", got)
	}
}

func TestSidebarHeadAndFootAreTheApps(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	nh, _ := r.tt.Find("New host")
	more, ok := r.tt.Find("More host actions")
	if !ok || nh.H != sizeControlSm || more.H != sizeControlSm {
		t.Fatalf("New host %v and More host actions %v (%v): both are k-btn--sm icon buttons", nh, more, ok)
	}
	if f, _ := r.tt.Find("Filter hosts"); f.Y < sizeTitlebar+space3 {
		t.Errorf("the filter is %v px under the title bar, want at least the head's 12", f.Y-sizeTitlebar)
	}
	if err := r.tt.Click("More host actions"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	for _, want := range []string{"Quick connect", "Local terminal"} {
		found := false
		for _, it := range r.tt.Menu() {
			found = found || it == want
		}
		if !found {
			t.Errorf("the menu has no %q: %v", want, r.tt.Menu())
		}
	}
	if err := r.tt.ChooseMenuItem("Quick connect"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if r.w.quick == nil {
		t.Error("Quick connect in the menu did not open the dialog")
	}
	r.w.quick = nil
	r.tt.Frame()
	// The Settings button is the gear alone: the "," key cap shows only while ⌘ is held in the app.
	if set, ok := r.tt.Find("Settings"); !ok || set.W != sizeControlSm {
		t.Errorf("the Settings button is %v (%v), want the 24 px icon button", set, ok)
	}
}

func TestHostsSegmentIsPressedAtStartWithTheLineRing(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	h, _ := r.tt.Find("Hosts")
	s, _ := r.tt.Find("Snippets")
	// Plan 030: the section switch is MyGo's Segmented: the chosen segment has a face of its own, the others do not.
	chosen, other := r.pixel(h.X+h.W/2, h.Y+3), r.pixel(s.X+s.W/2, s.Y+3)
	if chosen == other {
		t.Errorf("Hosts is %v and Snippets %v: Hosts is the default section and is pressed", chosen, other)
	}
	r.click(t, "Snippets")
	if got := r.pixel(h.X+h.W/2, h.Y+3); got == chosen {
		t.Error("Hosts stays pressed after Snippets is chosen")
	}
}

func TestTabCountIsAMonoBadge(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.click(t, "host api-1")
	r.click(t, "Split right")
	tab, _ := r.tt.Find("tab api-1")
	// k-badge--mono tab-count: the hover ground behind the number, at the right of the pill, before the close button
	// (16 px and a gap, plan 025).
	// The sample column runs down the badge's left side: a glyph of the count, which differs with the font (on Linux a pixel of
	// the digit, [45 45 49] over the ground), can cover one row of it but not all.
	x, top := tab.X+tab.W-space2-space4-space1_5-2, tab.Y+tab.H/2-rem(tabCountPx)/2+1
	found := false
	var got [3]uint8
	for dy := float32(0); dy < rem(tabCountPx)-2; dy++ {
		if got = r.pixel(x, top+dy); is(got, hoverGround) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("tab count ground %v at the last sample, want hover %v on some row of the badge", got, hoverGround)
	}
}

func TestNewHostStartActionHasThePlusIcon(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.click(t, "New tab")
	for _, name := range []string{"Quick connect", "New host"} {
		if _, ok := r.tt.Find(name); !ok {
			t.Errorf("no start action %q", name)
		}
	}
}

func TestFocusViewToolIsOffForAOnePaneTabAndOnWithTwo(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.click(t, "host api-1")
	icon1, _ := r.tt.Find("Focus view")
	dim := r.inkSum(icon1)
	r.click(t, "Split right")
	icon2, _ := r.tt.Find("Focus view")
	if bright := r.inkSum(icon2); bright < dim*3/2 { // plan 030: MyGo's disabled button dims by its own amount
		t.Errorf("the tool draws %d of ink with one pane and %d with two: a disabled button is dimmed", dim, bright)
	}
}

// inkSum adds up how far the pixels of rc are from its corner's ground: a dim icon has less.
func (r *rig) inkSum(rc ui.Rect) (n int) {
	bg := r.pixel(rc.X+0.5, rc.Y+0.5)
	abs := func(a, b uint8) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	for y := rc.Y + 2; y < rc.Y+rc.H-2; y++ {
		for x := rc.X + 2; x < rc.X+rc.W-2; x++ {
			p := r.pixel(x, y)
			n += abs(p[0], bg[0]) + abs(p[1], bg[1]) + abs(p[2], bg[2])
		}
	}
	return n
}
