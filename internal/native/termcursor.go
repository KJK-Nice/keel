package native

import "github.com/egoist/mygo/plugins/terminal"

// Settings · Terminal · Cursor and Keyboard: the cursor's shape and blink, and on macOS what Option is. The plugin takes
// them when a terminal is made (Options.Cursor, NoBlink, OptionAsAlt) and has no setter yet, so they apply to panes
// opened afterwards, as Right-click does. Programs may still change the cursor's shape (DECSCUSR). Kept in
// keel-native.json ("cursorStyle", "cursorBlink", "optionKey").

const (
	cursorBlock, cursorBar, cursorUnderline = "block", "bar", "underline"
	optionSpecial, optionAlt                = "special", "alt"
)

var (
	cursorStyles = [][2]string{{cursorBlock, "Block"}, {cursorBar, "Bar"}, {cursorUnderline, "Underline"}}
	optionModes  = [][2]string{{optionSpecial, "Characters"}, {optionAlt, "Alt (Meta)"}}
)

// validChoice is v when it is one of opts, else the first.
func validChoice(v string, opts [][2]string) string {
	for _, o := range opts {
		if o[0] == v {
			return v
		}
	}
	return opts[0][0]
}

// cursorStyleOf is the plugin's style of a value.
func cursorStyleOf(v string) terminal.CursorStyle {
	switch v {
	case cursorBar:
		return terminal.CursorBar
	case cursorUnderline:
		return terminal.CursorUnderline
	}
	return terminal.CursorBlock
}

// setCursorStyle is the Cursor style row: panes opened from now on.
func (w *workspace) setCursorStyle(v string) {
	if v = validChoice(v, cursorStyles); v != w.prefs.CursorStyle {
		w.prefs.CursorStyle = v
		w.setPref(keyCursorStyle)
	}
}

// setCursorBlink is the Blink row: panes opened from now on.
func (w *workspace) setCursorBlink(on bool) {
	if on != w.prefs.CursorBlink {
		w.prefs.CursorBlink = on
		w.setPref(keyCursorBlink)
	}
}

// setOptionKey is the Option key row (macOS): panes opened from now on.
func (w *workspace) setOptionKey(v string) {
	if v = validChoice(v, optionModes); v != w.prefs.OptionKey {
		w.prefs.OptionKey = v
		w.setPref(keyOptionKey)
	}
}
