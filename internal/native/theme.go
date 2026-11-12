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
