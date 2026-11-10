package native

import (
	"math"
	"slices"
	"strings"
	"testing"
)

// UI size scales what the app writes in rem (text, icons, the kbd cap, the tab's widest, the palette) and nothing in px
// (the title bar, a control, the sidebar, the spacing): keel.css and tokens.css.
func TestUISizeScalesRemSizesAndNotPxSizes(t *testing.T) {
	r := settingsRig(t, settingsBackend())
	p := r.w.pageTab("settings").pg.settings
	type measure struct{ tab, newHost, sidebarEdge, status, kbdH, text float32 }
	read := func(size int) measure {
		p.uiV = float64(size)
		r.tt.Frame()
		r.tt.Frame()
		if r.w.win.UISize != size {
			t.Fatalf("UI size %d after moving the slider to %d", r.w.win.UISize, size)
		}
		tab, _ := r.tt.Find("tab Settings")
		nh, _ := r.tt.Find("More host actions") // the last control of the sidebar's head, 8 px from its edge
		st, _ := r.tt.Find("keel serve · 0 sessions")
		foot, _ := r.tt.Find("Settings") // the sidebar footer's button
		return measure{tab: tab.H, newHost: nh.H, sidebarEdge: nh.X + nh.W + space2 + 1, status: st.H, kbdH: foot.H, text: uiFontSize}
	}
	small, large := read(uiSizeMin), read(uiSizeMax)
	// In px: unchanged.
	if small.tab != sizeTab || large.tab != sizeTab {
		t.Errorf("tab height %v and %v, want %v at both sizes", small.tab, large.tab, float32(sizeTab))
	}
	if small.newHost != sizeControlSm || large.newHost != sizeControlSm {
		t.Errorf("control height %v and %v, want %v", small.newHost, large.newHost, float32(sizeControlSm))
	}
	if small.sidebarEdge != sizeSidebar || large.sidebarEdge != sizeSidebar {
		t.Errorf("sidebar width %v and %v, want %v", small.sidebarEdge, large.sidebarEdge, float32(sizeSidebar))
	}
	// In rem: value × scale. The body is the UI size itself; a caption is 12/13 of it.
	for _, c := range []struct {
		n    int
		got  float32
		want float32
	}{{uiSizeMin, small.text, 11}, {uiSizeMax, large.text, 16}} {
		if math.Abs(float64(c.got-c.want)) > 0.001 {
			t.Errorf("body text at UI size %d is %v px, want %v", c.n, c.got, c.want)
		}
	}
	if got := captionSize; math.Abs(float64(got-12*16/13.0)) > 0.001 {
		t.Errorf("caption %v, want %v", got, 12*16/13.0)
	}
	if ratio := large.status / small.status; ratio < 16.0/11*0.93 || ratio > 16.0/11*1.07 {
		t.Errorf("the status text grew %vx, want about %v", ratio, 16.0/11)
	}
	if iconSize != rem(iconPx) || dotSize != rem(dotPx) || kbdBox != rem(kbdBoxPx) || tabMaxWidth != rem(tabMaxPx) || paletteWidth != rem(paletteWidthPx) {
		t.Error("an icon, dot, key cap, tab or palette size is not on the scale")
	}
	// The app's default is 12 px; its bounds are 11 and 16.
	setUISize(uiSizeDefault)
	if uiFontSize != 12 || uiScale(uiSizeDefault) != 12.0/13 || uiSizeMin != 11 || uiSizeMax != 16 {
		t.Errorf("default body %v, scale %v", uiFontSize, uiScale(uiSizeDefault))
	}
	// Out of range or unreadable values fall back to the default; the choice is kept.
	if got := loadPrefs(t.TempDir()).UISize; got != uiSizeDefault {
		t.Errorf("default %d", got)
	}
	if string(r.prefsFile(t)["uiSize"]) != "16" {
		t.Errorf("file has %s", r.prefsFile(t)["uiSize"])
	}
	if loadPrefs(r.w.dataDir).UISize != uiSizeMax {
		t.Error("the next window does not start at the saved size")
	}
}

func TestUISizeLabelsAndWordsAreTheApps(t *testing.T) {
	r := settingsRig(t, settingsBackend())
	r.texts(t, "UI size", "Tabs, settings, and chrome. 11–16px. Terminal size is separate.")
	if !r.has("UI size slider") {
		t.Error("no UI size slider")
	}
}

func TestTerminalFontRowListsTheAppsFiveAndAppliesToOpenPanes(t *testing.T) {
	r := settingsRig(t, settingsBackend())
	r.w.openHost(testHosts[1]) // a pane to change live
	r.w.openPage("settings")
	r.tt.Frame()
	r.section(t, "Terminal")
	var sizes []float32
	var fams []string
	for _, tb := range r.w.tabs {
		for _, p := range tb.panes {
			p.setFont = func(s float32) { sizes = append(sizes, s); fams = append(fams, p.fontFamily) }
		}
	}
	r.click(t, "Terminal font picker")
	for _, label := range []string{"JetBrains Mono", "IBM Plex Mono", "Menlo / SF Mono", "Consolas", "System mono", "Ships with Keel", "macOS", "Windows", "The system's monospace font"} {
		if !slices.Contains(r.tt.Menu(), label) && !r.tt.HasText(label) {
			t.Errorf("the font menu has no %q: %v", label, r.tt.Menu())
		}
	}
	if err := r.tt.ChooseMenuItem("IBM Plex Mono"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if r.w.win.TermFont != fontPlex || len(sizes) != 1 || !strings.HasPrefix(fams[0], plexFamily) {
		t.Fatalf("font %q, pane calls %v %v", r.w.win.TermFont, sizes, fams)
	}
	if string(r.prefsFile(t)["termFont"]) != `"ibmplex"` || loadPrefs(r.w.dataDir).TermFont != fontPlex {
		t.Errorf("file %v", r.prefsFile(t))
	}
	r.w.openHost(testHosts[1]) // a pane opened now starts in it
	if got := r.w.focusedPane(); !strings.HasPrefix(got.fontFamily, plexFamily) {
		t.Errorf("a new pane has %q", got.fontFamily)
	}
	if validTermFont("comic") != fontJetBrains || loadPrefs(t.TempDir()).TermFont != fontJetBrains {
		t.Error("JetBrains Mono is the default and the answer to anything else")
	}
}
