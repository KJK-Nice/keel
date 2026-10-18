package agent

import (
	"fmt"
	"strings"

	"keel/internal/agent/redact"
	"keel/internal/applog"
)

// Notes for the agent (docs/ui-redesign.md §15). A note is text the user
// wrote, or a proposal of the agent's that the user saved. It is sent with
// a question like pane text is: only for a host the agent may read now
// (scope), only when a pane on that host is read for that question,
// redacted with the request's Redactor, and logged as agent.note. The
// user's own notes go with any question that reads at least one pane.
//
// Notes are framed apart from pane text in the provider message, as
// context the user curated (NoteFrame), never as pane output and never as
// an instruction that overrides the system prompt.

// NoteExcerpt is the redacted text of the notes sent for one host (or the
// user's own notes, HostID empty), one note per line.
type NoteExcerpt struct {
	HostID   string
	HostName string
	Count    int
	Text     redact.Safe
	Report   redact.Report
}

// ReadHostNotes redacts a host's notes with r for one request and logs
// agent.note. A host the agent may not read is refused before anything is
// redacted or logged.
func (a *Auditor) ReadHostNotes(scope Scope, h Host, notes []string, r *redact.Redactor) (NoteExcerpt, error) {
	if !scope.AllowsHost(h.ID) {
		return NoteExcerpt{}, ErrNotReadable
	}
	ex, err := a.readNotes(notes, r)
	if err != nil {
		return NoteExcerpt{}, err
	}
	ex.HostID, ex.HostName = h.ID, h.Name
	name := h.Name
	if name == "" {
		name = "host " + h.ID
	}
	a.log.Append(applog.Entry{
		Kind:       EventNote,
		Message:    fmt.Sprintf("agent sent %s for %s with a question · %s", noteCount(ex.Count, "host note", "host notes"), name, redactionNote(ex.Report)),
		HostID:     h.ID,
		Count:      ex.Count,
		Redactions: ex.Report.Strings(),
		Source:     "host",
	})
	return ex, nil
}

// ReadUserNotes redacts the user's own notes with r and logs agent.note.
func (a *Auditor) ReadUserNotes(notes []string, r *redact.Redactor) (NoteExcerpt, error) {
	ex, err := a.readNotes(notes, r)
	if err != nil {
		return NoteExcerpt{}, err
	}
	a.log.Append(applog.Entry{
		Kind:       EventNote,
		Message:    fmt.Sprintf("agent sent %s with a question · %s", noteCount(ex.Count, "of your notes", "of your notes"), redactionNote(ex.Report)),
		Count:      ex.Count,
		Redactions: ex.Report.Strings(),
		Source:     "user",
	})
	return ex, nil
}

func (a *Auditor) readNotes(notes []string, r *redact.Redactor) (NoteExcerpt, error) {
	if r == nil {
		return NoteExcerpt{}, fmt.Errorf("agent: no redactor")
	}
	var b strings.Builder
	n := 0
	for _, t := range notes {
		// Kept notes are one line already; only line breaks become spaces
		// here. Whitespace is not collapsed before redaction, so a vault
		// value is matched as it is in the note.
		t = strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(t))
		if t == "" {
			continue
		}
		b.WriteString("- " + t + "\n")
		n++
	}
	before := r.Report()
	safe := r.Text(strings.TrimRight(b.String(), "\n"))
	return NoteExcerpt{Count: n, Text: safe, Report: diff(before, r.Report())}, nil
}

// NoteChanged logs agent.note for a note the user added, saved from a
// proposal, edited or deleted: what happened and to whose notes, never the
// text. rep is what was redacted from the text before it was kept.
func (a *Auditor) NoteChanged(what, hostID, hostName string, rep redact.Report) {
	whose := "your notes"
	if hostID != "" {
		whose = "host notes for " + hostName
		if hostName == "" {
			whose = "host notes for host " + hostID
		}
	}
	a.log.Append(applog.Entry{
		Kind:       EventNote,
		Message:    fmt.Sprintf("%s · %s · %s", what, whose, redactionNote(rep)),
		HostID:     hostID,
		Redactions: rep.Strings(),
	})
}

func noteCount(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
