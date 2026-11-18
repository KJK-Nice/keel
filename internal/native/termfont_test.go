package native

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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
	if r.w.prefs.TermWeight != 500 || p.fontWeight != 500 || p.lineHeight != 1.3 || calls != 2 {
		t.Fatalf("weight %d, pane %d %v, setFont %d", r.w.prefs.TermWeight, p.fontWeight, p.lineHeight, calls)
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
	one, tall := gridFor(gateStack(), 12, 400, 1, 2), gridFor(gateStack(), 12, 400, 1.5, 2)
	if tall.ch <= one.ch*1.4 || tall.cw != one.cw {
		t.Errorf("rows %v → %v, columns %v → %v", one.ch, tall.ch, one.cw, tall.cw)
	}
}
