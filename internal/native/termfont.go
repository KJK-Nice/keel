package native

// Settings · Terminal · Font (term.js FONT_STACKS and fontFamily; terminalFontOptions in internal/ui/data.go): the five
// choices of the app, with its labels. JetBrains Mono and IBM Plex Mono ship with the client (fonts/); the others are the
// system's. The choice is kept in keel-native.json as "termFont" and applies to every open pane at once (Terminal.SetFont).

const (
	fontJetBrains = "jetbrains"
	fontPlex      = "ibmplex"
	plexFamily    = "IBM Plex Mono"
)

// termFont is one option: its value, the app's label and description, and the family list the terminal is given.
type termFont struct{ value, label, desc, stack string }

// The stacks are the app's with ui-monospace left out (a CSS keyword); monospace ends each, as monoStack does.
var termFonts = []termFont{
	{fontJetBrains, "JetBrains Mono", "Ships with Keel", monoFamily + ",SFMono-Regular,Menlo,Consolas," + monoFallback},
	{fontPlex, plexFamily, "Ships with Keel", plexFamily + ",Menlo,Consolas," + monoFallback},
	{"menlo", "Menlo / SF Mono", "macOS", "Menlo,SFMono-Regular," + monoFallback},
	{"consolas", "Consolas", "Windows", "Consolas," + monoFallback},
	{"system", "System mono", "The system's monospace font", "SFMono-Regular,Menlo,Consolas," + monoFallback},
}

// validTermFont is the value, or the first font for anything else (term.js loadPrefs).
func validTermFont(v string) string {
	for _, f := range termFonts {
		if f.value == v {
			return v
		}
	}
	return fontJetBrains
}

// termStack is the family list of a font value.
func termStack(v string) string {
	for _, f := range termFonts {
		if f.value == v {
			return f.stack
		}
	}
	return termFonts[0].stack
}

// setTermFont is the Font row: every terminal takes the family at once and the choice is kept.
func (w *workspace) setTermFont(v string) {
	v = validTermFont(v)
	if v == w.win.TermFont {
		return
	}
	w.win.TermFont = v
	w.savePref("termFont", v)
	for _, t := range w.tabs {
		for _, p := range t.panes {
			p.fontFamily = termStack(v)
			if p.setFont != nil {
				p.setFont(p.fontSize)
			}
		}
	}
}
