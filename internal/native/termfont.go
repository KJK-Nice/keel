package native

// Settings · Terminal · Font: the five choices, with their labels. JetBrains Mono and IBM Plex Mono ship with the client (fonts/); the others are the
// system's. The choice is kept in keel-native.json as "termFont" and applies to every open pane at once (Terminal.SetFont).

const (
	fontJetBrains = "jetbrains"
	fontPlex      = "ibmplex"
	plexFamily    = "IBM Plex Mono"
)

// termFont is one option: its value, its label and description, and the family list the terminal is given.
type termFont struct{ value, label, desc, stack string }

// monospace ends each stack, as monoStack does.
var termFonts = []termFont{
	{fontJetBrains, "JetBrains Mono", "Ships with Keel", monoFamily + ",SFMono-Regular,Menlo,Consolas," + monoFallback},
	{fontPlex, plexFamily, "Ships with Keel", plexFamily + ",Menlo,Consolas," + monoFallback},
	{"menlo", "Menlo / SF Mono", "macOS", "Menlo,SFMono-Regular," + monoFallback},
	{"consolas", "Consolas", "Windows", "Consolas," + monoFallback},
	{"system", "System mono", "The system's monospace font", "SFMono-Regular,Menlo,Consolas," + monoFallback},
}

// validTermFont is the value, or the first font for anything else.
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
	if v == w.prefs.TermFont {
		return
	}
	w.prefs.TermFont = v
	w.setPref(keyTermFont)
	for _, t := range w.tabs {
		for _, p := range t.panes {
			p.fontFamily = termStack(v)
			if p.setFont != nil {
				p.setFont(p.fontSize)
			}
		}
	}
}
