package native

import (
	"testing"
)

// The Agent page's composer ("Message the agent on the page") has a visible area in every layout, with the agent panel open
// beside the page or not, and the page never pushes the panel out of the window (v0.2.1: a MinWidth(0) on the page root made the
// composer collapse beside the open panel; before it, in a wider font the page pushed the panel out, issue #140).
func TestTheAgentPageComposerIsVisibleAtEveryWidthWithThePanelOpenOrNot(t *testing.T) {
	for _, width := range []int{1280, 1100, 850} {
		for _, panel := range []bool{false, true} {
			r := savedRig(t)
			r.w.setAgentOpen(false)
			r.tt.SetSize(width, 720)
			r.openPageSettled()
			r.w.setAgentOpen(panel)
			r.settle()
			page, ok := r.tt.Find("agent page")
			if !ok {
				t.Fatalf("width %v panel %v: no page", width, panel)
			}
			comp, ok := r.tt.Find("Message the agent on the page")
			t.Logf("width %v panel %v: page %v composer %v %v", width, panel, page, ok, comp)
			// Room: 100 px where the page has 400 or more (beside the panel at 850 px the page itself is 218 px wide).
			min := float32(1)
			if page.W >= 400 {
				min = 100
			}
			if !ok || comp.W < min || comp.H < 20 || comp.X < page.X || comp.X+comp.W > page.X+page.W+0.5 {
				t.Errorf("width %v panel %v: the composer has no visible area inside the page: %v (page %v)", width, panel, comp, page)
			}
			if panel {
				p, ok := r.tt.Find("agent panel")
				if !ok || p.X+p.W > float32(width)+0.5 || p.X < page.X+page.W-0.5 {
					t.Errorf("width %v: the panel %v is not beside the page %v inside the window", width, p, page)
				}
			}
		}
	}
}
