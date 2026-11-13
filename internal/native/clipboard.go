package native

import "github.com/egoist/mygo/ui"

// One way to the clipboard. Action and view code, and answers coming back from keel serve, have no UI context to write
// with; copy queues the text and the frame writes it (flushClipboard, called from runLater, once per frame), so a copy is
// never lost because of which panel is on screen.

type clipItem struct {
	text, what string
	quiet      bool // no toast: the control shows the copy itself (Share's button, the snippet menu)
}

// copy puts text on the clipboard and says "Copied", then what it is when there is a word for it ("Copied local address").
func (w *workspace) copy(text, what string) {
	w.clips = append(w.clips, clipItem{text: text, what: what})
}

// copyQuiet puts text on the clipboard and says nothing.
func (w *workspace) copyQuiet(text string) {
	w.clips = append(w.clips, clipItem{text: text, quiet: true})
}

// flushClipboard writes what copy queued.
func (w *workspace) flushClipboard(c *ui.Context) {
	for _, it := range w.clips {
		c.WriteClipboard(it.text)
		switch {
		case it.quiet:
		case it.what == "":
			w.say("Copied")
		default:
			w.say("Copied " + it.what)
		}
	}
	w.clips = nil
}
