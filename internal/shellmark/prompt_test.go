package shellmark

import "testing"

// Prompt (docs/ui-redesign.md §6 rule 13): a direct type goes only to a
// shell at its prompt when the shell sends marks.
func TestTrackerPrompt(t *testing.T) {
	tr := NewTracker()
	var at uint64
	feed := func(s string) {
		tr.Feed([]byte(s), at)
		at += uint64(len(s))
	}
	check := func(what string, marks, prompt bool) {
		t.Helper()
		m, p := tr.Prompt()
		if m != marks || p != prompt {
			t.Fatalf("%s: Prompt() = %v, %v; want %v, %v", what, m, p, marks, prompt)
		}
	}
	check("no marks", false, false)
	feed("\x1b]133;A\x07$ \x1b]133;B\x07")
	check("at the prompt", true, true)
	feed("ls\r\n\x1b]133;C\x07out")
	check("running", true, false)
	feed("\x1b]133;D;0\x07")
	check("done, no prompt yet", true, false)
	feed("\x1b]133;A\x07$ ")
	check("next prompt", true, true)
	tr.Reset(at)
	check("a new shell", false, false)
}
