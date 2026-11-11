package native

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// themeRig is a rig on a scratch data dir with one pane open.
func themeRig(t *testing.T) *rig {
	t.Helper()
	r := newRigIn(t, okBackend(), t.TempDir())
	r.click(t, "host api-1")
	return r
}

func (r *rig) wantPix(t *testing.T, what string, x, y float32, hex string) {
	t.Helper()
	if want := tok(hex); !is(r.pixel(x, y), want) {
		t.Errorf("%s at (%v,%v): got %v, want %s", what, x, y, r.pixel(x, y), hex)
	}
}

func TestChoosingATheme_RepaintsTheTitleBarSidebarTileAndDialog(t *testing.T) {
	r := themeRig(t)
	for _, th := range []struct {
		choice string
		p      *hexPalette
	}{{themeLight, &lightHex}, {themeDark, &darkHex}, {themeLight, &lightHex}} {
		r.w.setTheme(th.choice)
		r.tt.Frame()
		r.wantPix(t, th.choice+" title bar", 900, 10, th.p.chrome)
		r.wantPix(t, th.choice+" sidebar", 100, 400, th.p.chrome)
		pane, _ := r.tt.Find("pane 1")
		r.wantPix(t, th.choice+" tile body", pane.X+pane.W/2, pane.Y+pane.H/2, th.p.stage)
		r.wantPix(t, th.choice+" pane bar", pane.X+pane.W/2, pane.Y+sizePanebar/2, th.p.surface)
		r.openPal(t)
		d, ok := r.tt.Find("dialog Command palette")
		if !ok {
			t.Fatal("no palette dialog")
		}
		r.wantPix(t, th.choice+" dialog", d.X+d.W-90, d.Y+paletteSearch/2, th.p.raised)
		r.w.closePalette()
		r.tt.Frame()
	}
}

func TestSystemFollowsTheOSAppearanceLive(t *testing.T) {
	r := themeRig(t)
	r.w.setTheme(themeSystem)
	for _, dark := range []bool{true, false, true} {
		r.tt.SetDark(dark)
		r.tt.Frame()
		p := paletteOf(dark)
		r.wantPix(t, "title bar", 900, 10, p.chrome)
		if r.w.dark != dark {
			t.Errorf("dark %v after the OS went dark=%v", r.w.dark, dark)
		}
	}
	r.w.setTheme(themeLight) // a fixed choice ignores the OS
	r.tt.SetDark(true)
	r.tt.Frame()
	r.wantPix(t, "light on a dark OS", 900, 10, lightHex.chrome)
	r.w.setTheme(themeDark)
	r.tt.SetDark(false)
	r.tt.Frame()
	r.wantPix(t, "dark on a light OS", 900, 10, darkHex.chrome)
}

func TestThemeIsKeptAndTheNextWindowStartsInIt(t *testing.T) {
	r := themeRig(t)
	if validTheme("sepia") != themeSystem || loadPrefs(t.TempDir()).Theme != themeSystem {
		t.Fatal("system is the default and the answer to anything else")
	}
	r.w.setTheme(themeLight)
	raw := string(r.prefsFile(t)["theme"])
	if raw != `"light"` {
		t.Fatalf("theme in keel-native.json is %s", raw)
	}
	if got := loadPrefs(r.w.dataDir).Theme; got != themeLight {
		t.Errorf("the next window reads %q", got)
	}
	// Choosing in Settings does the same, and leaves the other fields.
	if err := savePrefs(r.w.dataDir, map[string]any{"future": 1}); err != nil {
		t.Fatal(err)
	}
	r.w.setTheme(themeDark)
	if m := r.prefsFile(t); string(m["theme"]) != `"dark"` || string(m["future"]) != "1" {
		t.Errorf("file %v", m)
	}
}

func TestSettingsThemeRowChoosesTheTheme(t *testing.T) {
	r := settingsRig(t, settingsBackend())
	r.texts(t, "Theme", "Light, dark, or follow the system.")
	for _, name := range []string{"Light", "System", "Dark"} { // the app's three labels, in its order
		if !r.has("Color theme "+name) && !r.has(name) {
			t.Errorf("no %q in the theme control", name)
		}
	}
	r.click(t, "Light")
	if r.w.win.Theme != themeLight || r.w.dark {
		t.Errorf("after Light: choice %q, dark %v", r.w.win.Theme, r.w.dark)
	}
	r.wantPix(t, "settings page", 700, 400, lightHex.surface)
	r.click(t, "Dark")
	r.wantPix(t, "settings page", 700, 400, darkHex.surface)
	r.click(t, "System")
	if r.w.win.Theme != themeSystem {
		t.Errorf("choice %q", r.w.win.Theme)
	}
}

func TestEveryOpenTerminalGetsTheThemesColours(t *testing.T) {
	r := themeRig(t)
	r.click(t, "Split right")
	n := map[string]int{}
	for id, p := range r.w.tab().panes {
		p.setTheme = func() { n[id]++ }
	}
	r.w.setTheme(themeLight)
	r.tt.Frame()
	r.tt.Frame() // a frame that changes nothing does not call again
	if n["1"] != 1 || n["2"] != 1 {
		t.Errorf("panes told %v, want once each", n)
	}
	if th := terminalTheme(); th.Background != tok(lightHex.termBg) || th.Foreground != tok(lightHex.termFg) || th.Palette[1] != tok(lightHex.ansi[1]) ||
		th.Cursor != tok(lightHex.termCursor) || th.Selection != tok(lightHex.termSelection) {
		t.Errorf("light terminal theme %+v", th)
	}
	r.w.setTheme(themeDark)
	r.tt.Frame()
	if th := terminalTheme(); th.Background != tok(darkHex.termBg) || th.Palette[15] != tok(darkHex.ansi[15]) {
		t.Errorf("dark terminal theme %+v", th)
	}
	if n["1"] != 2 {
		t.Errorf("panes told %v after the second change", n)
	}
}

func TestHostTintsFollowTheTheme(t *testing.T) {
	useTheme(false)
	if got := hostTint("var(--ansi-2)"); got != tok(lightHex.ansi[2]) {
		t.Errorf("light tint %v", got)
	}
	useTheme(true)
	if got := hostTint("var(--ansi-2)"); got != tok(darkHex.ansi[2]) {
		t.Errorf("dark tint %v", got)
	}
}

func TestTranslucentGroundsFollowTheThemeAndItsOpaqueRule(t *testing.T) {
	for _, native := range []bool{true, false} {
		r := themeRig(t)
		r.w.translucentWin, r.w.themeNative = true, native
		r.w.win.Opacity, r.w.alpha = 50, 0.5
		r.w.setTheme(themeSystem)
		r.tt.SetDark(true)
		r.tt.Frame()
		wantPixel(t, "dark system", r.tt.Image(), 900, 10, chrome, 0.5)
		r.w.setTheme(themeLight) // differs from the OS's
		r.tt.Frame()
		wantAlpha := float32(0.5)
		if !native {
			wantAlpha = 1 // the window cannot take the theme: it stays opaque
		}
		wantPixel(t, "light on a dark OS, native="+map[bool]string{true: "true", false: "false"}[native], r.tt.Image(), 900, 10, chrome, wantAlpha)
		// The terminal body keeps less of the see-through in the light theme (vibrancy-term-share).
		if native {
			want := 255 * (1 - (1-0.5)*float64(termShareLight))
			if got := float64(r.w.termGround().A); got < want-2 || got > want+2 {
				t.Errorf("light terminal ground alpha %v, want %v", got, want)
			}
		}
	}
}

var hexLiteral = regexp.MustCompile(`#[0-9a-fA-F]{6}|ui\.Hex\(`)

func TestNoHexColourOutsideTokens(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasPrefix(f, "tokens") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, ln := range strings.Split(string(b), "\n") {
			if hexLiteral.MatchString(ln) {
				t.Errorf("%s:%d names a hex colour: %s", f, i+1, strings.TrimSpace(ln))
			}
		}
	}
}

func TestUnlockAndFirstRunScreensFollowTheTheme(t *testing.T) {
	for _, light := range []bool{true, false} {
		p := &darkHex
		if light {
			p = &lightHex
		}
		choice := map[bool]string{true: themeLight, false: themeDark}[light]
		r := newRig(t, lockedBackend())
		r.w.win.Theme = choice
		r.tt.Frame()
		// The page's ground is the stage; the field's ground is the stage too, with a strong line.
		r.wantPix(t, choice+" Unlock ground", 100, 100, p.stage)
		r.w.gate = nil

		r2 := firstRunRig(t, setupBackend())
		r2.w.win.Theme = choice
		r2.tt.Frame()
		r2.wantPix(t, choice+" First run ground", 640, 700, p.stage)
	}
}
