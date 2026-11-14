package native

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"

	"keel/internal/limits"
	"keel/internal/native/keelclient"
)

// The pure rules of notes for the agent (docs/ui-redesign.md §15): the words a note and a proposed note carry. They are
// pinned by noteslogic_test.go. Nothing here talks to
// keel serve, draws or types.

const maxNoteBytes = limits.MaxAgentNoteBytes

var noteTooLong = fmt.Sprintf("A note is at most %d bytes.", maxNoteBytes)

// dayLabel is "Oct 12" for a timestamp, "" for none.
func dayLabel(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return clockMD(t)
}

func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

// noteSourceLabel says where a note came from: "You · Oct 12", or "Proposed by the agent from pane prod-db-02 · Oct 14 ·
// you approved" ("you edited" when the user changed it).
func noteSourceLabel(n keelclient.Note) string {
	day := dayLabel(n.CreatedAt)
	if n.Source != "agent" {
		return joinNonEmpty(" · ", "You", day)
	}
	from := "Proposed by the agent"
	if n.FromPane {
		from += " from pane " + orElse(n.SeenIn, "output")
	}
	state := "you approved"
	if n.Edited {
		state = "you edited"
	}
	return joinNonEmpty(" · ", from, day, state)
}

// notesCountLabel is the notes list's head: "4 notes · 412 chars". A character is a UTF-16 unit, as keel serve counts it.
func notesCountLabel(notes []keelclient.Note) string {
	chars := 0
	for _, n := range notes {
		chars += len(utf16.Encode([]rune(n.Text)))
	}
	return fmt.Sprintf("%s · %s", countWord(len(notes), "note", "notes"), countWord(chars, "char", "chars"))
}

// isInvisibleRune is a control character, a line or paragraph separator or a zero-width character: keel serve turns
// them into spaces, and so does cleanNote.
func isInvisibleRune(c rune) bool {
	return c <= 0x1f || (c >= 0x7f && c <= 0x9f) || c == 0x2028 || c == 0x2029 || (c >= 0x200b && c <= 0x200f) || c == 0x2060 || c == 0xfeff
}

// cleanNote makes typed text one line, as keel serve keeps it, and says whether it can be saved. This only decides
// whether a request is worth sending; keel serve cleans and redacts what it receives.
func cleanNote(text string) (cleaned string, ok, tooLong bool) {
	var b strings.Builder
	for _, r := range text {
		if isInvisibleRune(r) {
			r = ' '
		}
		b.WriteRune(r)
	}
	cleaned = strings.Join(strings.FieldsFunc(b.String(), unicode.IsSpace), " ")
	return cleaned, cleaned != "" && len(cleaned) <= maxNoteBytes, len(cleaned) > maxNoteBytes
}

// hostNotesOf picks a host's notes from GET /api/agent/notes, in the order they came (oldest first).
func hostNotesOf(notes []keelclient.Note, hostID string) (out []keelclient.Note) {
	for _, n := range notes {
		if hostID != "" && n.HostID == hostID {
			out = append(out, n)
		}
	}
	return out
}

// groupNotesOf picks a folder's notes (§17).
func groupNotesOf(notes []keelclient.Note, groupID string) (out []keelclient.Note) {
	for _, n := range notes {
		if groupID != "" && n.GroupID == groupID {
			out = append(out, n)
		}
	}
	return out
}

// userNotesOf picks the user's own notes: no host and no group.
func userNotesOf(notes []keelclient.Note) (out []keelclient.Note) {
	for _, n := range notes {
		if n.HostID == "" && n.GroupID == "" {
			out = append(out, n)
		}
	}
	return out
}

// noteProposalTarget is the badge on a proposed note: whose notes it joins.
func noteProposalTarget(p keelclient.NoteProposal) string {
	if p.For == "user" {
		return "Your notes · every host"
	}
	return "Host notes · " + orElse(p.Host, "this host")
}

// noteProposalSource is the line under a proposed note's text.
func noteProposalSource(p keelclient.NoteProposal) string {
	switch {
	case p.FromPane:
		return "Seen in pane " + orElse(p.SeenIn, orElse(p.Host, "output")) + " · terminal output"
	case p.From == "user":
		return "From your messages in this conversation"
	}
	return "From this conversation · agent summary"
}

// noteProposalScope says when a saved note would be sent.
func noteProposalScope(p keelclient.NoteProposal) string {
	if p.For == "user" {
		return "Sent with questions that read a pane the agent may read"
	}
	return "Sent when " + orElse(p.Host, "this host") + " is in scope"
}

// noteProposalDone is the line a closed proposal leaves.
func noteProposalDone(p keelclient.NoteProposal) string {
	switch p.Status {
	case "saved":
		if p.For == "user" {
			return "Saved to your notes"
		}
		return "Saved to host notes · " + orElse(p.Host, "")
	case "dismissed":
		return "Dismissed. Nothing was saved."
	}
	return ""
}

// noteFromPaneWarning is shown on a note whose text came from terminal output.
const noteFromPaneWarning = "This text came from terminal output, not from you. Check it before saving: a saved note is sent to the agent with every question that reads this host, and text in a pane can be written to mislead the agent."

// noteProposalWarn reports whether a proposed note gets the warning treatment: its text was found in pane output, or any
// pane was read for the answer that proposed it. Keel's own "from pane output" check is a hint a crafted pane can evade,
// so a read pane is reason enough.
func noteProposalWarn(p keelclient.NoteProposal) bool { return p.FromPane || len(p.PanesRead) > 0 }

// noteProposalWarning is the warning a proposed note shows, "" for none. A note about the user is sent on every host, so
// its warning is the strongest.
func noteProposalWarning(p keelclient.NoteProposal) string {
	switch {
	case !noteProposalWarn(p):
		return ""
	case p.For == "user":
		return "The agent read terminal output for this answer, and this note may come from it, not from you. A note about you is sent with questions on every host the agent may read. Save it only if you would have written it yourself."
	case p.FromPane:
		return noteFromPaneWarning
	}
	return "The agent read terminal output for this answer, and this text may come from it, not from you. Check it before saving: a saved note is sent to the agent with every question that reads this host."
}

// noteProposalOrigin says where the text was read and where the note would go, when they differ. "" when there is
// nothing to tell apart.
func noteProposalOrigin(p keelclient.NoteProposal) string {
	var read []string
	for _, h := range p.PanesRead {
		if h != "" {
			read = append(read, h)
		}
	}
	list := ""
	switch {
	case len(read) > 1:
		list = strings.Join(read[:len(read)-1], ", ") + " and " + read[len(read)-1]
	case len(read) == 1:
		list = read[0]
	}
	if p.For == "user" {
		if list == "" {
			return ""
		}
		return "This answer read " + list + "; this note would be sent on every host."
	}
	host := orElse(p.Host, "this host")
	if p.SeenIn != "" && p.SeenIn != p.Host {
		return "Seen in pane " + p.SeenIn + "; it would be saved to the notes of " + host + "."
	}
	for _, h := range read {
		if h != p.Host {
			return "This answer read " + list + "; the note would be saved to the notes of " + host + "."
		}
	}
	return ""
}

// redactedToast says what was replaced before a note was kept.
func redactedToast(n int) string {
	switch {
	case n <= 0:
		return ""
	case n == 1:
		return "1 secret-looking value was replaced before saving."
	}
	return fmt.Sprintf("%d secret-looking values were replaced before saving.", n)
}

// savedRedactedToast is the toast after a proposal's Save or a runbook's Save.
func savedRedactedToast(n int) string {
	switch {
	case n <= 0:
		return ""
	case n == 1:
		return "Saved. 1 secret-looking value was replaced first."
	}
	return fmt.Sprintf("Saved. %d secret-looking values were replaced first.", n)
}
