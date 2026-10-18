package shellmark

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func collect(chunks ...string) []Mark {
	var p Parser
	var out []Mark
	var base uint64
	for _, c := range chunks {
		p.Feed([]byte(c), base, func(m Mark) { out = append(out, m) })
		base += uint64(len(c))
	}
	return out
}

func kinds(ms []Mark) string {
	var b strings.Builder
	for _, m := range ms {
		b.WriteByte(byte(m.Kind))
	}
	return b.String()
}

const (
	esc = "\x1b"
	bel = "\x07"
	st  = esc + "\\"
)

func osc(s string) string { return esc + "]" + s + bel }

func TestParsesEachMark(t *testing.T) {
	in := osc("133;A") + "$ " + osc("133;B") + "ls\r\n" + osc("133;C") + "a b\r\n" + esc + "]133;D;2" + st
	ms := collect(in)
	if got := kinds(ms); got != "ABCD" {
		t.Fatalf("kinds = %q", got)
	}
	if ms[0].Start != 0 || ms[0].End != uint64(len(osc("133;A"))) {
		t.Errorf("A at %d..%d", ms[0].Start, ms[0].End)
	}
	d := ms[3]
	if !d.HasExit || d.Exit != 2 {
		t.Errorf("exit = %d %v", d.Exit, d.HasExit)
	}
	if d.End != uint64(len(in)) {
		t.Errorf("D end = %d, want %d", d.End, len(in))
	}
	if want := uint64(strings.Index(in, esc+"]133;D")); d.Start != want {
		t.Errorf("D start = %d, want %d", d.Start, want)
	}
}

func TestSplitAcrossEveryByte(t *testing.T) {
	in := "x" + osc("133;A") + "p" + osc("133;B") + "cmd" + osc("133;C") + "out" + esc + "]133;D;127" + st + osc("7;file://box/tmp/a%20b")
	whole := collect(in)
	if kinds(whole) != "ABCD7" {
		t.Fatalf("whole = %q", kinds(whole))
	}
	// Feed one byte at a time and at every two-way split: same marks.
	var one []string
	for i := 0; i < len(in); i++ {
		one = append(one, in[i:i+1])
	}
	if got := collect(one...); !equal(got, whole) {
		t.Fatalf("byte by byte = %+v, want %+v", got, whole)
	}
	for i := 0; i <= len(in); i++ {
		if got := collect(in[:i], in[i:]); !equal(got, whole) {
			t.Fatalf("split at %d = %+v", i, got)
		}
	}
	if whole[3].Exit != 127 || whole[4].Cwd != "/tmp/a b" {
		t.Errorf("exit %d cwd %q", whole[3].Exit, whole[4].Cwd)
	}
}

func equal(a, b []Mark) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestMalformed(t *testing.T) {
	cases := map[string]string{
		"unknown letter":          osc("133;Z"),
		"letter then junk":        osc("133;AB"),
		"empty":                   osc("133;"),
		"other osc":               osc("0;title") + osc("8;;https://x") + osc("52;c;aGk="),
		"control in payload":      esc + "]133;\x01A" + bel,
		"cancelled":               esc + "]133;A\x18" + "rest",
		"substituted":             esc + "]133;A\x1a",
		"unterminated":            esc + "]133;A",
		"esc not st":              esc + "]133;A" + esc + "x",
		"non ascii":               esc + "]133;A;é" + bel,
		"cwd no scheme":           osc("7;/tmp"),
		"cwd no path":             osc("7;file://host"),
		"cwd control":             osc("7;file://h/a%0Ab"),
		"cwd bad escape":          osc("7;file://h/a%zz"),
		"cwd c1":                  osc("7;file://h/a%C2%85"),
		"cwd invalid utf8":        osc("7;file://h/%ff"),
		"csi only":                esc + "[31mred" + esc + "[0m",
		"bare esc at end of data": "abc" + esc,
	}
	for name, in := range cases {
		if ms := collect(in); len(ms) != 0 {
			t.Errorf("%s: got %+v", name, ms)
		}
	}
}

func TestExitStatusIsStrict(t *testing.T) {
	cases := map[string]struct {
		exit int
		has  bool
	}{
		"133;D":                   {0, false},
		"133;D;":                  {0, false},
		"133;D;0":                 {0, true},
		"133;D;255":               {255, true},
		"133;D;1;aid=7":           {1, true},
		"133;D;-1":                {0, false},
		"133;D;+1":                {0, false},
		"133;D; 1":                {0, false},
		"133;D;1x":                {0, false},
		"133;D;2147483647":        {2147483647, true},
		"133;D;2147483648":        {0, false},
		"133;D;99999999999":       {0, false},
		"133;D;0000000001":        {1, true},
		"133;D;aid=1":             {0, false},
		"133;D;12345678901234567": {0, false},
	}
	for in, want := range cases {
		ms := collect(osc(in))
		if len(ms) != 1 || ms[0].Kind != CommandEnd {
			t.Errorf("%s: marks %+v", in, ms)
			continue
		}
		if ms[0].Exit != want.exit || ms[0].HasExit != want.has {
			t.Errorf("%s: exit %d %v, want %d %v", in, ms[0].Exit, ms[0].HasExit, want.exit, want.has)
		}
	}
	// Params after the letter are allowed on A, B and C.
	if got := kinds(collect(osc("133;A;cl=m;aid=3"), osc("133;C;"))); got != "AC" {
		t.Errorf("params: %q", got)
	}
}

func TestHugePayloadIsSkippedAndBounded(t *testing.T) {
	var p Parser
	var got []Mark
	emit := func(m Mark) { got = append(got, m) }
	p.Feed([]byte(esc+"]133;D;1"), 0, emit)
	big := bytes.Repeat([]byte("9"), 1<<20)
	for i := 0; i < 8; i++ {
		p.Feed(big, uint64(8+i*len(big)), emit)
	}
	if cap(p.buf) > 2*MaxPayload {
		t.Fatalf("buffer grew to %d", cap(p.buf))
	}
	p.Feed([]byte(bel+osc("133;A")), 0, emit)
	if kinds(got) != "A" {
		t.Fatalf("got %+v: the oversized OSC must be dropped, the next one kept", got)
	}
	// A long cwd is refused too.
	if ms := collect(osc("7;file://h/" + strings.Repeat("a", MaxCwd+1))); len(ms) != 0 {
		t.Errorf("long cwd kept")
	}
}

func TestNestedOSCStartsOver(t *testing.T) {
	// ESC ] inside an OSC abandons the outer one; the inner is read.
	ms := collect(esc + "]133;" + esc + "]133;C" + bel)
	if kinds(ms) != "C" || ms[0].Start != 6 {
		t.Fatalf("got %+v", ms)
	}
	// ESC ESC ] still starts an OSC at the second ESC.
	ms = collect(esc + esc + "]133;A" + bel)
	if kinds(ms) != "A" || ms[0].Start != 1 {
		t.Fatalf("esc esc: %+v", ms)
	}
	// Reset drops a sequence in progress.
	var p Parser
	var got []Mark
	p.Feed([]byte(esc+"]133;"), 0, func(m Mark) { got = append(got, m) })
	p.Reset()
	p.Feed([]byte("A"+bel), 6, func(m Mark) { got = append(got, m) })
	if len(got) != 0 {
		t.Fatalf("reset kept the old sequence: %+v", got)
	}
}

func feedTracker(tr *Tracker, chunks ...string) uint64 {
	var base uint64
	for _, c := range chunks {
		tr.Feed([]byte(c), base)
		base += uint64(len(c))
	}
	return base
}

func TestTrackerRecordsCommands(t *testing.T) {
	tr := NewTracker()
	clock := time.Unix(100, 0)
	tr.now = func() time.Time { clock = clock.Add(time.Second); return clock }
	in := osc("7;file://h/home/u") + osc("133;A") + "$ " + osc("133;B") + "false\r\n" + osc("133;C") + "oops\r\n" + osc("133;D;1") +
		osc("133;A") + "$ " + osc("133;B") + "\r\n" + osc("133;D;0") + // empty Enter: no C, no record
		osc("133;A") + "$ " + osc("133;B") + "sleep 9\r\n" + osc("133;C") + "zz"
	feedTracker(tr, in)
	cmds := tr.Commands()
	if len(cmds) != 2 {
		t.Fatalf("%d commands: %+v", len(cmds), cmds)
	}
	c := cmds[0]
	if !c.Done || !c.HasExit || c.Exit != 1 || c.Cwd != "/home/u" || !c.HasCommandStart {
		t.Errorf("first = %+v", c)
	}
	if got := in[c.CommandStart:c.CommandTextEnd]; got != "false\r\n" {
		t.Errorf("command text %q", got)
	}
	if got := in[c.OutputStart:c.OutputEnd]; got != "oops\r\n" {
		t.Errorf("output %q", got)
	}
	if !c.Finished.After(c.Started) {
		t.Errorf("times %v %v", c.Started, c.Finished)
	}
	last, ok := tr.Last()
	if !ok || last.Done || last.Seq != 2 {
		t.Errorf("last = %+v", last)
	}
	if in[last.OutputStart:] != "zz" {
		t.Errorf("running output %q", in[last.OutputStart:])
	}
}

func TestTrackerIgnoresStrayEnds(t *testing.T) {
	tr := NewTracker()
	feedTracker(tr, osc("133;D;5")+osc("133;C")+"x"+osc("133;D;0")+osc("133;D;9"))
	cmds := tr.Commands()
	if len(cmds) != 1 || cmds[0].Exit != 0 {
		t.Fatalf("%+v", cmds)
	}
}

func TestTrackerNestedShells(t *testing.T) {
	// Outer shell runs ssh (C), the remote shell reports a command, the
	// outer D arrives after the inner one closed: the inner record wins and
	// the outer stays unfinished; nothing panics or grows.
	tr := NewTracker()
	feedTracker(tr,
		osc("133;A")+osc("133;B")+"ssh box\r\n"+osc("133;C"),
		osc("133;A")+osc("133;B")+"make\r\n"+osc("133;C")+"err\r\n"+osc("133;D;2"),
		osc("133;A")+osc("133;B")+"exit\r\n"+osc("133;C")+osc("133;D;0"),
		osc("133;D;0"),
	)
	cmds := tr.Commands()
	if len(cmds) != 3 || cmds[0].Done || cmds[1].Exit != 2 || !cmds[2].Done {
		t.Fatalf("%+v", cmds)
	}
}

func TestTrackerIsBounded(t *testing.T) {
	tr := NewTracker()
	one := osc("133;A") + osc("133;C") + "o" + osc("133;D;0")
	feedTracker(tr, strings.Repeat(one, MaxCommands*3))
	cmds := tr.Commands()
	if len(cmds) != MaxCommands || cmds[len(cmds)-1].Seq != MaxCommands*3 {
		t.Fatalf("%d records, last seq %d", len(cmds), cmds[len(cmds)-1].Seq)
	}
	if cap(tr.cmds) > 2*MaxCommands+1 {
		t.Errorf("cap %d", cap(tr.cmds))
	}
}

func TestTrackerReset(t *testing.T) {
	tr := NewTracker()
	feedTracker(tr, osc("133;A")+osc("133;B")+"cmd"+esc+"]133")
	tr.Reset()
	// The new shell's first bytes do not finish the old sequence, and its
	// C has no B from the old prompt.
	tr.Feed([]byte(";C"+bel+osc("133;C")), 1000)
	cmds := tr.Commands()
	if len(cmds) != 1 || cmds[0].HasCommandStart || cmds[0].OutputStart != 1000+3+uint64(len(osc("133;C"))) {
		t.Fatalf("%+v", cmds)
	}
}

func FuzzParser(f *testing.F) {
	f.Add([]byte(osc("133;A")+osc("133;D;1")), 3)
	f.Add([]byte(esc+"]133;"+esc+"]7;file://h/x"+st), 1)
	f.Fuzz(func(t *testing.T, b []byte, split int) {
		tr := NewTracker()
		if split < 0 {
			split = -split
		}
		split %= len(b) + 1
		tr.Feed(b[:split], 0)
		tr.Feed(b[split:], uint64(split))
		for _, c := range tr.Commands() {
			if c.OutputStart > uint64(len(b)) || (c.Done && c.OutputEnd < c.OutputStart) {
				t.Fatalf("bad record %+v for %q", c, b)
			}
		}
		if len(tr.p.buf) > MaxPayload {
			t.Fatal("payload over cap")
		}
	})
}
