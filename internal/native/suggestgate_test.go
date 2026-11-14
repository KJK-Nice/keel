package native

import (
	"io"
	"math"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// The feasibility gate: three things the terminal view must allow for in-pane suggestions to work.
// Each is held here against a real terminal.Terminal (libghostty-vt) under ui.Tester.
//
//  1. The pane's prompt line is known as the user types (Terminal.Snapshot, read by cursorRow in suggestgeom.go).
//  2. Tab, Shift+Tab, Esc, the option keys and Cmd+Z are taken before the terminal only while the window says so
//     (terminal.Options.OnKey), and reach the shell otherwise.
//  3. Something can be drawn at a cell: a cell's rect is measured from the font (gridFor) and equals the rect the
//     plugin's own view reports for the caret, after a resize, a text size change and at two display scales.

// gateConn is a shell for the gate: what the terminal writes is kept (and echoed when echo is on, as a tty does); what the
// "shell" prints is put on out.
type gateConn struct {
	mu   sync.Mutex
	got  []byte
	echo bool
	out  chan []byte
	buf  []byte
	done chan struct{}
	once sync.Once
}

func newGateConn(echo bool) *gateConn {
	return &gateConn{echo: echo, out: make(chan []byte, 256), done: make(chan struct{})}
}

func (g *gateConn) Read(p []byte) (int, error) {
	for len(g.buf) == 0 {
		select {
		case g.buf = <-g.out:
		case <-g.done:
			return 0, io.EOF
		}
	}
	n := copy(p, g.buf)
	g.buf = g.buf[n:]
	return n, nil
}

func (g *gateConn) Write(p []byte) (int, error) {
	g.mu.Lock()
	g.got = append(g.got, p...)
	echo := g.echo
	g.mu.Unlock()
	if echo {
		g.out <- append([]byte(nil), p...)
	}
	return len(p), nil
}

func (g *gateConn) Close() error { g.once.Do(func() { close(g.done) }); return nil }

// take returns what the terminal wrote since the last take.
func (g *gateConn) take() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	s := string(g.got)
	g.got = nil
	return s
}

// gateTerm is a terminal on a gateConn in a tester window of w×h DIPs with the keyboard.
func gateTerm(t *testing.T, echo bool, onKey func(ui.Modifiers, ui.Key) bool, w, h int) (*terminal.Terminal, *gateConn, *ui.Tester) {
	t.Helper()
	registerFonts()
	conn := newGateConn(echo)
	term, err := terminal.New(terminal.Options{
		Conn: conn, Font: terminal.Font{Family: gateStack(), Size: 13}, Padding: terminal.Padding{X: termPadX, Y: termPadY}, OnKey: onKey,
	})
	if err != nil {
		t.Fatal("the terminal library is missing, so the gate cannot be held:", err) // never a skip: a skipped gate proves nothing
	}
	t.Cleanup(func() { term.Close() })
	tt := ui.NewTester(func(c *ui.Context) { terminal.View(c, term).Fill().AutoFocus() }, w, h)
	tt.Frame()
	return term, conn, tt
}

// waitRow waits for the cursor's row to read want, and returns how long that took after the last frame.
func waitRow(t *testing.T, term *terminal.Terminal, want string) (row int, took time.Duration) {
	t.Helper()
	start := time.Now()
	var got string
	for time.Since(start) < 2*time.Second {
		_, rows := term.Size()
		r, text, ok := cursorRow(string(term.Snapshot()), rows)
		if ok && text == want {
			return r, time.Since(start)
		}
		got = text
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("the cursor's line reads %q, want %q (screen %q)", got, want, term.Text())
	return 0, 0
}

// Gate 1: the line the cursor is on, and its row, are in the Snapshot as soon as the shell has echoed the key, with no
// frame needed in between.
func TestGateKnowsThePromptLineAsTheUserTypes(t *testing.T) {
	term, conn, tt := gateTerm(t, true, nil, 600, 200)
	conn.out <- []byte("earlier output\r\nsh-3.2$ ")
	row, _ := waitRow(t, term, "sh-3.2$")
	if row != 1 {
		t.Fatalf("the prompt is on row %d, want 1", row)
	}
	var worst time.Duration
	typed := ""
	for _, ch := range "show the files here" {
		tt.TypeKey(0, ui.KeyUnknown, string(ch))
		typed += string(ch)
		_, took := waitRow(t, term, strings.TrimRight("sh-3.2$ "+typed, " ")) // the Snapshot's rows end without spaces
		worst = max(worst, took)
	}
	if got := conn.take(); got != "show the files here" {
		t.Fatalf("the shell got %q, want the keys as typed", got)
	}
	t.Logf("the line was readable %v after the shell echoed the slowest key", worst)
	if worst > 250*time.Millisecond {
		t.Errorf("the line took %v to show", worst)
	}
	// A line the shell redraws in place (a history recall) is read as it is, and a full-screen program is not a prompt.
	conn.out <- []byte("\r\x1b[2Ksh-3.2$ ls -la")
	waitRow(t, term, "sh-3.2$ ls -la")
	conn.out <- []byte("\x1b[?1049h\x1b[Hless")
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); time.Sleep(time.Millisecond) {
		if _, _, ok := cursorRow(string(term.Snapshot()), 24); !ok {
			return
		}
	}
	t.Fatal("the alternate screen is read as a prompt line")
}

type seenKey struct {
	mods ui.Modifiers
	key  ui.Key
}

// settledTake takes what the terminal wrote. The terminal writes a key from its own goroutine, so a take straight after a
// frame can run before the write (and the late bytes then land in the next case). With want set it waits for want; with
// want empty it gives a late write a few frames to show itself.
func settledTake(tt *ui.Tester, conn *gateConn, want string) string {
	for i := 0; i < 100; i++ {
		conn.mu.Lock()
		got := string(conn.got)
		conn.mu.Unlock()
		if want != "" && got == want {
			break
		}
		if want == "" && i >= 5 {
			break
		}
		tt.Frame()
		time.Sleep(2 * time.Millisecond)
	}
	return conn.take()
}

// Gate 2: OnKey sees a key before the terminal, takes it (and then nothing reaches the shell) or leaves it (and the shell
// gets it as usual), by what the window says at that moment.
func TestGateTakesKeysOnlyWhileTheWindowSaysSo(t *testing.T) {
	var take bool
	var seen []seenKey
	_, conn, tt := gateTerm(t, false, func(m ui.Modifiers, k ui.Key) bool {
		seen = append(seen, seenKey{m, k})
		return take
	}, 400, 200)
	keys := []struct {
		name  string
		press func()
		mods  ui.Modifiers
		key   ui.Key
		sent  string // what the shell gets when the key is left
	}{
		{"Tab", func() { tt.Key(0, ui.KeyTab) }, 0, ui.KeyTab, "\t"},
		{"Shift+Tab", func() { tt.Key(ui.Shift, ui.KeyTab) }, ui.Shift, ui.KeyTab, "\x1b[Z"},
		{"Esc", func() { tt.Key(0, ui.KeyEscape) }, 0, ui.KeyEscape, "\x1b"},
		{"Up", func() { tt.Key(0, ui.KeyUp) }, 0, ui.KeyUp, "\x1b[A"},
		{"Down", func() { tt.Key(0, ui.KeyDown) }, 0, ui.KeyDown, "\x1b[B"},
		{"1", func() { tt.TypeKey(0, ui.Key1, "1") }, 0, ui.Key1, "1"},
	}
	if runtime.GOOS == "darwin" { // a Command key never reaches the program on macOS; ui.Cmd is Ctrl elsewhere, and Ctrl+Z is the shell's (^Z)
		keys = append(keys, struct {
			name  string
			press func()
			mods  ui.Modifiers
			key   ui.Key
			sent  string
		}{"Cmd+Z", func() { tt.Key(ui.Cmd, ui.KeyZ) }, ui.Cmd, ui.KeyZ, ""})
	}
	for _, k := range keys {
		seen = nil
		take = true
		k.press()
		tt.Frame()
		tt.Frame()
		got := settledTake(tt, conn, "")
		if got != "" {
			t.Errorf("%s taken: the shell still got %q", k.name, got)
		}
		if len(seen) != 1 || seen[0] != (seenKey{k.mods, k.key}) {
			t.Errorf("%s taken: OnKey saw %v, want one %v", k.name, seen, seenKey{k.mods, k.key})
		}
		seen = nil
		take = false
		k.press()
		tt.Frame()
		tt.Frame() // the text of a key is delivered a frame after the key
		if got := settledTake(tt, conn, k.sent); got != k.sent {
			t.Errorf("%s left alone: the shell got %q, want %q", k.name, got, k.sent)
		}
		if len(seen) != 1 {
			t.Errorf("%s left alone: OnKey saw %v", k.name, seen)
		}
	}
}

// caretCell is the cell the plugin's view reports for the caret (where an input method would compose), as a rect.
func caretCell(t *testing.T, tt *ui.Tester) ui.Rect {
	t.Helper()
	r, ok := tt.TextCaret()
	if !ok {
		t.Fatal("the terminal view reports no caret")
	}
	return r
}

// gateStack is the terminal's family list in the gate tests: Keel's default (JetBrains Mono) on macOS, where the window registers
// it; the system mono elsewhere. On Linux the window registers no bundled font (registerFonts), but the fontconfig of a test
// process holds the shipped woff2 files its font tests registered, under their own family names too, and those measure about
// 1.9 times too wide: the gate must not depend on which tests ran before it.
func gateStack() string {
	if runtime.GOOS == "darwin" {
		return termStack(fontJetBrains)
	}
	return termStack("system")
}

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 0.01 }

// Gate 3: a cell's rect, measured from the font, is the plugin's own: the caret's rect is the cursor's cell, and the same
// numbers put a mark on any cell of the line. Held across window sizes, text sizes and display scales.
func TestGateCellRectsAreThePluginsOwn(t *testing.T) {
	for _, sc := range []float32{1, 2} {
		for _, size := range []float32{13, 17, 10} {
			term, conn, tt := gateTerm(t, false, nil, 700, 260)
			tt.SetScale(sc)
			term.SetFont(terminal.Font{Family: gateStack(), Size: size})
			tt.Frame()
			conn.out <- []byte("a\r\nsh-3.2$ show me the biggest files")
			waitRow(t, term, "sh-3.2$ show me the biggest files")
			tt.Frame()
			check := func(what string) {
				t.Helper()
				cols, rows := term.Size()
				snap := string(term.Snapshot())
				row, text, ok := cursorRow(snap, rows)
				m := snapCursor.FindStringSubmatch(snap[strings.Index(snap, "\x1b[H"):])
				if !ok || m == nil {
					// Diagnostics: a line that exactly fills the columns leaves the cursor on the next row (pending wrap),
					// where there is no text line to read. The cell width the plugin used decides the columns: log it, with the
					// widths this process measures for the family (a cell of 13.8 px at size 13 is not JetBrains Mono's 7.8).
					gg := gridFor(gateStack(), size, sc)
					t.Fatalf("%s: the cursor is not placed in %q (scale %v, size %v, %d cols x %d rows, row %d ok %v, cell %.2f x %.2f, \"MMMMMMMMMM\" is %.2f wide in the stack, %.2f in monospace)",
						what, snap, sc, size, cols, rows, row, ok, gg.cw, gg.ch,
						probeWidth("MMMMMMMMMM", size, gateStack()), probeWidth("MMMMMMMMMM", size, "monospace"))
				}
				col, _ := strconv.Atoi(m[2])
				g := gridFor(gateStack(), size, sc)
				want, got := g.at(row, col-1, 1), caretCell(t, tt)
				if !near(want.X, got.X) || !near(want.Y, got.Y) || !near(want.W, got.W) || !near(want.H, got.H) {
					t.Errorf("%s (scale %v, size %v, %d cols x %d rows, %q): measured %v, the plugin's caret %v", what, sc, size, cols, rows, text, want, got)
				}
			}
			check("first")
			tt.SetSize(520, 330) // a resize
			tt.Frame()
			check("after a resize")
			term.SetFont(terminal.Font{Family: gateStack(), Size: size + 3}) // a text size change
			size += 3
			tt.Frame()
			tt.Frame() // the font is laid out when the view is next painted
			check("after a text size change")
		}
	}
}
