package native

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"keel/internal/native/keelclient"
)

// Every case of cmd/keel/web/js/test/notes.test.mjs (6 tests), in the same order.

func TestNoteSourceLineSaysWhoWroteItAndFromWhichPane(t *testing.T) {
	at := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		n    keelclient.Note
		want string
	}{
		{keelclient.Note{Source: "user", CreatedAt: at}, `^You · Oct 1[12]$`},
		{keelclient.Note{Source: "agent", CreatedAt: at}, `^Proposed by the agent · Oct 1[12] · you approved$`},
		{keelclient.Note{Source: "agent", FromPane: true, SeenIn: "prod-db-02", Edited: true, CreatedAt: at}, `^Proposed by the agent from pane prod-db-02 · Oct 1[12] · you edited$`},
	} {
		if got := noteSourceLabel(c.n); !regexp.MustCompile(c.want).MatchString(got) {
			t.Errorf("%+v: %q, want %s", c.n, got, c.want)
		}
	}
	if got := noteSourceLabel(keelclient.Note{}); got != "You" {
		t.Errorf("an empty note: %q", got)
	}
}

func TestNoteCountLineAndOneLineCleaningLikeKeelServe(t *testing.T) {
	for _, c := range []struct {
		notes []keelclient.Note
		want  string
	}{
		{nil, "0 notes · 0 chars"},
		{[]keelclient.Note{{Text: "abc"}, {Text: "de"}}, "2 notes · 5 chars"},
		{[]keelclient.Note{{Text: "a"}}, "1 note · 1 char"},
	} {
		if got := notesCountLabel(c.notes); got != c.want {
			t.Errorf("%q, want %q", got, c.want)
		}
	}
	//lint:ignore ST1018 the zero-width space in the note is the test input
	got, ok, long := cleanNote("  one\ntwo three​x  ")
	if got != "one two three x" || !ok || long {
		t.Errorf("cleanNote: %q %v %v", got, ok, long)
	}
	if _, ok, _ := cleanNote("   "); ok {
		t.Error("blank text can be saved")
	}
	if _, ok, long := cleanNote(strings.Repeat("é", 501)); ok || !long {
		t.Errorf("501 two-byte characters: ok %v tooLong %v", ok, long)
	}
	if _, ok, long := cleanNote(strings.Repeat("é", 500)); !ok || long {
		t.Errorf("500 two-byte characters is exactly 1000 bytes: ok %v tooLong %v", ok, long)
	}
}

func TestHostNotesAndYourNotesArePickedApart(t *testing.T) {
	all := []keelclient.Note{{ID: "1", HostID: "h1"}, {ID: "2"}, {ID: "3", HostID: "h2"}, {ID: "4", HostID: "h1"}, {ID: "5", GroupID: "g1"}}
	ids := func(ns []keelclient.Note) (out []string) {
		for _, n := range ns {
			out = append(out, n.ID)
		}
		return out
	}
	if got := strings.Join(ids(hostNotesOf(all, "h1")), ","); got != "1,4" {
		t.Errorf("host notes: %s", got)
	}
	if got := ids(hostNotesOf(all, "")); len(got) != 0 {
		t.Errorf("no host: %v", got)
	}
	if got := strings.Join(ids(userNotesOf(all)), ","); got != "2" {
		t.Errorf("your notes (a group note is not one): %s", got)
	}
}

func TestAProposedNoteSaysWhoseNotesItJoinsWhereItCameFromAndWhenItIsSent(t *testing.T) {
	host := keelclient.NoteProposal{For: "host", Host: "prod-db-01", From: "conversation"}
	eq := func(what, got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("%s: %q, want %q", what, got, want)
		}
	}
	eq("target", noteProposalTarget(host), "Host notes · prod-db-01")
	eq("source", noteProposalSource(host), "From this conversation · agent summary")
	eq("scope", noteProposalScope(host), "Sent when prod-db-01 is in scope")
	mine := keelclient.NoteProposal{For: "user", From: "user"}
	eq("your target", noteProposalTarget(mine), "Your notes · every host")
	eq("your source", noteProposalSource(mine), "From your messages in this conversation")
	if !strings.HasPrefix(noteProposalScope(mine), "Sent with questions that read a pane") {
		t.Errorf("your scope: %q", noteProposalScope(mine))
	}
	eq("pane source", noteProposalSource(keelclient.NoteProposal{For: "host", FromPane: true, SeenIn: "prod-db-02"}), "Seen in pane prod-db-02 · terminal output")
	host.Status, mine.Status = "saved", "saved"
	eq("done host", noteProposalDone(host), "Saved to host notes · prod-db-01")
	eq("done yours", noteProposalDone(mine), "Saved to your notes")
	eq("dismissed", noteProposalDone(keelclient.NoteProposal{Status: "dismissed"}), "Dismissed. Nothing was saved.")
	eq("open", noteProposalDone(keelclient.NoteProposal{Status: "open"}), "")
	if !strings.Contains(noteFromPaneWarning, "terminal output, not from you") {
		t.Error("the from-pane warning")
	}
}

func TestEveryProposalFromAnAnswerThatReadAPaneIsWarnedANoteAboutYouMostStrongly(t *testing.T) {
	plain := keelclient.NoteProposal{For: "host", Host: "web-1"}
	if noteProposalWarn(plain) || noteProposalWarning(plain) != "" {
		t.Error("a proposal from no pane read is warned")
	}
	read := keelclient.NoteProposal{For: "host", Host: "web-1", PanesRead: []string{"web-1"}}
	if !noteProposalWarn(read) || !strings.HasPrefix(noteProposalWarning(read), "The agent read terminal output for this answer") {
		t.Errorf("after reading panes: %q", noteProposalWarning(read))
	}
	fp := read
	fp.FromPane = true
	if noteProposalWarning(fp) != noteFromPaneWarning {
		t.Errorf("from pane: %q", noteProposalWarning(fp))
	}
	if w := noteProposalWarning(keelclient.NoteProposal{For: "user", PanesRead: []string{"web-1"}}); !strings.Contains(w, "every host the agent may read. Save it only if you would have written it yourself") {
		t.Errorf("about you: %q", w)
	}
	eq := func(what, got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("%s: %q, want %q", what, got, want)
		}
	}
	eq("origin: the same host", noteProposalOrigin(read), "")
	two := read
	two.PanesRead = []string{"db-2", "web-1"}
	eq("origin: two panes", noteProposalOrigin(two), "This answer read db-2 and web-1; the note would be saved to the notes of web-1.")
	seen := fp
	seen.SeenIn = "db-2"
	eq("origin: seen in", noteProposalOrigin(seen), "Seen in pane db-2; it would be saved to the notes of web-1.")
	eq("origin: yours", noteProposalOrigin(keelclient.NoteProposal{For: "user", PanesRead: []string{"a", "b", "c"}}), "This answer read a, b and c; this note would be sent on every host.")
	eq("origin: yours, nothing read", noteProposalOrigin(keelclient.NoteProposal{For: "user"}), "")
}

func TestTheTraceSaysWhichNotesWentWithAQuestionAndReadSoFarLeavesThemOut(t *testing.T) {
	for _, c := range []struct {
		s    keelclient.TraceStep
		want string
	}{
		{keelclient.TraceStep{Status: "notes", NoteKind: "host", Host: "prod-db-01", Notes: 3}, "Used host notes · prod-db-01 (3)"},
		{keelclient.TraceStep{Status: "notes", NoteKind: "user", Notes: 2}, "Used your notes (2)"},
	} {
		if got := traceText(c.s); got != c.want {
			t.Errorf("%q, want %q", got, c.want)
		}
	}
	sum := readSummary([]convTurn{{trace: []keelclient.TraceStep{{Status: "notes", NoteKind: "host", Host: "web-1", Notes: 2}, {Status: "read", Host: "web-1", Lines: 10}}}})
	if len(sum) != 1 || sum[0].host != "web-1" || sum[0].lines != 10 {
		t.Errorf("read so far: %+v", sum)
	}
	if redactedToast(0) != "" || redactedToast(1) != "1 secret-looking value was replaced before saving." || redactedToast(2) != "2 secret-looking values were replaced before saving." {
		t.Error("redactedToast")
	}
}
