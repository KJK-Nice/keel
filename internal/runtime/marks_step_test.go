package runtime

import "testing"

// A runbook step's result (docs/ui-redesign.md §16): the command whose
// output starts after the mark taken right before the step was typed, not
// an earlier one, and none until the shell reports one.
func TestCommandAfterMark(t *testing.T) {
	s := markSession(1 << 16)
	s.output([]byte("\x1b]133;A\x07$ \x1b]133;B\x07true\r\n\x1b]133;C\x07\x1b]133;D;0\x07\x1b]133;A\x07$ \x1b]133;B\x07"))
	mark := s.StreamMark()
	if got := s.CommandsAfter(mark); len(got) != 0 {
		t.Fatal("found a command before the step ran")
	}
	s.output([]byte("false\r\n\x1b]133;C\x07"))
	got := s.CommandsAfter(mark)
	if len(got) != 1 || got[0].Done {
		t.Fatalf("running step: %+v", got)
	}
	s.output([]byte("\x1b]133;D;3\x07\x1b]133;A\x07$ "))
	got = s.CommandsAfter(mark)
	if len(got) != 1 || !got[0].Done || !got[0].HasExit || got[0].Exit != 3 {
		t.Fatalf("finished step: %+v", got)
	}
	// A later command comes after the step's own, in order.
	s.output([]byte("\x1b]133;B\x07true\r\n\x1b]133;C\x07\x1b]133;D;0\x07"))
	if got = s.CommandsAfter(mark); len(got) != 2 || got[0].Exit != 3 || got[1].Exit != 0 {
		t.Fatalf("commands after the mark: %+v", got)
	}
}
