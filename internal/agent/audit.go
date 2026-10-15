package agent

import (
	"fmt"
	"strings"
	"time"

	"keel/internal/agent/redact"
	"keel/internal/applog"
)

// Logger is where audit entries go. *applog.Book (keel.log) satisfies it.
type Logger interface {
	Append(applog.Entry)
}

// ScrollbackSource reads a pane's history. *runtime.Runtime satisfies it;
// the agent depends on this one read method only.
type ScrollbackSource interface {
	ReadScrollback(sessionID string) ([]byte, error)
}

// maxExcerptBytes caps one pane's excerpt after redaction (1000 lines of
// a wide terminal).
const maxExcerptBytes = 256 * 1024

// Auditor runs every agent read and insert, and writes each one to
// keel.log (§6.7). There is no way to read a pane or authorize an insert
// through this package without an entry being written.
type Auditor struct {
	log Logger
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
}

// ReadPane reads the last lines of a pane in scope, redacts them with r
// (shared across the request), and logs agent.read with the pane, host,
// line count and redaction counts. A pane out of scope is refused before
// anything is read.
func (a *Auditor) ReadPane(src ScrollbackSource, scope Scope, p Pane, lines int, r *redact.Redactor) (Excerpt, error) {
	return a.readPane(src, scope, p, lines, r, false)
}

// ReadPaneScreen is ReadPane for reading the commands typed in a pane
// (history-first suggestions): each line is what the terminal shows once
// the line is done, so a shell redrawing the prompt line with a carriage
// return (a cleared line, an inserted suggestion, line editing) leaves only
// its final text, not every draft. Logged the same way.
func (a *Auditor) ReadPaneScreen(src ScrollbackSource, scope Scope, p Pane, lines int, r *redact.Redactor) (Excerpt, error) {
	return a.readPane(src, scope, p, lines, r, true)
}

// FinalLines keeps, of every line of raw PTY output, the text after its
// last lone carriage return: what the terminal shows once the line ends.
func FinalLines(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	for i, ln := range lines {
		if k := strings.LastIndexByte(strings.TrimRight(ln, "\r"), '\r'); k >= 0 {
			lines[i] = ln[k+1:]
		}
	}
	return strings.Join(lines, "\n")
}

func (a *Auditor) readPane(src ScrollbackSource, scope Scope, p Pane, lines int, r *redact.Redactor, screen bool) (Excerpt, error) {
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
	in := string(raw)
	if screen {
		in = FinalLines(in)
	}
	text, n := LastLines(redact.Normalize(in), ClampLines(lines))
	before := r.Report()
	safe := r.Text(text).Tail(maxExcerptBytes)
	rep := diff(before, r.Report())
	a.log.Append(applog.Entry{
		Kind:       EventRead,
		Message:    fmt.Sprintf("agent read %s · last %d lines · %s", hostLabel(p), n, redactionNote(rep)),
		HostID:     p.HostID,
		SessionID:  p.SessionID,
		Lines:      n,
		Redactions: rep.Strings(),
	})
	return Excerpt{Pane: p, Lines: n, Text: safe, Report: rep}, nil
}

// Suggest logs agent.suggest: a suggestion was produced for a pane. rep is
// what was redacted from the text sent for it; risk is the label shown
// (Read-only, Changes state, Destructive).
func (a *Auditor) Suggest(p Pane, rep redact.Report, risk string) {
	a.SuggestFrom(p, rep, risk, "")
}

// SuggestFrom is Suggest with the suggestion's source: "generated" (the
// model provider), "history" (a command from the pane's scrollback) or
// "snippet" (a snippet marked agent-usable). In-pane suggestions (§5.5)
// always name one.
func (a *Auditor) SuggestFrom(p Pane, rep redact.Report, risk, source string) {
	a.SuggestTimed(p, rep, risk, source, Timing{})
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

// SuggestTimed is SuggestFrom with the time the model and Jev took, so a
// slow suggestion shows where the time went. It logs no content.
func (a *Auditor) SuggestTimed(p Pane, rep redact.Report, risk, source string, t Timing) {
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
	a.log.Append(e)
}

// PromptLines logs agent.read for prompt lines of p sent to a detector
// (§7): n lines, rep their redactions together. Detection runs on a typing
// pause, so the caller summarises: one entry stands for every line sent in
// a window, and Count says how many.
func (a *Auditor) PromptLines(p Pane, n int, rep redact.Report, detector string) {
	if n <= 0 {
		return
	}
	what := "1 prompt line"
	if n > 1 {
		what = fmt.Sprintf("%d prompt lines", n)
	}
	a.log.Append(applog.Entry{
		Kind:       EventRead,
		Message:    fmt.Sprintf("agent sent %s from %s to %s to notice plain language · %s", what, hostLabel(p), detectorName(detector), redactionNote(rep)),
		HostID:     p.HostID,
		SessionID:  p.SessionID,
		Lines:      n,
		Count:      n,
		Redactions: rep.Strings(),
		Source:     detector,
	})
}

// Sent logs agent.read for text sent off the machine for an in-pane
// suggestion (§6.7): what was sent ("the words on the prompt line", "2
// candidate commands"), n lines of it, its redactions, to whom and why.
// The caller logs before the call, so a call that fails is logged too.
func (a *Auditor) Sent(p Pane, what string, n int, rep redact.Report, to, why, source string) {
	a.log.Append(applog.Entry{
		Kind:       EventRead,
		Message:    fmt.Sprintf("agent sent %s from %s to %s %s · %s", what, hostLabel(p), to, why, redactionNote(rep)),
		HostID:     p.HostID,
		SessionID:  p.SessionID,
		Lines:      n,
		Count:      n,
		Redactions: rep.Strings(),
		Source:     source,
	})
}

// Insert authorizes putting command on p's prompt line (⇥ / ⇧⇥, not run)
// and logs agent.insert. It writes nothing: the caller does the write
// through the user's own approved input path, and only if this returns nil.
func (a *Auditor) Insert(p Pane, command string) error {
	return a.InsertFrom(p, command, "")
}

// InsertFrom is Insert with the suggestion's source, which the entry
// records.
func (a *Auditor) InsertFrom(p Pane, command, source string) error {
	return a.authorize(EventInsert, "inserted", p, command, source)
}

// Type authorizes typing command into p (Type into pane ↵) and logs
// agent.type. Like Insert, it writes nothing itself.
func (a *Auditor) Type(p Pane, command string) error {
	return a.authorize(EventType, "typed", p, command, "")
}

func (a *Auditor) authorize(kind, verb string, p Pane, command, source string) error {
	if err := CanInsert(p.Role); err != nil {
		return err
	}
	lines := 0
	if command != "" {
		lines = strings.Count(strings.TrimRight(command, "\n"), "\n") + 1
	}
	a.log.Append(applog.Entry{
		Kind:      kind,
		Message:   fmt.Sprintf("agent command %s into %s · %d bytes, %d lines", verb, hostLabel(p), len(command), lines),
		HostID:    p.HostID,
		SessionID: p.SessionID,
		Lines:     lines,
		Bytes:     len(command),
		Source:    source,
	})
	return nil
}

// Diff returns what one Redactor redacted between two of its reports.
func Diff(before, after redact.Report) redact.Report { return diff(before, after) }

// Merge adds the counts of two reports.
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

func diff(before, after redact.Report) redact.Report {
	out := redact.Report{Counts: map[redact.Kind]int{}}
	for k, v := range after.Counts {
		if d := v - before.Counts[k]; d > 0 {
			out.Counts[k] = d
		}
	}
	return out
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

func detectorName(id string) string {
	if id == "jev" {
		return "TypeSafe Jev"
	}
	return id
}
