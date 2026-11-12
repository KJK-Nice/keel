package native

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// Plan 031 step 2, group 6: a wait that does not end at the model shows MyGo's spinner, named by the wait, and none shows
// when nothing waits.
func TestWaitsShowASpinner(t *testing.T) {
	r := approveRig(t)
	r.w.newHost()
	r.tt.Frame()
	r.tt.Frame()
	d := r.w.hostDlg
	d.test = hostTest{state: "dead", short: "Could not test", sent: d.draftHash()}
	r.tt.Frame()
	if r.has("Testing") {
		t.Error("a spinner shows with no test running")
	}
	d.test = hostTest{running: true, state: "connecting", short: "Testing…", sent: d.draftHash(), cancel: func() {}}
	r.tt.Frame()
	if !r.has("Testing") {
		t.Error("no spinner while the connection is tested")
	}
}

// Plan 030 step 2: the theme is MyGo's, with the system accent and Keel's meaning colours.

// The palette's chrome, text and lines are MyGo's own themes' values, so a MyGo update that moves them is seen here.
func TestPaletteIsMyGos(t *testing.T) {
	for _, c := range []struct {
		name string
		p    *hexPalette
		m    *ui.Theme
	}{{"dark", &darkHex, ui.DarkTheme()}, {"light", &lightHex, ui.LightTheme()}} {
		for what, pair := range map[string][2]ui.Color{
			"page ground": {tok(c.p.surface), c.m.Background}, "hover": {tok(c.p.hover), c.m.SurfaceHover}, "line": {tok(c.p.line), c.m.Border},
			"ink": {tok(c.p.ink), c.m.Text}, "ink-muted": {tok(c.p.inkMuted), c.m.TextMuted},
		} {
			if pair[0] != pair[1] {
				t.Errorf("%s %s is %v, MyGo's is %v", c.name, what, pair[0], pair[1])
			}
		}
	}
	if tok(darkHex.raised) != ui.DarkTheme().Surface || tok(lightHex.chrome) != ui.LightTheme().Surface {
		t.Error("the dark controls' ground or the light bars' ground is not MyGo's Surface")
	}
}

// The macOS accent colours (light appearance values) and graphite: the text on the accent holds 4.5:1, and so does the
// accent as text on the page and on the bars, in both appearances.
func TestAccentDerivationKeepsContrast(t *testing.T) {
	accents := map[string]ui.Color{
		"blue": ui.RGB(0, 122, 255), "purple": ui.RGB(149, 61, 150), "pink": ui.RGB(247, 79, 158), "red": ui.RGB(224, 56, 62),
		"orange": ui.RGB(247, 130, 27), "yellow": ui.RGB(255, 198, 0), "green": ui.RGB(98, 186, 70), "graphite": ui.RGB(140, 140, 140),
	}
	for name, col := range accents {
		for _, dark := range []bool{false, true} {
			a := deriveAccent(dark, col)
			if got := contrast(a.fill, a.text); got < 4.5 {
				t.Errorf("%s dark=%v: text on the accent %.2f:1", name, dark, got)
			}
			p := paletteOf(dark)
			for what, bg := range map[string]ui.Color{"page": tok(p.surface), "bars": tok(p.chrome)} {
				if got := contrast(a.ink, bg); got < 4.5 {
					t.Errorf("%s dark=%v: accent text on the %s %.2f:1", name, dark, what, got)
				}
			}
			if a.hover == a.fill || a.pressed == a.fill {
				t.Errorf("%s dark=%v: hover or pressed equals the fill", name, dark)
			}
		}
	}
	if d := deriveAccent(true, ui.Color{}); d.fill != ui.DarkTheme().Accent && contrast(d.fill, d.text) < 4.5 {
		t.Errorf("no system accent: fill %v", d.fill)
	}
}

// The meaning colours are fixed: a Destructive badge and a Production badge are the same whatever the accent, and MyGo's
// Danger, Warning and Success agree with them.
func TestMeaningColoursDoNotFollowTheAccent(t *testing.T) {
	t.Cleanup(func() { useTheme(true) })
	for _, dark := range []bool{true, false} {
		useTheme(dark)
		useAccent(dark, ui.RGB(0, 122, 255))
		f1, s1, t1, fs1, ss1 := flare, signal, tide, flareSoft, signalSoft
		useAccent(dark, ui.RGB(247, 79, 158))
		if flare != f1 || signal != s1 || tide != t1 || flareSoft != fs1 || signalSoft != ss1 {
			t.Errorf("dark=%v: a meaning colour moved with the accent", dark)
		}
		tt := ui.NewTester(func(c *ui.Context) {
			windowTheme(c, dark)
			th := c.Theme()
			if th.Danger != flare || th.Warning != signal || th.Success != tide || th.Font != "" || th.Accent != brass {
				t.Errorf("dark=%v: theme Danger %v Warning %v Success %v Font %q Accent %v", dark, th.Danger, th.Warning, th.Success, th.Font, th.Accent)
			}
			ui.Box(c)
		}, 100, 100)
		tt.Frame()
	}
}
