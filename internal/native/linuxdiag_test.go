package native

import (
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Issue #140 (plan 037 part B, round 1): what would explain a layout difference between macOS and Linux, logged when a test
// fails (and only then), so the Ubuntu job's log answers "what differs" without another round. Not product code: it asks
// nothing of the product but what the tests already read.

// probeWidth is the width a string takes at size in family, measured in a Tester of its own ("" is the theme's default family).
func probeWidth(s string, size float32, family string) float32 {
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().AlignItems(ui.Start).Children(func() {
			e := ui.Text(c, s).Label("probe").FontSize(size).TextColor(ui.Color{R: 255, G: 255, B: 255, A: 255}).MaxLines(1)
			if family != "" {
				e.Font(family)
			}
		})
	}, 1600, 200)
	tt.Frame()
	r, ok := tt.Find("probe")
	if !ok {
		return -1
	}
	return r.W
}

// The probe itself works where the text engine does (macOS): a measurable width, and wider in a larger size. Elsewhere it only
// logs, so the diagnostic cannot add a failure of its own.
func TestTheDiagnosticProbeMeasuresText(t *testing.T) {
	small, large := probeWidth("Hosts Panes", 12, ""), probeWidth("Hosts Panes", 24, "")
	t.Logf("linux-diag: probe widths %v and %v", small, large)
	if runtime.GOOS == "darwin" && (small <= 0 || large <= small) {
		t.Errorf("probe widths %v and %v", small, large)
	}
}

// diagFonts logs the text facts: the measured width of a fixed string in the default, the bundled and a missing family, at
// the UI size. Equal widths in the bundled and missing families mean the bundled one is not in use; a zero width means the
// text engine drew nothing (MyGo's Linux engine, Pango through purego, falls back to a stub when its libraries are missing).
func diagFonts(t *testing.T) {
	t.Helper()
	const probe = "Hosts Panes Snippets Forwards"
	t.Logf("linux-diag: GOOS=%s GOARCH=%s uiFontSize=%v", runtime.GOOS, runtime.GOARCH, uiFontSize)
	for _, f := range []struct{ name, family string }{{"default", ""}, {"bundled mono (" + monoFamily + ", not registered here)", monoFamily}, {"monospace", "monospace"}, {"missing family", "keel-test-missing-5d1e"}} {
		t.Logf("linux-diag: %q at the UI size in %s is %v wide", probe, f.name, probeWidth(probe, uiFontSize, f.family))
	}
}

// diagWindow is diagFonts and the window's own measures: the sidebar's width, the rects of the chrome that decides how wide the
// page is, and the texts the window shows. Use it in a test with `defer diagWindow(t, r)`-style registration (t.Cleanup).
func diagWindow(t *testing.T, r *rig) {
	t.Helper()
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		diagFonts(t)
		if r == nil || r.tt == nil {
			return
		}
		t.Logf("linux-diag: sidebar width %v, sidebar shown %v", r.w.sidebarNow(), r.w.sidebar)
		for _, l := range []string{"Toggle sidebar", "Agent", "sidebar hosts", "pane 1", "agent page", "start page", "Resize sidebar"} {
			if rc, ok := r.tt.Find(l); ok {
				t.Logf("linux-diag: %q at x=%.1f y=%.1f w=%.1f h=%.1f", l, rc.X, rc.Y, rc.W, rc.H)
			}
		}
		texts := r.tt.Texts()
		if len(texts) > 40 {
			texts = texts[:40]
		}
		t.Logf("linux-diag: first texts: %s", strings.Join(texts, " | "))
	})
}
