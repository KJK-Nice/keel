package shellmark

import "time"

// MaxCommands is how many command records a pane keeps; the oldest go
// first. The record holds offsets, not text, so this bounds memory
// whatever the stream holds.
const MaxCommands = 64

// Command is one command a shell reported with OSC 133. Offsets are
// positions in the pane's output stream (the scrollback ring's write
// counter): the text is read from the ring while it still holds it.
type Command struct {
	Seq uint64
	// PromptStart is where the prompt began (the A mark), or where C was
	// seen when the shell sent no A.
	PromptStart uint64
	// CommandStart is just past the B mark: the command as typed (and
	// echoed) runs from here to CommandTextEnd. HasCommandStart is false
	// when the shell sent no B (fish, many prompt themes).
	CommandStart    uint64
	HasCommandStart bool
	// CommandTextEnd is where the C mark began: the typed text ends there.
	CommandTextEnd uint64
	// OutputStart is just past the C mark; OutputEnd is where the D mark
	// began. Done is false while the command runs (or when it never
	// reported an end: a dropped connection, a nested shell's prompt).
	OutputStart uint64
	OutputEnd   uint64
	Done        bool
	Exit        int
	HasExit     bool
	// Cwd is the last OSC 7 directory seen before the command started.
	Cwd      string
	Started  time.Time
	Finished time.Time
}

// Tracker turns marks into command records. A command exists once its
// output starts (C); a prompt with no command (Enter on an empty line)
// leaves no record. Marks are flat: a nested shell's marks (ssh from a
// shell that has integration into one that has it too) interleave with
// the outer ones, and the innermost complete command wins. An end (D)
// closes the latest command only if it is still open, so a stray or
// repeated D changes nothing. Not safe for concurrent use; the runtime
// calls it under the session lock.
type Tracker struct {
	p       Parser
	pending *Command // prompt seen (A or B), output not started yet
	cmds    []Command
	cwd     string
	seq     uint64
	now     func() time.Time
}

// NewTracker returns an empty tracker.
func NewTracker() *Tracker { return &Tracker{now: time.Now} }

// Feed scans b, whose first byte is at stream offset base.
func (t *Tracker) Feed(b []byte, base uint64) {
	t.p.Feed(b, base, t.mark)
}

// Reset is called when a new shell starts on the same pane (reconnect): a
// sequence or prompt in progress belongs to the old shell. Records stay.
func (t *Tracker) Reset() {
	t.p.Reset()
	t.pending = nil
}

func (t *Tracker) mark(m Mark) {
	switch m.Kind {
	case PromptStart:
		t.pending = &Command{PromptStart: m.Start}
	case CommandStart:
		if t.pending == nil {
			t.pending = &Command{PromptStart: m.Start}
		}
		t.pending.CommandStart = m.End
		t.pending.HasCommandStart = true
	case OutputStart:
		c := Command{PromptStart: m.Start}
		if t.pending != nil {
			c = *t.pending
		}
		t.pending = nil
		t.seq++
		c.Seq = t.seq
		c.CommandTextEnd = m.Start
		c.OutputStart = m.End
		c.Cwd = t.cwd
		c.Started = t.now()
		t.cmds = append(t.cmds, c)
		if len(t.cmds) > MaxCommands {
			t.cmds = append(t.cmds[:0], t.cmds[len(t.cmds)-MaxCommands:]...)
		}
	case CommandEnd:
		if len(t.cmds) == 0 {
			return
		}
		c := &t.cmds[len(t.cmds)-1]
		if c.Done || m.Start < c.OutputStart {
			return
		}
		c.OutputEnd = m.Start
		c.Done = true
		c.Exit, c.HasExit = m.Exit, m.HasExit
		c.Finished = t.now()
	case Cwd:
		t.cwd = m.Cwd
	}
}

// Last returns the latest command record, finished or not.
func (t *Tracker) Last() (Command, bool) {
	if len(t.cmds) == 0 {
		return Command{}, false
	}
	return t.cmds[len(t.cmds)-1], true
}

// Commands returns a copy of the records, oldest first.
func (t *Tracker) Commands() []Command {
	return append([]Command(nil), t.cmds...)
}

// Cwd returns the last directory reported with OSC 7, or "".
func (t *Tracker) Cwd() string { return t.cwd }

// Block is a command's text, read from the scrollback for the agent. The
// runtime fills it from a Command and the ring; text is raw PTY output
// with the scrollback mask applied, so it still needs the agent's
// Normalize and Redactor before it may leave the machine.
type Block struct {
	Command string
	Output  string
	Exit    int
	HasExit bool
	Done    bool
	Cwd     string
	// Truncated is true when the start of the output had already left the
	// scrollback ring.
	Truncated bool
}
