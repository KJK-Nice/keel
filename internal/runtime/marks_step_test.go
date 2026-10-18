package runtime

import "testing"

// A runbook step's result (docs/ui-redesign.md §16): the command whose
// output starts after the mark taken right before the step was typed, not
// an earlier one, and none until the shell reports one.
func TestCommandAfterMark(t *testing.T) {
	s := markSession(1 << 16)
	s.output([]byte("\x1b]133;A\x07$ \x1b]133;B\x07true\r\n\x1b]133;C\x07\x1b]133;D;0\x07\x1b]133;A\x07$ \x1b]133;B\x07"))
	mark := s.StreamMark()
	if _, ok := s.CommandAfter(mark); ok {
		t.Fatal("found a command before the step ran")
	}
	s.output([]byte("false\r\n\x1b]133;C\x07"))
	c, ok := s.CommandAfter(mark)
	if !ok || c.Done {
		t.Fatalf("running step: %+v %v", c, ok)
	}
	s.output([]byte("\x1b]133;D;3\x07\x1b]133;A\x07$ "))
	c, ok = s.CommandAfter(mark)
	if !ok || !c.Done || !c.HasExit || c.Exit != 3 {
		t.Fatalf("finished step: %+v %v", c, ok)
	}
	// A later command does not replace the step's own.
	s.output([]byte("\x1b]133;B\x07true\r\n\x1b]133;C\x07\x1b]133;D;0\x07"))
	if c, _ = s.CommandAfter(mark); c.Exit != 3 {
		t.Fatalf("later command won: %+v", c)
	}
}
