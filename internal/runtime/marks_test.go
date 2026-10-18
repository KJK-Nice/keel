package runtime

import (
	"strings"
	"testing"

	"keel/internal/shellmark"
)

func markSession(ringSize int) *Session {
	return &Session{buf: newRing(ringSize), marks: shellmark.NewTracker(), clients: map[uint64]*clientConn{}}
}

func TestRingRange(t *testing.T) {
	r := newRing(8)
	r.Write([]byte("abcdefghij")) // holds cdefghij, offsets 2..10
	cases := []struct {
		from, to uint64
		want     string
		cut      bool
	}{
		{2, 10, "cdefghij", false},
		{4, 6, "ef", false},
		{0, 5, "cde", true},
		{8, 99, "ij", false},
		{10, 12, "", false},
		{6, 4, "", false},
	}
	for _, c := range cases {
		got, cut := r.Range(c.from, c.to)
		if string(got) != c.want || cut != c.cut {
			t.Errorf("Range(%d,%d) = %q %v, want %q %v", c.from, c.to, got, cut, c.want, c.cut)
		}
	}
}

func TestLastCommandFromMarks(t *testing.T) {
	s := markSession(1 << 16)
	if _, ok := s.LastCommand(); ok {
		t.Fatal("no marks yet, got a command")
	}
	// Split mid-sequence, as PTY reads do.
	chunks := []string{
		"\x1b]7;file://box/srv/app\x07\x1b]133;A\x07$ \x1b]13",
		"3;B\x07make test\r\n\x1b]133;C\x07FAIL pkg password=hunter2\r\n",
		"exit status 1\r\n\x1b]133;D;1\x07\x1b]133;A\x07$ ",
	}
	for _, c := range chunks {
		s.output([]byte(c))
	}
	// The ring holds the bytes unchanged: marks are read, never removed.
	if got := string(s.buf.Bytes()); got != strings.Join(chunks, "") {
		t.Fatalf("ring changed: %q", got)
	}
	blk, ok := s.LastCommand()
	if !ok {
		t.Fatal("no command")
	}
	if blk.Command != "make test\r\n" || !blk.Done || !blk.HasExit || blk.Exit != 1 || blk.Cwd != "/srv/app" || blk.Truncated {
		t.Fatalf("block = %+v", blk)
	}
	// The scrollback mask applies, as for ReadScrollback.
	if strings.Contains(blk.Output, "hunter2") || !strings.Contains(blk.Output, "exit status 1") {
		t.Fatalf("output = %q", blk.Output)
	}

	// A running command reads to the end of what was written.
	s.output([]byte("\x1b]133;B\x07tail -f log\r\n\x1b]133;C\x07line 1\r\n"))
	blk, ok = s.LastCommand()
	if !ok || blk.Done || blk.HasExit || blk.Output != "line 1\r\n" {
		t.Fatalf("running = %+v", blk)
	}
}

func TestLastCommandOutOfRing(t *testing.T) {
	s := markSession(64)
	s.output([]byte("\x1b]133;C\x07" + strings.Repeat("x", 100)))
	blk, ok := s.LastCommand()
	if !ok || !blk.Truncated || len(blk.Output) != 64 {
		t.Fatalf("partly out = %+v", blk)
	}
	s.output([]byte("\x1b]133;D;0\x07" + strings.Repeat("y", 100)))
	if blk, ok := s.LastCommand(); ok {
		t.Fatalf("output gone from the ring, got %+v", blk)
	}
}

func TestLastCommandCapsCommandText(t *testing.T) {
	s := markSession(1 << 16)
	s.output([]byte("\x1b]133;B\x07" + strings.Repeat("a", 3*maxCommandText) + "\x1b]133;C\x07ok\x1b]133;D;0\x07"))
	blk, ok := s.LastCommand()
	if !ok || len(blk.Command) != maxCommandText {
		t.Fatalf("command %d bytes", len(blk.Command))
	}
}
