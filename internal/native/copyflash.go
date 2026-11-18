package native

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"golang.org/x/text/width"
)

// Settings · Terminal · Play animation on copy: when the terminal copies text (the user's copy, Copy on select, a
// program's OSC 52), the copied cells flash in the selection's colour and fade (duration-slow). The plugin tells only
// the text (Options.OnCopy), so the cells are found in the screen's text: a copy found exactly once on the screen
// flashes its cells; one found nowhere (the scrollback, a full-screen program) or more than once flashes the whole
// terminal, faintly. With reduced motion nothing flashes. Kept in keel-native.json ("copyFlash"), on by default.

const (
	durationSlow = 240 * time.Millisecond // duration-slow
	flashKey     = "copy-flash"
)

// cellSpan is n cells from col on screen row row (0-based).
type cellSpan struct{ row, col, n int }

// copyFlash is a flash in progress: its cells (none for the whole terminal) and its number, which starts its fade.
type copyFlash struct {
	spans []cellSpan
	seq   int
}

// cellsOf is the cells s takes on the screen: two for an East Asian wide or full-width character.
func cellsOf(s string) int {
	n := 0
	for _, r := range s {
		switch width.LookupRune(r).Kind() {
		case width.EastAsianWide, width.EastAsianFullwidth:
			n += 2
		default:
			n++
		}
	}
	return n
}

// copiedSpans finds text on the screen of a terminal Snapshot: the cells of its one place, or false when it is not
// on the screen, is there more than once, or the screen is a full-screen program's.
func copiedSpans(snap string, rows int, text string) ([]cellSpan, bool) {
	alt, lines, top, _ := readSnapshot(snap, rows)
	if alt || top >= len(lines) {
		return nil, false
	}
	screen := lines[top:]
	parts := strings.Split(strings.ReplaceAll(strings.TrimRight(text, "\r\n"), "\r\n", "\n"), "\n")
	for i := range parts {
		parts[i] = strings.TrimRight(parts[i], " ")
	}
	if strings.TrimSpace(strings.Join(parts, "")) == "" {
		return nil, false
	}
	var found []cellSpan
	places := 0
	if len(parts) == 1 {
		for row, l := range screen {
			for from := 0; ; {
				i := strings.Index(l[from:], parts[0])
				if i < 0 {
					break
				}
				places++
				found = []cellSpan{{row, cellsOf(l[:from+i]), cellsOf(parts[0])}}
				from += i + len(parts[0])
			}
		}
	} else {
		k := len(parts)
		for row := 0; row+k <= len(screen); row++ {
			first, last := screen[row], screen[row+k-1]
			if !strings.HasSuffix(first, parts[0]) || !strings.HasPrefix(last, parts[k-1]) {
				continue
			}
			match := true
			for j := 1; j < k-1; j++ {
				match = match && screen[row+j] == parts[j]
			}
			if !match {
				continue
			}
			places++
			found = []cellSpan{{row, cellsOf(first[:len(first)-len(parts[0])]), cellsOf(parts[0])}}
			for j := 1; j < k; j++ {
				found = append(found, cellSpan{row + j, 0, cellsOf(parts[j])})
			}
		}
	}
	if places != 1 {
		return nil, false
	}
	return found, true
}

// copied is the terminal's copy: the pane flashes what it copied, when the setting is on.
func (p *pane) copied(snap string, rows int, text string) {
	if !p.copyFlash {
		return
	}
	spans, _ := copiedSpans(snap, rows, text)
	p.flash = &copyFlash{spans: spans, seq: p.flashSeq + 1}
}

// copyFlashOverlay draws p's flash over its terminal: the copied cells, or the whole terminal faintly, fading out.
func (w *workspace) copyFlashOverlay(c *ui.Context, p *pane, host ui.Element) {
	f := p.flash
	if f == nil {
		return
	}
	if f.seq != p.flashSeq { // a new copy starts its fade from full
		p.flashSeq = f.seq
		host.Jump(flashKey, 1)
	}
	a := host.AnimateTo(flashKey, 0, ui.Animation{Duration: durationSlow, Ease: ui.EaseOut}) // reduced motion: at once, so nothing shows
	if a <= 0 {
		p.flash = nil
		return
	}
	col := p.theme().Selection
	none := func(float32, float32, ui.Rect) bool { return false }
	if len(f.spans) == 0 {
		ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Background(col).Opacity(a / 2).Label("copy flash").HitTest(none)
		return
	}
	g := gridFor(p.termFont(p.fontSize), p.sg.scale)
	for _, s := range f.spans {
		if s.n == 0 {
			continue
		}
		r := g.at(s.row, s.col, s.n)
		ui.Box(c).Absolute().Left(r.X).Top(r.Y).Width(r.W).Height(r.H).Radius(radiusXs).Background(col).Opacity(a).Label("copy flash").HitTest(none)
	}
}

// setCopyFlash is the Play animation on copy row.
func (w *workspace) setCopyFlash(on bool) {
	if on == w.prefs.CopyFlash {
		return
	}
	w.prefs.CopyFlash = on
	w.setPref(keyCopyFlash)
	for _, t := range w.tabs {
		for _, p := range t.panes {
			p.copyFlash = on
		}
	}
}
