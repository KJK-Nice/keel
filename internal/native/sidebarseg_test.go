package native

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// Plan 031 step 1: the four section labels (Hosts, Panes with its count, Snippets, Forwards) show whole at the default
// sidebar width and at every UI size, in both appearances. A label is whole when its segment, less its padding and
// border, is as wide as the text measured at the theme's font size.
func TestSidebarSegmentLabelsFitAtEveryUISize(t *testing.T) { sidebarSegmentsFit(t, false) }

// ... and at the narrowest the sash (sash.go sidebarMin) lets the sidebar be.
func TestSidebarSegmentLabelsFitAtTheNarrowestSidebar(t *testing.T) { sidebarSegmentsFit(t, true) }

func sidebarSegmentsFit(t *testing.T, narrow bool) {
	defer setUISize(uiSizeDefault)
	for _, dark := range []bool{false, true} {
		for size := uiSizeMin; size <= uiSizeMax; size++ {
			r := splitRig(t) // two panes, so Panes shows
			setUISize(size)
			if narrow {
				r.w.prefs.SidebarWidth =sidebarMin()
			}
			r.w.setTheme(map[bool]string{true: themeDark, false: themeLight}[dark])
			widths := map[string]float32{}
			r.tt = ui.NewTester(func(c *ui.Context) {
				r.w.view(c)
				for _, s := range []string{"Hosts", "Panes", "Snippets", "Forwards"} {
					w, _ := c.MeasureText(0, ui.Span{Text: s, Size: captionSize})
					widths[s] = w
				}
			}, 1280, 720)
			setUISize(size)
			r.w.prefs.UISize = size
			r.tt.Frame()
			r.tt.Frame()
			for _, c := range []struct{ seg, word string }{{"Hosts", "Hosts"}, {"Panes 2", "Panes"}, {"Snippets", "Snippets"}, {"Forwards", "Forwards"}} {
				seg, ok := r.tt.Find(c.seg)
				if !ok {
					t.Fatalf("dark %v size %d: no segment %q in %v", dark, size, c.seg, r.tt.Texts())
				}
				room := seg.W - 2*space0_5 - 2 // padding and the selected segment's border
				if room < widths[c.word]-0.5 {
					t.Errorf("dark %v size %d: %q has %.1f px for a %.1f px word", dark, size, c.seg, room, widths[c.word])
				}
			}
			if track, ok := r.tt.Find("Vault section"); !ok || track.W > r.w.sidebarNow() {
				t.Errorf("narrow %v dark %v size %d: the switch %v is wider than the sidebar %v", narrow, dark, size, track, r.w.sidebarNow())
			}
		}
	}
}
