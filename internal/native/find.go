package native

import (
	"fmt"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// Find in the terminal. The bar drives the terminal's own
// search, which only highlights and scrolls: nothing is sent to the session.

type termSearch struct {
	start      func(text string) // "" ends the search
	next, prev func()
	status     func() (selected, total int) // selected is 1 for the newest match; 0 when none is
}

// openFind opens the find bar in p and gives its field the keyboard. Only one bar is open at a time.
func (w *workspace) openFind(p *pane) {
	if p == nil || p.search == nil {
		return
	}
	if w.findPane != nil && w.findPane != p {
		w.endFind()
	}
	w.findPane, w.findFocus, w.findDone = p, true, ""
}

func (w *workspace) endFind() {
	if p := w.findPane; p != nil && p.search != nil {
		p.search.start("")
	}
	w.findPane, w.findDone = nil, ""
}

// closeFind is endFind that, with refocus, gives the keyboard back to the terminal.
func (w *workspace) closeFind(refocus bool) {
	p := w.findPane
	w.endFind()
	if refocus && p != nil {
		w.wantFocus = p.id
	}
}

func (w *workspace) findStep(next bool) {
	if p := w.findPane; p != nil && p.search != nil && w.findText != "" {
		if w.findDone != w.findText { // text typed in this frame
			p.search.start(w.findText)
			w.findDone = w.findText
		}
		if next {
			p.search.next()
		} else {
			p.search.prev()
		}
	}
}

// syncFind starts the search when the text changed, so it searches as you type.
func (w *workspace) syncFind() {
	if p := w.findPane; p != nil && p.search != nil && w.findText != w.findDone {
		p.search.start(w.findText)
		w.findDone = w.findText
	}
}

// findCount is the bar's count: "2/3", or "0" for text that is not found; "" without text.
func (w *workspace) findCount(p *pane) string {
	if w.findText == "" || p.search == nil {
		return ""
	}
	sel, total := p.search.status()
	switch {
	case total > 0 && sel > 0:
		return fmt.Sprintf("%d/%d", sel, total)
	case total > 0:
		return fmt.Sprint(total)
	}
	return "0"
}

func (w *workspace) findBar(c *ui.Context, p *pane) {
	hairline(ui.Row(c).FillWidth().Height(sizeControl).Shrink(0).Padding(0, space1, 0, space3).Gap(space1_5).
		Background(w.ground(surface)).Label("find bar"), 0, 0, 1, 0).Children(func() {
		in := ui.TextInputBase(c, &w.findText).Label("Find in terminal").Placeholder("Find").
			PlaceholderColor(inkFaint).TextColor(ink).FontSize(uiFontSize).Grow(1).MinWidth(0).Bind(&w.findHandle)
		if w.findFocus { // opening the bar selects the last search, so typing replaces it
			in.SetTextSelection(0, utf8.RuneCountInString(w.findText))
		}
		in.OnTextKey(func(k ui.TextKey) bool {
			switch {
			case k.Key == ui.KeyEscape && k.Mods == 0:
				w.act(func() { w.closeFind(true) })
			case k.Key == ui.KeyEnter && k.Mods == 0:
				w.act(func() { w.findStep(true) })
			case k.Key == ui.KeyEnter && k.Mods == ui.Shift:
				w.act(func() { w.findStep(false) })
			default:
				return false
			}
			return true
		})
		caption(c, w.findCount(p), inkMuted)
		if iconBtn(c, "chevron-up", "Previous match", sizeControlSm, false) {
			w.act(func() { w.findStep(false) })
		}
		if iconBtn(c, "chevron-down", "Next match", sizeControlSm, false) {
			w.act(func() { w.findStep(true) })
		}
		if iconBtn(c, "cancel-01", "Close", sizeControlSm, false) {
			w.act(func() { w.closeFind(true) })
		}
	})
}
