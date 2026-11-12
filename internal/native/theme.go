package native

import (
	"runtime"

	"github.com/egoist/mygo/ui"
)

// Appearance · Theme (term.js themeChoice, setTheme, applyThemeClass; page.templ applyTheme): the choice is system,
// light or dark, saved in keel-native.json as "theme". system follows the OS appearance and changes with it. The colours
// of each theme are tokens.go's; everything reads them through the variables useTheme sets.

const (
	themeSystem = "system"
	themeLight  = "light"
	themeDark   = "dark"
)

var themeChoices = [][2]string{{themeSystem, "System"}, {themeLight, "Light"}, {themeDark, "Dark"}}

// validTheme is the choice, or system for anything else (term.js setTheme).
func validTheme(s string) string {
	if s == themeLight || s == themeDark {
		return s
	}
	return themeSystem
}

// followsNatively reports whether the window takes the theme as its own appearance (the macOS chrome, as
// internal/desktop/appearance.go does for the webview). Elsewhere a theme that differs from the OS's keeps the window
// opaque, because the backdrop would show the OS's tint (page.templ applyTheme).
func followsNatively() bool { return runtime.GOOS == "darwin" }

// resolveTheme runs first in every frame: it reads the OS appearance, works out the theme in use and sets the colour
// variables. A change gives every terminal its colours and the window its appearance.
func (w *workspace) resolveTheme(c *ui.Context) {
	choice := validTheme(w.win.Theme)
	osDark := c.Theme().Dark
	if w.themeNative && w.themeSeen && choice != themeSystem {
		osDark = w.osDark // the window's own appearance is the choice now: the OS's was read before it
	}
	dark := choice == themeDark || (choice == themeSystem && osDark)
	changed := !w.themeSeen || dark != w.dark
	w.osDark, w.dark, w.themeSeen = osDark, dark, true
	useTheme(dark)
	useAccent(dark, c.Preferences().Accent)
	if w.translucentWin { // the grounds' alpha: the opacity, unless the theme cannot match the backdrop
		w.alpha = alphaFor(w.win.Opacity)
		if w.opaqueForTheme() {
			w.alpha = 1
		}
	}
	useControls(w.alpha, dark)
	if !changed {
		return
	}
	for _, t := range w.tabs {
		for _, p := range t.panes {
			if p.setTheme != nil {
				p.setTheme()
			}
		}
	}
}

// The system accent (plan 030). MyGo reads it from the desktop (NSColor.controlAccentColor on macOS, the settings portal on
// Linux) and hands it to a frame as Context.Preferences().Accent, and a change of it repaints the next frame; zero means the
// desktop has none, and the appearance's MyGo accent stands. No cgo and no private API: nothing of Keel's own reads it.
// accentSet is the family a theme and the views take from it.
type accentSet struct{ fill, hover, pressed, text, ink, soft, focus, selection ui.Color }

var accent accentSet

// accentBase is the accent of MyGo's own theme for an appearance, the fallback.
func accentBase(dark bool) ui.Color {
	if dark {
		return ui.DarkTheme().Accent
	}
	return ui.LightTheme().Accent
}

// deriveAccent builds the accent family for the system accent a on an appearance. The text on the accent is white when the
// fill can be darkened by at most a fifth to give it 4.5:1, else near-black when that gives 4.5:1, else the fill goes darker
// until white does; ink is the accent as text on the page, darkened (light) or lightened (dark) toward the text colour until
// it has 4.5:1 on both the page and the bars.
func deriveAccent(dark bool, a ui.Color) accentSet {
	if a.A == 0 {
		a = accentBase(dark)
	}
	a.A = 255
	white, black, nearBlack := ui.RGB(255, 255, 255), ui.RGB(0, 0, 0), ui.RGB(24, 24, 27)
	fill, text := a, white
	switch {
	case contrast(a, white) >= 4.5:
	case contrast(a.Mix(black, 0.2), white) >= 4.5:
		for m := float32(0.05); ; m += 0.05 {
			if fill = a.Mix(black, m); contrast(fill, white) >= 4.5 {
				break
			}
		}
	case contrast(a, nearBlack) >= 4.5:
		text = nearBlack
	default:
		for m := float32(0.25); ; m += 0.05 {
			if fill = a.Mix(black, m); contrast(fill, white) >= 4.5 || m >= 1 {
				break
			}
		}
	}
	s := accentSet{fill: fill, text: text}
	if dark {
		s.hover, s.pressed = fill.Mix(white, 0.2), fill.Mix(black, 0.15)
		s.selection, s.focus, s.soft = a.Alpha(0.4), a.Mix(white, 0.2).Alpha(0.6), a.Alpha(0.16)
	} else {
		s.hover, s.pressed = fill.Mix(black, 0.12), fill.Mix(black, 0.25)
		s.selection, s.focus, s.soft = a.Alpha(0.25), a.Alpha(0.55), a.Alpha(0.14)
	}
	toward := tok(paletteOf(dark).ink)
	s.ink = a
	for m := float32(0.05); m <= 1; m += 0.05 {
		if contrast(s.ink, tok(paletteOf(dark).surface)) >= 4.5 && contrast(s.ink, tok(paletteOf(dark).chrome)) >= 4.5 {
			break
		}
		s.ink = a.Mix(toward, m)
	}
	return s
}

// useAccent sets the accent family of the colour variables for the system accent a (zero: MyGo's default).
func useAccent(dark bool, a ui.Color) {
	accent = deriveAccent(dark, a)
	accentFill, accentStrong, onAccent, accentInk, accentSoft, focus = accent.fill, accent.hover, accent.text, accent.ink, accent.soft, accent.ink // focus: Keel's own rings (the tile) are opaque; MyGo's ring is accent.focus
}

// controlBase is the opaque face of MyGo's controls (Theme.Surface) in an appearance, before the control alpha.
func controlBase(dark bool) ui.Color {
	if dark {
		return ui.DarkTheme().Surface
	}
	return ui.LightTheme().Surface
}

// windowTheme is the one place that builds the MyGo theme for a frame and sets it (plan 030). It starts from MyGo's own light
// or dark theme, whose radius, spacing, control sizes and focus ring stay, and changes only: the system accent family, the
// meaning colours MyGo's components use (Danger, Warning, Success are flare, signal and tide), the control fills at the window's
// control alpha, and the font size (the system font, scaled by Appearance · UI size). The old per-frame globals map as:
// Background = surface (the page), Surface/SurfaceHover = the control fills, Border = line, Text/TextMuted = ink/inkMuted,
// Accent = accentFill, Focus = focus, FontSize = uiFontSize. The grounds the window draws itself (w.ground) are chrome, surface
// and stage.
func windowTheme(c *ui.Context, dark bool) {
	t := ui.LightTheme()
	if dark {
		t = ui.DarkTheme()
	}
	t.Background, t.Surface, t.SurfaceHover, t.SurfacePressed = surface, controlFill(controlBase(dark)), hoverGround, controlFill(t.SurfacePressed)
	t.Border, t.Text, t.TextMuted = line, ink, inkMuted
	t.Danger, t.Warning, t.Success = flare, signal, tide
	t.Accent, t.AccentHover, t.AccentPressed, t.AccentText = accent.fill, accent.hover, accent.pressed, accent.text
	t.Selection, t.Focus = accent.selection, accent.focus
	t.Font, t.FontSize = "", uiFontSize
	c.SetTheme(t)
}

// opaqueForTheme is the rule "when the theme differs from the system's, the window stays opaque", for a window that
// cannot take the theme as its own appearance.
func (w *workspace) opaqueForTheme() bool {
	return !w.themeNative && w.dark != w.osDark
}

// setTheme is term.js setTheme: it keeps the choice and gives the window its appearance.
func (w *workspace) setTheme(choice string) {
	choice = validTheme(choice)
	if choice == w.win.Theme {
		return
	}
	w.win.Theme = choice
	w.savePref("theme", choice)
	if w.appearanceHook != nil {
		w.appearanceHook(choice)
	}
}
