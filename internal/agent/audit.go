package agent

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"keel/internal/agent/redact"
	"keel/internal/applog"
	"keel/internal/shellmark"
)

// Logger is where audit entries go. *applog.Book (keel.log) satisfies it.
// The error says the entry was not written.
type Logger interface {
	Append(applog.Entry) error
}

// ScrollbackSource reads a pane's history. *runtime.Runtime satisfies it;
// the agent depends on this one read method only.
type ScrollbackSource interface {
	ReadScrollback(sessionID string) ([]byte, error)
}

// CommandSource is a ScrollbackSource that also knows a pane's last
// command from its shell-integration marks (OSC 133). keel serve's pane
// source satisfies it; a source without it reads the tail as before.
type CommandSource interface {
	LastCommand(sessionID string) (shellmark.Block, bool)
}

// maxExcerptBytes caps one pane's excerpt after redaction (1000 lines of
// a wide terminal).
const maxExcerptBytes = 256 * 1024

// maxCommandLines caps the command line(s) shown above a block's output.
const maxCommandLines = 20

// Auditor runs every agent read and insert, and writes each one to
// keel.log (§6.7). There is no way to read a pane or authorize an insert
// through this package without an entry being written.
type Auditor struct {
	log Logger
	// missed are entries keel.log did not take, oldest first (under mu).
	mu     sync.Mutex
	missed []applog.Entry
}

// ErrAuditLog refuses a read, a send or a write into a pane while keel.log
// cannot be written (§6.7).
var ErrAuditLog = errors.New("agent: keel.log cannot be written, so the agent reads, sends and types nothing; check the disk and the log file")

// maxMissed bounds the entries kept while keel.log cannot be written.
const maxMissed = 256

// gate writes the entry for something about to happen: a pane read about
// to be returned, text about to be sent, bytes about to be typed. When it
// cannot be written (or an earlier entry is still owed) it returns
// ErrAuditLog and the caller must not go on; the entry is dropped, since
// what it describes did not happen.
func (a *Auditor) gate(e applog.Entry) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.flushLocked(); err != nil {
		return err
	}
	if err := a.log.Append(e); err != nil {
		return fmt.Errorf("%w: %v", ErrAuditLog, err)
	}
	return nil
}

// note writes the entry for something that already happened (bytes that
// were written, a suggestion made, a note saved). When it cannot be
// written the entry is kept, with its time, and written before any later
// one; until then gate refuses everything.
func (a *Auditor) note(e applog.Entry) error {
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.missed) < maxMissed {
		a.missed = append(a.missed, e)
	}
	return a.flushLocked()
}

// Event logs something the agent service did that reads, sends and types
// nothing, under kind (EventAgent, EventNote): a message and a count,
// never content.
func (a *Auditor) Event(kind, source, message string, count int) {
	_ = a.note(applog.Entry{Kind: kind, Source: source, Message: message, Count: count})
}

func (a *Auditor) flushLocked() error {
	for len(a.missed) > 0 {
		if err := a.log.Append(a.missed[0]); err != nil {
			return fmt.Errorf("%w: %v", ErrAuditLog, err)
		}
		a.missed = a.missed[1:]
	}
	return nil
}

// NewAuditor returns an Auditor that logs to l. A nil l is an error: an
// unlogged read must not be possible.
func NewAuditor(l Logger) (*Auditor, error) {
	if l == nil {
		return nil, ErrNoAuditLog
	}
	return &Auditor{log: l}, nil
}

// Excerpt is the redacted tail of one pane, ready for a provider request.
type Excerpt struct {
	Pane   Pane
	Lines  int
	Text   redact.Safe
	Report redact.Report
	// LastCommand is set when the excerpt is the pane's last command (from
	// OSC 133 marks) rather than its tail; Command says how it ended.
	LastCommand bool
	Command     CommandEnd
}

// CommandEnd is how a command read from shell-integration marks ended, as
// the shell reported it. It is untrusted output like the text itself.
type CommandEnd struct {
	Running   bool
	Abandoned bool
	Exit      int
	HasExit   bool
}

// Label is the short form used in keel.log and the trace: "exit 1",
// "exit status not reported", "still running", "ended when the shell went
// away".
func (c CommandEnd) Label() string {
	switch {
	case c.Running:
		return "still running"
	case c.Abandoned:
		return "ended when the shell went away"
	case c.HasExit:
		return fmt.Sprintf("exit %d", c.Exit)
	default:
		return "exit status not reported"
	}
}

// reported is the value on the excerpt's "reported exit:" line.
func (c CommandEnd) reported() string {
	switch {
	case c.Running:
		return "none yet (still running)"
	case c.Abandoned:
		return "none (the shell went away before it ended)"
	case c.HasExit:
		return fmt.Sprintf("%d", c.Exit)
	default:
		return "not reported"
	}
}

// ReadPane reads a pane in scope, redacts it with r (shared across the
// request), and logs agent.read with the pane, host, line count and
// redaction counts. A pane out of scope is refused before anything is
// read. When the pane's shell reports its commands (OSC 133, src is a
// CommandSource), it reads the last command, its output (at most lines
// lines), its exit status and what the pane printed after it; otherwise
// the last lines of the scrollback.
//
// source is "" for the agent panel; another reader names itself
// ("claude-code" for keel mcp), and the agent.read entry records it
// (Source, and "for <source>" in the message).
func (a *Auditor) ReadPane(src ScrollbackSource, scope Scope, p Pane, lines int, r *redact.Redactor, source string) (Excerpt, error) {
	if !scope.Allows(p) {
		return Excerpt{}, ErrNotReadable
	}
	if r == nil {
		return Excerpt{}, fmt.Errorf("agent: no redactor")
	}
	if cs, ok := src.(CommandSource); ok {
		if blk, ok := cs.LastCommand(p.SessionID); ok {
			return a.readCommand(p, blk, lines, r, source)
		}
	}
	return a.readPane(src, scope, p, lines, r, asWritten, source)
}

// readCommand turns a command block into an excerpt, as one plain text
// that goes through r like any pane text. Logged as agent.read.
//
// Every field the marks gave is labelled "reported": the command line, the
// exit status, the cwd and where the output starts and ends come from the
// remote's own output, and a program (cat of a crafted file) can print
// fake ones. So the excerpt never uses a prompt-like "$ " that would read
// as Keel's own statement. And the excerpt always covers at least the
// pane's plain tail (the last lines setting): what the marked parts do not
// show of it is shown first, as the pane's earlier text, raw. So marks a
// program printed (a fake end, then a fake command and output) cannot
// push real text out of what the agent reads; at worst they change how
// it is labelled. In the normal case that earlier part is just the
// context before the command, and nothing is sent twice.
func (a *Auditor) readCommand(p Pane, blk shellmark.Block, lines int, r *redact.Redactor, source string) (Excerpt, error) {
	end := CommandEnd{Running: !blk.Done, Abandoned: blk.Abandoned, Exit: blk.Exit, HasExit: blk.HasExit}
	budget := ClampLines(lines)
	var marked strings.Builder
	if blk.Cwd != "" {
		fmt.Fprintf(&marked, "reported cwd: %s\n", oneLine(blk.Cwd))
	}
	cmd := commandText(blk.Command)
	cmdLines := 0
	if cmd == "" {
		marked.WriteString("reported command: (not reported)\n")
	} else {
		for _, ln := range strings.Split(cmd, "\n") {
			marked.WriteString("reported command: " + ln + "\n")
			cmdLines++
		}
	}
	marked.WriteString("reported output:\n")
	out, n := tailPart(blk.Output, budget, blk.Truncated, &marked)
	if out != "" {
		marked.WriteString(out + "\n")
	}
	fmt.Fprintf(&marked, "reported exit: %s\n", end.reported())
	afterLines := 0
	if strings.TrimSpace(redact.Normalize(blk.After)) != "" {
		marked.WriteString("output after that command (the pane's latest text):\n")
		var after string
		after, afterLines = tailPart(blk.After, budget, blk.AfterTruncated, &marked)
		marked.WriteString(after + "\n")
	}
	// The lines of the plain tail the marked parts do not cover.
	var b strings.Builder
	earlierLines := 0
	if k := budget - cmdLines - n - afterLines; k > 0 && !blk.Truncated {
		var earlier string
		earlier, earlierLines = LastLines(redact.Normalize(blk.Before), k)
		if strings.TrimSpace(earlier) != "" {
			b.WriteString("pane text before the reported command (raw, as printed):\n")
			b.WriteString(earlier + "\n")
		} else {
			earlierLines = 0
		}
	}
	b.WriteString(marked.String())
	before := r.Report()
	safe := r.Text(strings.TrimRight(b.String(), "\n")).Tail(maxExcerptBytes)
	rep := Diff(before, r.Report())
	extra := ""
	if earlierLines > 0 {
		extra += fmt.Sprintf(" · %d before it", earlierLines)
	}
	if afterLines > 0 {
		extra += fmt.Sprintf(" · %d after it", afterLines)
	}
	if err := a.gate(applog.Entry{
		Kind:       EventRead,
		Message:    fmt.Sprintf("agent read %s%s · last command, %s · %d lines of output%s · %s", hostLabel(p), forSource(source), end.Label(), n, extra, redactionNote(rep)),
		HostID:     p.HostID,
		SessionID:  p.SessionID,
		Lines:      earlierLines + n + afterLines,
		Redactions: rep.Strings(),
		Source:     source,
	}); err != nil {
		return Excerpt{}, err
	}
	return Excerpt{Pane: p, Lines: n, Text: safe, Report: rep, LastCommand: true, Command: end}, nil
}

// tailPart is the last lines of raw PTY text as plain text, writing a
// "(earlier text not included)" line to b first when it was cut.
func tailPart(raw string, lines int, truncated bool, b *strings.Builder) (string, int) {
	norm := strings.TrimRight(redact.Normalize(raw), "\n")
	out, n := LastLines(norm, lines)
	if truncated || (norm != "" && n < strings.Count(norm, "\n")+1) {
		b.WriteString("(earlier text not included)\n")
	}
	return out, n
}

// commandText is the typed command from the region between the B and C
// marks: plain text, blank lines dropped, at most maxCommandLines lines.
func commandText(raw string) string {
	var out []string
	for _, ln := range strings.Split(redact.Normalize(raw), "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			out = append(out, ln)
		}
		if len(out) == maxCommandLines {
			break
		}
	}
	return strings.Join(out, "\n")
}

func oneLine(s string) string {
	return strings.NewReplacer("\n", " ", "\r", " ").Replace(s)
}

// ReadPaneScreen is ReadPane for reading the commands typed in a pane
// (history-first suggestions): the raw output goes through CommandLines
// first, so a line that was redrawn, moved over or erased is not read as
// it was first written. Logged the same way.
func (a *Auditor) ReadPaneScreen(src ScrollbackSource, scope Scope, p Pane, lines int, r *redact.Redactor) (Excerpt, error) {
	return a.readPane(src, scope, p, lines, r, CommandLines, "")
}

var csiRe = regexp.MustCompile(`\x1b\[([0-?]*)[ -/]*([@-~])`)

// CommandLines prepares raw PTY output for reading commands from it. It
// does not emulate a terminal, so it does not claim to give what the
// screen showed; it is conservative instead, and blanks (keeps as an
// empty line, so line positions hold) every line it cannot read plainly:
//
//   - of each line it keeps only the text after its last lone carriage
//     return (a shell redrawing the prompt line: line editing, a cleared
//     line, an inserted suggestion);
//   - a line holding a backspace, a cursor move (up, down, left, right, to
//     a column or a position), a scroll, an insert or delete of characters
//     or lines, or an erase other than "to the end of the line" is
//     blanked;
//   - a cursor-up by n (CSI n A, CSI n F, ESC M) blanks the n lines above
//     too, since it can rewrite them ("ESC[1A ESC[2K" erases the line
//     above);
//   - an erase of the display above the cursor or all of it (CSI 1J, 2J,
//     3J) or a cursor position (CSI H, f, d) blanks every line before it.
//
// The last line, the prompt line being edited, keeps its text (only its
// prompt is read from it), but its moves still blank the lines above.
func CommandLines(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	blank := make([]bool, len(lines))
	last := len(lines) - 1
	for i, ln := range lines {
		if k := strings.LastIndexByte(strings.TrimRight(ln, "\r"), '\r'); k >= 0 {
			ln = ln[k+1:]
			lines[i] = ln
		}
		up, above, touched := motions(ln)
		if touched && i != last {
			blank[i] = true
		}
		for j := max(i-up, 0); j < i; j++ {
			blank[j] = true
		}
		if above {
			for j := 0; j < i; j++ {
				blank[j] = true
			}
		}
	}
	for i := range lines {
		if blank[i] {
			lines[i] = ""
		}
	}
	return strings.Join(lines, "\n")
}

// motions reads the moves in one line of raw output: how many lines up the
// cursor goes, whether anything before it may be rewritten, and whether
// the line itself was edited in place.
func motions(ln string) (up int, above, touched bool) {
	if strings.ContainsRune(ln, '\b') {
		touched = true
	}
	if n := strings.Count(ln, "\x1bM"); n > 0 {
		up += n
		touched = true
	}
	for _, m := range csiRe.FindAllStringSubmatch(ln, -1) {
		param, final := m[1], m[2]
		n := 1
		if v, err := strconv.Atoi(param); err == nil && v > 0 {
			n = v
		}
		switch final {
		case "A", "F":
			up += n
			touched = true
		case "H", "f", "d":
			above, touched = true, true
		case "J":
			touched = true
			if param == "1" || param == "2" || param == "3" {
				above = true
			}
		case "K":
			if param == "1" || param == "2" {
				touched = true
			}
		case "B", "C", "D", "E", "G", "`", "a", "e", "S", "T", "L", "M", "P", "@", "X":
			touched = true
		}
	}
	return up, above, touched
}

// asWritten reads a pane's output as the shell wrote it.
func asWritten(raw string) string { return raw }

// readPane reads a pane's scrollback through view (asWritten, or
// CommandLines), redacts it and logs the read.
func (a *Auditor) readPane(src ScrollbackSource, scope Scope, p Pane, lines int, r *redact.Redactor, view func(raw string) string, source string) (Excerpt, error) {
	if !scope.Allows(p) {
		return Excerpt{}, ErrNotReadable
	}
	if r == nil {
		return Excerpt{}, fmt.Errorf("agent: no redactor")
	}
	raw, err := src.ReadScrollback(p.SessionID)
	if err != nil {
		return Excerpt{}, err
	}
	text, n := LastLines(redact.Normalize(view(string(raw))), ClampLines(lines))
	before := r.Report()
	safe := r.Text(text).Tail(maxExcerptBytes)
	rep := Diff(before, r.Report())
	if err := a.gate(applog.Entry{
		Kind:       EventRead,
		Message:    fmt.Sprintf("agent read %s%s · last %d lines · %s", hostLabel(p), forSource(source), n, redactionNote(rep)),
		HostID:     p.HostID,
		SessionID:  p.SessionID,
		Lines:      n,
		Redactions: rep.Strings(),
		Source:     source,
	}); err != nil {
		return Excerpt{}, err
	}
	return Excerpt{Pane: p, Lines: n, Text: safe, Report: rep}, nil
}

// Timing is how long an in-pane suggestion's calls took. Asked is false
// when Jev was not asked (no key, or every candidate was destructive by
// the code rule). Pick is the history pick (Jev choosing among commands
// already run); Model is 0 when that pick was confident and the model was
// not called.
type Timing struct {
	Model time.Duration
	Pick  time.Duration
	Jev   time.Duration
	Asked bool
}

// Suggest logs agent.suggest: a suggestion was produced for a pane. rep is
// what was redacted from the text sent for it; risk is the label shown
// (Read-only, Changes state, Destructive); source is "" for the agent
// panel, else who or what it came from: "generated" (the model provider),
// "history" (a command from the pane's scrollback), "snippet" (a snippet
// marked agent-usable), "claude-code" (keel mcp). t is the time an in-pane
// suggestion's calls took, so a slow one shows where the time went; the
// zero Timing otherwise. It logs no content.
func (a *Auditor) Suggest(p Pane, rep redact.Report, risk, source string, t Timing) {
	from := ""
	if source != "" {
		from = " · " + source
	}
	took := ""
	if t.Pick > 0 {
		took += fmt.Sprintf(" · history pick %d ms", t.Pick.Milliseconds())
	}
	if t.Model > 0 {
		took += fmt.Sprintf(" · model %d ms", t.Model.Milliseconds())
	}
	if (t.Model > 0 || t.Pick > 0) && t.Asked {
		took += fmt.Sprintf(" · Jev %d ms", t.Jev.Milliseconds())
	}
	e := applog.Entry{
		Kind:       EventSuggest,
		Message:    fmt.Sprintf("agent suggested a command for %s · %s%s · %s%s", hostLabel(p), riskLabel(risk), from, redactionNote(rep), took),
		HostID:     p.HostID,
		SessionID:  p.SessionID,
		Redactions: rep.Strings(),
		Source:     source,
	}
	// At least 1 ms when made, so a fast call is never read as "not made".
	if t.Model > 0 {
		e.ModelMs = max(t.Model.Milliseconds(), 1)
	}
	if t.Asked {
		e.JevMs = max(t.Jev.Milliseconds(), 1)
	}
	// Nothing to refuse here: the entry is owed until it is written.
	_ = a.note(e)
}

// Egress describes text sent off the machine, for its agent.read entry.
type Egress struct {
	// Pane is the pane the text is about; the zero Pane when there is none
	// (a question with no pane in scope).
	Pane Pane
	// What was sent ("the words on the prompt line", "2 candidate
	// commands"), and N lines or commands of it.
	What string
	N    int
	// Report is what was redacted from it.
	Report redact.Report
	// To whom ("TypeSafe Jev", "Gemini"), Why ("to rate their risk"), and
	// Source, the provider's or the suggestion source's id.
	To, Why, Source string
	// Summary: the entry stands for sends already made (the rest of a
	// detection window, §7), so it is owed rather than refused when
	// keel.log cannot take it now.
	Summary bool
}

// Sent logs agent.read for text about to be sent off the machine (§6.7).
// The sender logs before the call, so a call that fails is logged too, and
// must not send when this returns an error.
func (a *Auditor) Sent(e Egress) error {
	from := ""
	if e.Pane != (Pane{}) {
		from = " from " + hostLabel(e.Pane)
	}
	entry := applog.Entry{
		Kind:       EventRead,
		Message:    fmt.Sprintf("agent sent %s%s to %s %s · %s", e.What, from, e.To, e.Why, redactionNote(e.Report)),
		HostID:     e.Pane.HostID,
		SessionID:  e.Pane.SessionID,
		Lines:      e.N,
		Count:      e.N,
		Redactions: e.Report.Strings(),
		Source:     e.Source,
	}
	if e.Summary {
		return a.note(entry)
	}
	return a.gate(entry)
}

// InsertFrom logs agent.insert: command was put on p's prompt line (⇥ /
// ⇧⇥, not run) from a suggestion of that source. It is called once the
// bytes are written, and refuses a client that does not drive the pane.
func (a *Auditor) InsertFrom(p Pane, command, source string) error {
	return a.logWrite(EventInsert, "inserted", p, command, source, true)
}

// How a Type into pane was approved, as agent.type records it (Source).
const (
	// TypeApproved: in the approve dialog, which showed the exact bytes.
	TypeApproved = "approved"
	// TypeDirect: one click, without the dialog, and without Enter: the
	// command waits on the prompt line (docs/ui-redesign.md §6 rule 13).
	TypeDirect = "direct"
)

// TypeFrom logs agent.type with how the Type was approved. TypeApproved is
// logged before the write, and the caller writes only if this returns nil;
// TypeDirect is logged once the bytes are written. It writes nothing
// itself and refuses a client that does not drive the pane.
func (a *Auditor) TypeFrom(p Pane, command, how string) error {
	if how == TypeDirect {
		return a.logWrite(EventType, "typed without Enter", p, command, how, true)
	}
	return a.logWrite(EventType, "typed", p, command, how, false)
}

// logWrite logs a write into a pane (ids and counts, never the bytes),
// refusing a client that does not drive it. written says the bytes are
// already in the pane.
func (a *Auditor) logWrite(kind, verb string, p Pane, command, source string, written bool) error {
	if err := CanInsert(p.Role); err != nil {
		return err
	}
	lines := 0
	if command != "" {
		lines = strings.Count(strings.TrimRight(command, "\n"), "\n") + 1
	}
	e := applog.Entry{
		Kind:      kind,
		Message:   fmt.Sprintf("agent command %s into %s · %d bytes, %d lines", verb, hostLabel(p), len(command), lines),
		HostID:    p.HostID,
		SessionID: p.SessionID,
		Lines:     lines,
		Bytes:     len(command),
		Source:    source,
	}
	if written {
		return a.note(e)
	}
	return a.gate(e)
}

func Merge(a, b redact.Report) redact.Report {
	out := redact.Report{Counts: map[redact.Kind]int{}}
	for k, v := range a.Counts {
		out.Counts[k] += v
	}
	for k, v := range b.Counts {
		out.Counts[k] += v
	}
	return out
}

// Diff returns what one Redactor redacted between two of its reports.
func Diff(before, after redact.Report) redact.Report {
	out := redact.Report{Counts: map[redact.Kind]int{}}
	for k, v := range after.Counts {
		if d := v - before.Counts[k]; d > 0 {
			out.Counts[k] = d
		}
	}
	return out
}

// forSource is " for <source>" in a log message when a reader other than
// the agent panel read the pane, "" otherwise.
func forSource(source string) string {
	if source == "" {
		return ""
	}
	return " for " + source
}

func hostLabel(p Pane) string {
	if p.HostName != "" {
		return p.HostName
	}
	if p.HostID != "" {
		return "host " + p.HostID
	}
	return "a pane"
}

func riskLabel(r string) string {
	if r == "" {
		return "risk not rated"
	}
	return r
}

func redactionNote(rep redact.Report) string {
	switch n := rep.Total(); n {
	case 0:
		return "no redactions"
	case 1:
		return "1 redaction"
	default:
		return fmt.Sprintf("%d redactions", n)
	}
}
