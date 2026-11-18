package native

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// The Font picker offers the five, then every other installed monospaced family once; a name that could break the
// family list is never kept.
func TestInstalledFontsInThePickerAndTheirStack(t *testing.T) {
	got := fontEntries([]ui.FontFamily{
		{Name: "Hack", Monospaced: true},
		{Name: "Helvetica"},                   // proportional: not offered
		{Name: "Menlo", Monospaced: true},     // one of the five already
		{Name: "Bad, Font", Monospaced: true}, // a comma would split the stack
		{Name: "Fira Code", Monospaced: true},
	})
	var labels []string
	for _, e := range got {
		labels = append(labels, e.label)
	}
	want := []string{"JetBrains Mono", "IBM Plex Mono", "Menlo / SF Mono", "Consolas", "System mono", "Hack", "Fira Code"}
	if !slices.Equal(labels, want) {
		t.Errorf("picker %q, want %q", labels, want)
	}
	if s := termStack("family:Hack"); !strings.HasPrefix(s, "Hack,") || !strings.HasSuffix(s, monoFallback) {
		t.Errorf("stack %q: the family first, a monospace fallback last", s)
	}
	for _, v := range []string{"family:", "family:a,b", `family:"x"`, "family: Hack", "family:" + strings.Repeat("x", familyMax+1)} {
		if validTermFont(v) != fontJetBrains {
			t.Errorf("%q is kept", v)
		}
	}
	if termFontLabel("family:Hack") != "Hack" || termFontLabel(fontPlex) != plexFamily {
		t.Error("labels")
	}
}

// Weight and line height load within their ranges, snap the weight to 100s, are kept, and reach an open pane's font.
func TestWeightAndLineHeightApplyToOpenPanesAndAreKept(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "keel-native.json"), []byte(`{"termWeight": 950, "termLineHeight": 90}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if p := loadPrefs(dir); p.TermWeight != termWeightDefault || p.LineHeight != lineHeightDefault {
		t.Fatalf("out of range loads as the default: %d %d", p.TermWeight, p.LineHeight)
	}
	r := settingsRig(t, settingsBackend())
	r.w.openHost(testHosts[1])
	p := r.w.focusedPane()
	calls := 0
	p.setFont = func(float32) { calls++ }
	r.w.setTermWeight(530)
	r.w.setLineHeight(130)
	if r.w.prefs.TermWeight != 500 || p.font.Weight != 500 || p.font.LineHeight != 1.3 || calls != 2 {
		t.Fatalf("weight %d, pane %d %v, setFont %d", r.w.prefs.TermWeight, p.font.Weight, p.font.LineHeight, calls)
	}
	if f := p.termFont(14); f.Weight != 500 || f.LineHeight != 1.3 || f.Size != 14 {
		t.Errorf("font %+v", f)
	}
	r.w.flushPrefs()
	if got := loadPrefs(r.w.dataDir); got.TermWeight != 500 || got.LineHeight != 130 {
		t.Errorf("kept %d %d", got.TermWeight, got.LineHeight)
	}
}

// The suggestion marks' grid has the terminal's rows: taller with the line height, as paint.go sizes them.
func TestGridRowsFollowTheLineHeight(t *testing.T) {
	one, tall := gridFor(terminal.Font{Family: gateStack(), Size: 12}, 2), gridFor(terminal.Font{Family: gateStack(), Size: 12, LineHeight: 1.5}, 2)
	if tall.ch <= one.ch*1.4 || tall.cw != one.cw {
		t.Errorf("rows %v → %v, columns %v → %v", one.ch, tall.ch, one.cw, tall.cw)
	}
}

// Font features keep valid OpenType tags only, once each; Ligatures off turns off calt, liga and dlig unless the
// features name them.
func TestFontFeaturesAndLigatures(t *testing.T) {
	for in, want := range map[string]string{
		"ss01, CV02 -calt":           "ss01, cv02, -calt",
		"ss01 ss01 +zero cv05=2":     "ss01, +zero, cv05=2",
		"liga=1000, x, --calt, -a=1": "",
		"ss1, frac, tnum=, onum=x":   "frac",
		"-calt=1, ss02":              "ss02", // a value and an off sign together say nothing
	} {
		if got := cleanFeatures(in); got != want {
			t.Errorf("cleanFeatures(%q) = %q, want %q", in, got, want)
		}
	}
	many := strings.Repeat("ss01 ss02 ss03 ss04 ss05 ss06 ss07 ss08 ss09 ss10 ", 2) + "cv01 cv02 cv03 cv04 cv05 cv06 cv07"
	if n := len(strings.Split(cleanFeatures(many), ", ")); n != featuresMax {
		t.Errorf("%d kept, at most %d", n, featuresMax)
	}
	p := defaultPrefs()
	if f := p.termFont(); len(f.Features) != 0 || f.CellWidth != 1 || f.Thicken {
		t.Errorf("defaults: %+v", f)
	}
	p.Ligatures, p.Features = false, "ss01, +liga"
	if got := p.termFont().Features; !slices.Equal(got, []string{"ss01", "+liga", "-calt", "-dlig"}) {
		t.Errorf("ligatures off with liga named: %q", got)
	}
}

// Cell width snaps to steps of 5, reaches an open pane, widens the suggestion grid's columns only, and the advanced
// rows are kept.
func TestAdvancedFontRowsApplyAndAreKept(t *testing.T) {
	r := settingsRig(t, settingsBackend())
	r.w.openHost(testHosts[1])
	p := r.w.focusedPane()
	calls := 0
	p.setFont = func(float32) { calls++ }
	r.w.setCellWidth(122)
	r.w.setLigatures(false)
	r.w.setFeatures("SS02 junky")
	r.w.setThicken(true)
	if r.w.prefs.CellWidth != 120 || p.font.CellWidth != 1.2 || r.w.prefs.Features != "ss02" || calls != 4 {
		t.Fatalf("cell width %d → %v, features %q, setFont %d", r.w.prefs.CellWidth, p.font.CellWidth, r.w.prefs.Features, calls)
	}
	if p.font.Thicken != isMac() {
		t.Errorf("thicken %v on %s", p.font.Thicken, runtime.GOOS)
	}
	one, wide := gridFor(terminal.Font{Family: gateStack(), Size: 12}, 2), gridFor(p.termFont(12), 2)
	if wide.cw < one.cw*1.15 || wide.ch != gridFor(terminal.Font{Family: p.font.Family, Size: 12}, 2).ch {
		t.Errorf("columns %v → %v", one.cw, wide.cw)
	}
	r.w.flushPrefs()
	got := loadPrefs(r.w.dataDir)
	if got.CellWidth != 120 || got.Ligatures || got.Features != "ss02" || !got.Thicken {
		t.Errorf("kept %+v", got)
	}
	r.w.showPage("settings")
	r.tt.Frame()
	r.section(t, "Terminal")
	r.texts(t, "Advanced font", "Ligatures", "Font features", "Cell width")
	for _, l := range []string{"Ligatures switch", "Font features input", "Cell width field"} {
		if !r.has(l) {
			t.Errorf("no %q", l)
		}
	}
}
