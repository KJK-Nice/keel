//go:build nativee2e

package native

import (
	"testing"
	"time"
)

// A word double-clicked in the terminal is copied (Copy on select) and flashes on its own cells, then fades.
func TestE2ECopyFlash(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	sid := e.openLocal()
	e.clickBody("1")
	e.typeLine("printf 'u%sq\\n' nique")
	e.waitScrollback(sid, "uniqueq")
	e.settle(300 * time.Millisecond)
	p := e.w.focusedPane()
	_, rows := p.size()
	spans, ok := copiedSpans(p.snapshot(), rows, "uniqueq")
	if !ok {
		t.Fatalf("the output is not on the screen once: %q", p.snapshot())
	}
	term, _ := e.tt.Find("Terminal")
	g := gridFor(p.termFont(p.fontSize), p.sg.scale)
	r := g.at(spans[0].row, spans[0].col, spans[0].n)
	x, y := term.X+r.X+r.W/2, term.Y+r.Y+r.H/2
	e.tt.ClickAt(x, y)
	e.tt.ClickAt(x, y) // a double click selects the word, and Copy on select copies it
	e.step()
	e.pump(func() bool { return p.flash != nil }, "the copy's flash")
	if f := p.flash; len(f.spans) != 1 || f.spans[0] != spans[0] {
		t.Errorf("the flash is on %v, want the word's cells %v", f.spans, spans)
	}
	e.shot("TestE2ECopyFlash")
	e.tt.Advance(durationSlow + 40*time.Millisecond) // the tester's clock: animations run on it
	e.pump(func() bool { return p.flash == nil && !e.hasLabel("copy flash") }, "the flash to fade")
}
