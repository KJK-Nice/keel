package native

import (
	"context"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// In-pane suggestions in the window (docs/ui-redesign.md §5.5): the glue between a pane's terminal, its
// suggestion machine (suggest.go) and keel serve.
//
// Nothing here builds a frame or writes a byte. The questions go through keelclient SuggestDetect and SuggestAsk, the
// insert through SuggestInsert (insertSuggestion, below, is its one caller) and the put-back of the user's words through
// RestoreWords (restoreWords, its one caller); guard_test.go keeps each so. keel serve decides everything: whether a line
// is noticed, which command is offered, whether ⇥ may insert it, and it writes the bytes (Ctrl-U and the command, never
// an Enter). The user's keys are never held back: suggestKey answers at once and only ever takes ⇥, ⇧⇥, esc, the option
// keys and ⌘Z while a notice or a card shows.

// paneSuggest is a pane's suggestion state.
type paneSuggest struct {
	m      *sugMachine
	v      sugView     // what the machine last rendered
	line   *promptLine // the prompt line as last read, where the marks go
	cursor func() (row int, text string, ok bool)
	scale  float32 // the display scale, learned when the pane is drawn
	nextAt time.Time
	cardH  float32 // the card's height as last laid out, to place it above the line when it does not fit below
	card   ui.Handle
}

// sugReadEvery is how often a noticed line or a card is followed while the terminal redraws; the Snapshot it reads
// holds the whole scrollback, so it is not read on every frame.
const sugReadEvery = 100 * time.Millisecond

// sugClock is the machine's clock: the frame's, so a test that moves the window's clock moves it too.
func (w *workspace) sugClock() time.Time {
	if w.sugNow.IsZero() {
		return time.Now()
	}
	return w.sugNow
}

// promptLine is the line the cursor is on, split into prompt and words; nil on the alternate screen, with no prompt on it,
// or for a body that cannot be read.
func (p *pane) promptLine() *promptLine {
	if p.sg.cursor == nil {
		return nil
	}
	row, text, ok := p.sg.cursor()
	if !ok {
		return nil
	}
	pl := promptInput(text)
	if pl != nil {
		pl.Row = row
	}
	p.sg.line = pl
	return pl
}

// canSuggest: the client drives a ready local session in the pane that has the keyboard, with no dialog open. A follower, a joined pane, a session that is not ready and a pane in the
// background get no notice, no card and no frame.
func (w *workspace) canSuggest(p *pane) bool {
	t := w.tabOf(p)
	if p.sg.cursor == nil || p.typing || t == nil || t != w.tab() || t.remote || t.focused != p.id || w.modalOpen() || w.gate != nil || w.first != nil {
		return false
	}
	return p.canType()
}

func (w *workspace) redraw() {
	if w.post != nil {
		w.post(func() {})
	}
}

// suggestMachine is p's machine, made on first use.
func (w *workspace) suggestMachine(p *pane) *sugMachine {
	if p.sg.m != nil {
		return p.sg.m
	}
	p.sg.m = newSugMachine(sugDeps{
		readLine:   p.promptLine,
		canSuggest: func() bool { return w.canSuggest(p) },
		mode:       w.suggestMode,
		sendDetect: func(seq int, line string) { _ = p.conn.SuggestDetect(seq, line) },
		sendSuggest: func(seq int, line string) {
			if err := p.conn.SuggestAsk(seq, line); err != nil { // not asked: never leave the card loading
				p.sg.m.OnSuggest(seq, false, nil, "Keel could not ask for a suggestion.", "send")
			}
		},
		insert: func(sug *keelclient.Suggestion, i int, shift bool, words string, done func(insertResult)) {
			w.suggestInsert(p, sug, i, shift, words, done)
		},
		restore: func(words string) { w.restoreWords(p, words) },
		render:  func(v sugView) { p.sg.v = v; w.redraw() },
		now:     w.sugClock,
	})
	return p.sg.m
}

// suggestKeyHere is the suggest key as this platform writes it (⌘Y, Ctrl+Shift+Y).
func suggestKeyHere() string { return commandKeyLabel("agent.suggest") }

// sugSettings are the two agent settings the machines follow, as last read or saved. They are kept apart from the
// Settings page's copy so that reading them for the panes never overwrites what that page or the panel shows.
type sugSettings struct{ suggestions, detection string }

// suggestMode is what the agent settings let a pane do on its own. Before the first read it is the defaults: on, and
// only lines starting with #.
func (w *workspace) suggestMode() sugMode {
	switch {
	case w.sugSet.suggestions == "off":
		return sugOff
	case w.sugSet.detection == detectionAuto:
		return sugAuto
	}
	return sugHash
}

// noteSuggestSettings takes the settings keel serve answered with, or the ones the Settings page just changed.
func (w *workspace) noteSuggestSettings(s keelclient.AgentSettings) {
	w.sugSet = sugSettings{s.Settings.Suggestions, s.Settings.Detection}
	w.suggestSettingsChanged()
}

// suggestSettingsChanged tells every pane's machine when the settings changed what a pane may do on its own (suggestions
// on or off, how a line is noticed): at once, from the Settings switch (before keel serve has answered the save), from
// First run, and through the vault rev poll when another window changed them.
func (w *workspace) suggestSettingsChanged() {
	mode := w.suggestMode()
	if w.sugModeSeen && mode == w.sugMode {
		return
	}
	w.sugMode, w.sugModeSeen = mode, true
	for _, t := range w.tabs {
		for _, p := range t.panes {
			if p.sg.m != nil {
				p.sg.m.ModeChanged()
			}
		}
	}
	w.redraw()
}

// refreshSuggestSettings reads the agent settings again, for the machines (the window's start, an unlock, a changed rev).
func (w *workspace) refreshSuggestSettings() {
	call(w, w.be.AgentSettings, func(s keelclient.AgentSettings, err error) {
		if err == nil {
			w.noteSuggestSettings(s)
		}
	})
}

// suggestRequest is the suggest key and the palette's action: ask for a command for the words on the focused pane's
// prompt line. It only asks (the machine's one question to keel serve); the card's ⇥ is the one way a command is inserted.
func (w *workspace) suggestRequest() {
	if p := w.focusedPane(); p != nil && !p.remote {
		w.suggestMachine(p).Request()
	}
}

// keyNames are the keys the machine knows by name; any other key is "?".
func keyName(k ui.Key) string {
	switch k {
	case ui.KeyTab:
		return "Tab"
	case ui.KeyEscape:
		return "Escape"
	case ui.KeyUp:
		return "ArrowUp"
	case ui.KeyDown:
		return "ArrowDown"
	case ui.KeyEnter:
		return "Enter"
	case ui.KeyZ:
		return "z"
	}
	for i, d := range digitKeys {
		if k == d {
			return string(rune('1' + i))
		}
	}
	return "?"
}

// suggestKey is terminal.Options.OnKey for a pane: it sees every key before the terminal and reports whether it took
// it. A key it does not take reaches the shell as usual, at once; it only tells the machine that the user typed.
func (w *workspace) suggestKey(p *pane, mods ui.Modifiers, key ui.Key) bool {
	m := w.suggestMachine(p)
	k := sugKey{Name: keyName(key), Shift: mods&ui.Shift != 0, Ctrl: mods&ui.Ctrl != 0, Meta: mods&ui.Super != 0, Alt: mods&ui.Alt != 0}
	if m.OnKey(k) {
		return true
	}
	if !k.Meta {
		data := "k"
		if key == ui.KeyEnter {
			data = string(rune(13)) // the user's own Enter: the machine only sees that the line was sent
		}
		m.OnInput(data)
	}
	return false
}

// suggestFrame runs once a frame: it hands keel serve's answers to the machines, runs a detection whose pause is over,
// and follows a noticed line or a card (at most every sugReadEvery).
func (w *workspace) suggestFrame(c *ui.Context) {
	w.sugNow = c.Now()
	for _, t := range w.tabs {
		for _, p := range t.panes {
			if p.remote {
				continue
			}
			for _, ev := range p.conn.SuggestEvents() {
				m := w.suggestMachine(p)
				switch ev.Kind {
				case keelclient.SuggestDetected:
					m.OnDetect(ev.Seq, ev.Plain, ev.Source)
				case keelclient.SuggestAnswered:
					m.OnSuggest(ev.Seq, ev.OK, ev.Suggestion, ev.Message, ev.Reason)
				case keelclient.SuggestRated:
					m.OnRisk(ev.ID, ev.Options)
				}
			}
			m := p.sg.m
			if m == nil {
				continue
			}
			if due, ok := m.Due(); ok {
				if !w.sugNow.Before(due) {
					m.Tick()
				} else {
					c.After(due.Sub(w.sugNow))
				}
			}
			if m.State() != sugIdle {
				if !w.sugNow.Before(p.sg.nextAt) {
					p.sg.nextAt = w.sugNow.Add(sugReadEvery)
					m.OnRender()
				}
				c.After(sugReadEvery)
			}
		}
	}
}

// suggestInsert is the machine's insert: right before the write, the pane must still be at a prompt with the words the
// suggestion was for and no dialog holding the keyboard; otherwise the card is dropped and nothing is written.
func (w *workspace) suggestInsert(p *pane, sug *keelclient.Suggestion, index int, shift bool, words string, done func(insertResult)) {
	if !w.canSuggest(p) {
		done(insertResult{Message: "You follow this pane. Suggestions are for the driver."})
		return
	}
	if line := p.promptLine(); line == nil || line.Input != words {
		done(insertResult{Dropped: true})
		return
	}
	w.insertSuggestion(p, keelclient.SuggestionRef{ID: sug.ID, Option: index, Shift: shift}, done)
}

// insertSuggestion is the user's ⇥ / ⇧⇥ on a card: the one caller of SuggestInsert. The reference names the suggestion,
// the option and ⇧ as the key was pressed; the bytes are keel serve's own, which keelclient keeps. Off the window's
// thread: keel serve answers within 4 s.
func (w *workspace) insertSuggestion(p *pane, ref keelclient.SuggestionRef, done func(insertResult)) {
	conn := p.conn
	callErr(w, func(c context.Context) error { return conn.SuggestInsert(c, ref) }, func(err error) {
		if err != nil {
			done(insertResult{Message: err.Error()})
			return
		}
		done(insertResult{OK: true})
	})
}

// restoreWords is ⌘Z after an insert: the user's own words back, as their keystrokes, to this pane only. The one caller
// of RestoreWords.
func (w *workspace) restoreWords(p *pane, words string) {
	if !w.canSuggest(p) {
		return
	}
	if err := p.conn.RestoreWords(words); err != nil {
		w.warn(err.Error())
	}
}
