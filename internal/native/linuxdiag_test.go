package native

import (
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Issue #140: what would explain a layout difference between macOS and Linux, logged when a test
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

// TestDiagRegisteredFontMetrics (issue #140) registers each shipped woff2 file by itself under a private family and logs
// what MyGo measures and draws: the width of ten Ms at the UI size (a 600-unit mono advance gives 7.8 px each at 13 px), the
// number of lit pixels of a render, and what ui.FontFamilies says of the family. It only logs: on Ubuntu CI the registered
// families measured about 1.8 times too wide, and this tells which file, and whether the first Linux fix (not registering them)
// can be undone.
func TestDiagRegisteredFontMetrics(t *testing.T) {
	files, _ := fontFiles.ReadDir("fonts")
	for _, f := range files {
		b, err := fontFiles.ReadFile("fonts/" + f.Name())
		if err != nil {
			t.Fatal(err)
		}
		family := "keel-diag-" + strings.TrimSuffix(f.Name(), ".woff2")
		err = ui.RegisterFont(b, family)
		w := probeWidth("MMMMMMMMMM", uiFontSize, family)
		ink := 0
		for _, p := range renderIn(family) {
			if p > 32 {
				ink++
			}
		}
		t.Logf("linux-diag: %s: RegisterFont=%v, ten Ms at %v px are %.2f wide (%.2f each), \"Hosts Panes Snippets Forwards\" %.2f, %d lit bytes in a render, missing family is %.2f wide",
			f.Name(), err, uiFontSize, w, w/10, probeWidth("Hosts Panes Snippets Forwards", uiFontSize, family), ink, probeWidth("MMMMMMMMMM", uiFontSize, "keel-test-missing-5d1e"))
	}
	for _, ff := range ui.FontFamilies() {
		if strings.HasPrefix(ff.Name, "keel-diag-jetbrains-mono-latin-400") || strings.HasPrefix(ff.Name, "keel-diag-ibm-plex-mono-latin-400") {
			t.Logf("linux-diag: family %q monospaced %v registered %v", ff.Name, ff.Monospaced, ff.Registered)
		}
	}
	// Is the registered face a monospace face at some size, and does its width scale with the size? A real 600-unit mono
	// gives the same advance to every glyph (7.8 px at 13) and doubles when the size does; ten Ms at 15 px each and a text that is
	// not 15 px a character would be a proportional face, or the family resolved to a different font than the file's.
	fam := "keel-diag-jetbrains-mono-latin-400-normal"
	for _, size := range []float32{6.5, 13, 26} {
		var sb strings.Builder
		for _, g := range []string{"M", "i", "W", "l", ".", "0", " "} {
			sb.WriteString(g + "=" + strconv.FormatFloat(float64(probeWidth(strings.Repeat(g, 10), size, fam))/10, 'f', 2, 32) + " ")
		}
		t.Logf("linux-diag: %s at %v px, advance of one glyph: %s| system monospace M=%.2f", fam, size, sb.String(), probeWidth("MMMMMMMMMM", size, "monospace")/10)
	}
	var names []string
	for _, ff := range ui.FontFamilies() {
		if ff.Registered || strings.Contains(strings.ToLower(ff.Name), "mono") {
			names = append(names, ff.Name+"(mono="+strconv.FormatBool(ff.Monospaced)+",reg="+strconv.FormatBool(ff.Registered)+")")
		}
	}
	t.Logf("linux-diag: families that are registered or called mono: %s", strings.Join(names, ", "))
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
		for _, l := range []string{"Toggle sidebar", "Agent", "sidebar hosts", "pane 1", "agent page", "agent panel", "agent conversation", "start page", "Resize sidebar",
			"conversation Why is pg-2 lagging", "Rename this conversation", "host filter All", "host filter ceph-rgw-1", "Retention settings", "In scope", "Conversations"} {
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
