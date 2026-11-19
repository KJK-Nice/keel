package native

import (
	"strings"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// Settings · Terminal · Typography: the font, its weight and the line height, kept in keel-native.json ("termFont",
// "termWeight", "termLineHeight") and applied to every open pane at once (Terminal.SetFont).
//
// The font is one of the five choices below (JetBrains Mono and IBM Plex Mono ship with the client, fonts/; the others
// are the system's) or any monospaced family installed on this machine, kept as "family:<name>". Nothing is loaded from
// the network: a family is the system's, or one the app registered.

const (
	fontJetBrains = "jetbrains"
	fontPlex      = "ibmplex"
	plexFamily    = "IBM Plex Mono"
	familyPrefix  = "family:" // an installed family, by name
	familyMax     = 100       // the longest family name kept
)

const (
	termWeightMin, termWeightMax, termWeightDefault, termWeightStep = 100, 900, 400, 100
	lineHeightMin, lineHeightMax, lineHeightDefault                 = 100, 200, 100
	cellWidthMin, cellWidthMax, cellWidthDefault, cellWidthStep     = 80, 150, 100, 5
	featuresMax                                                     = 16  // the most features kept
	featuresLen                                                     = 160 // the Font features input's longest text
)

// termFont is one option: its value, its label and description, and the family list the terminal is given.
type termFont struct{ value, label, desc, stack string }

// fallbackStack follows a chosen family, so a family that is gone draws in a monospace font, as monoStack ends.
const fallbackStack = ",SFMono-Regular,Menlo,Consolas," + monoFallback

var termFonts = []termFont{
	{fontJetBrains, "JetBrains Mono", "Ships with Slime", monoFamily + fallbackStack},
	{fontPlex, plexFamily, "Ships with Slime", plexFamily + ",Menlo,Consolas," + monoFallback},
	{"menlo", "Menlo / SF Mono", "macOS", "Menlo,SFMono-Regular," + monoFallback},
	{"consolas", "Consolas", "Windows", "Consolas," + monoFallback},
	{"system", "System mono", "The system's monospace font", "SFMono-Regular,Menlo,Consolas," + monoFallback},
}

// familyOf is the installed family a value names, or "".
func familyOf(v string) string {
	name, ok := strings.CutPrefix(v, familyPrefix)
	if !ok {
		return ""
	}
	return name
}

// validTermFont is the value, or the first font for anything else. A family name is kept only when it could not
// break the family list: not empty, no comma or quote, not too long.
func validTermFont(v string) string {
	for _, f := range termFonts {
		if f.value == v {
			return v
		}
	}
	if name := familyOf(v); strings.TrimSpace(name) == name && name != "" && len(name) <= familyMax && !strings.ContainsAny(name, `,"'`) {
		return v
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
	if name := familyOf(validTermFont(v)); name != "" {
		return name + fallbackStack
	}
	return termFonts[0].stack
}

// termFontLabel is what the Font row shows for a value.
func termFontLabel(v string) string {
	for _, f := range termFonts {
		if f.value == v {
			return f.label
		}
	}
	if name := familyOf(v); name != "" {
		return name
	}
	return termFonts[0].label
}

// fontEntries are the Font picker's choices: the five, then every other monospaced family of this machine.
func fontEntries(families []ui.FontFamily) []pickEntry {
	out := make([]pickEntry, 0, len(termFonts)+len(families))
	taken := map[string]bool{monoFamily: true, plexFamily: true, "Menlo": true, "Consolas": true}
	for _, f := range termFonts {
		out = append(out, pickEntry{value: f.value, label: f.label, desc: f.desc})
	}
	for _, f := range families {
		v := familyPrefix + f.Name
		if !f.Monospaced || taken[f.Name] || validTermFont(v) != v {
			continue
		}
		taken[f.Name] = true
		out = append(out, pickEntry{value: v, label: f.Name, desc: "Installed"})
	}
	return out
}

func lineHeightOf(pct int) float32 { return float32(pct) / 100 }

// noLigatures are the features Ligatures off turns off, unless the features name them.
var noLigatures = []string{"calt", "liga", "dlig"}

// cleanFeatures keeps the OpenType features of s, as the Font features row takes them: separated by commas or spaces,
// each a four-letter tag, + or - before it (on or off) or =n after it (a value), as Ghostty's font-feature. Anything
// else is dropped, as is a second mention of a tag; at most featuresMax are kept.
func cleanFeatures(s string) string {
	var out []string
	seen := map[string]bool{}
	for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		tag, val, hasVal := strings.Cut(strings.TrimLeft(f, "+-"), "=")
		if len(f)-len(strings.TrimLeft(f, "+-")) > 1 || len(tag) != 4 || seen[tag] || len(out) == featuresMax {
			continue
		}
		ok := !hasVal || (len(val) >= 1 && len(val) <= 3 && strings.Trim(val, "0123456789") == "")
		for _, r := range tag {
			ok = ok && (r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
		}
		if ok && !(hasVal && f[0] == '-') {
			seen[tag] = true
			out = append(out, f)
		}
	}
	return strings.Join(out, ", ")
}

// termFont is the terminal's font of the preferences, its Size aside.
func (p prefsDoc) termFont() terminal.Font {
	var features []string
	named := map[string]bool{}
	for _, f := range strings.Split(cleanFeatures(p.Features), ", ") {
		if f != "" {
			features = append(features, f)
			tag, _, _ := strings.Cut(strings.TrimLeft(f, "+-"), "=")
			named[tag] = true
		}
	}
	if !p.Ligatures {
		for _, tag := range noLigatures {
			if !named[tag] {
				features = append(features, "-"+tag)
			}
		}
	}
	return terminal.Font{Family: termStack(p.TermFont), Weight: p.TermWeight, LineHeight: lineHeightOf(p.LineHeight),
		CellWidth: float32(p.CellWidth) / 100, Features: features, Thicken: p.Thicken && isMac()}
}

// termFont is the font p's terminal draws with at size.
func (p *pane) termFont(size float32) terminal.Font {
	f := p.font
	f.Size = size
	return f
}

// applyTermFont gives every open pane the font of the preferences.
func (w *workspace) applyTermFont() {
	for _, t := range w.tabs {
		for _, p := range t.panes {
			p.font = w.prefs.termFont()
			if p.setFont != nil {
				p.setFont(p.fontSize)
			}
		}
	}
}

// setTermFont is the Font row: every terminal takes the family at once and the choice is kept.
func (w *workspace) setTermFont(v string) {
	v = validTermFont(v)
	if v == w.prefs.TermFont {
		return
	}
	w.prefs.TermFont = v
	w.setPref(keyTermFont)
	w.applyTermFont()
}

// setTermWeight is the Weight row: a step of 100, from 100 to 900.
func (w *workspace) setTermWeight(n int) {
	n = min(max((n+termWeightStep/2)/termWeightStep*termWeightStep, termWeightMin), termWeightMax)
	if n == w.prefs.TermWeight {
		return
	}
	w.prefs.TermWeight = n
	w.setPref(keyTermWeight)
	w.applyTermFont()
}

// setLineHeight is the Line height row, in % of the font's own.
func (w *workspace) setLineHeight(n int) {
	n = min(max(n, lineHeightMin), lineHeightMax)
	if n == w.prefs.LineHeight {
		return
	}
	w.prefs.LineHeight = n
	w.setPref(keyLineHeight)
	w.applyTermFont()
}

// installedFonts is ui.FontFamilies, which asks the system once (tens of milliseconds) and then keeps the list; a
// test replaces it.
var installedFonts = ui.FontFamilies

// setCellWidth is the Cell width row, in % of the font's advance, in steps of 5.
func (w *workspace) setCellWidth(n int) {
	n = min(max((n+cellWidthStep/2)/cellWidthStep*cellWidthStep, cellWidthMin), cellWidthMax)
	if n == w.prefs.CellWidth {
		return
	}
	w.prefs.CellWidth = n
	w.setPref(keyCellWidth)
	w.applyTermFont()
}

// setLigatures is the Ligatures row.
func (w *workspace) setLigatures(on bool) {
	if on == w.prefs.Ligatures {
		return
	}
	w.prefs.Ligatures = on
	w.setPref(keyLigatures)
	w.applyTermFont()
}

// setThicken is the Thicken row (macOS).
func (w *workspace) setThicken(on bool) {
	if on == w.prefs.Thicken {
		return
	}
	w.prefs.Thicken = on
	w.setPref(keyThicken)
	w.applyTermFont()
}

// setFeatures is the Font features row: what is typed, cleaned; the field then shows what was kept.
func (w *workspace) setFeatures(s string) {
	s = cleanFeatures(s)
	if s == w.prefs.Features {
		return
	}
	w.prefs.Features = s
	w.setPref(keyFeatures)
	w.applyTermFont()
}
