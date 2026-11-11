package native

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// Notes for the agent on the fake backend: each list and action sends the app's request; a proposal is kept only by its
// card's Save; Dismiss keeps nothing; Delete all notes asks first; and nothing is written to disk by the client. Every text
// is made up.

// lacks names the texts of want that are not on screen: a failure then says which, not the whole window.
func (r *rig) lacks(want ...string) (out []string) {
	for _, s := range want {
		if !r.tt.HasText(s) {
			out = append(out, s)
		}
	}
	return out
}

// onScreen says whether any text on screen holds s.
func (r *rig) onScreen(s string) bool {
	for _, x := range r.tt.Texts() {
		if strings.Contains(x, s) {
			return true
		}
	}
	return false
}

// addNoteTo types text into the open "Add a note" field and presses ↵.
func (r *rig) addNoteTo(t *testing.T, text string) {
	t.Helper()
	r.click(t, "Add a note")
	r.tt.Type(text)
	r.tt.Frame()
	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
}

// localHost is the Local Shell: its Edit host dialog is the short one, so the notes block is on screen.
var localHost = testHosts[3]

func TestHostNotesInEditHostAddEditDeleteAreSavedAtOnce(t *testing.T) {
	r := hostRig(t)
	r.w.editHost(localHost)
	r.tt.Frame()
	if miss := r.lacks("Notes for the agent", "No notes. Add what the agent should know about this host.", "0 notes · 0 chars",
		"Sent to the agent only when this host is in scope and Agent can read is on. Redacted like pane text. Stored encrypted in the vault."); len(miss) > 0 {
		t.Fatalf("missing %q", miss)
	}
	r.addNoteTo(t, "deploys run from /srv/app")
	if got := r.fb.mem.noteReqs; !slices.Equal(got, []string{"add h-local:deploys run from /srv/app"}) {
		t.Fatalf("requests %v", got)
	}
	if miss := r.lacks("deploys run from /srv/app", "1 note · 25 chars", "You"); len(miss) > 0 {
		t.Errorf("the note is not listed: missing %q", miss)
	}
	if r.w.hostDlg == nil || len(r.fb.saved) != 0 {
		t.Fatal("↵ in the notes field submitted the host form")
	}
	// Edit: ↵ saves the text.
	r.click(t, "Edit note")
	r.tt.Key(ui.Cmd, ui.KeyA)
	r.tt.Type("deploys run from /srv/app with make deploy")
	r.tt.Frame()
	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
	if got := r.fb.mem.noteReqs; len(got) != 2 || got[1] != "edit n1:deploys run from /srv/app with make deploy" {
		t.Fatalf("requests %v", got)
	}
	if !r.tt.HasText("deploys run from /srv/app with make deploy") {
		t.Error("the edit is not listed")
	}
	// Delete asks nothing, as the app does.
	r.click(t, "Delete note")
	if got := r.fb.mem.noteReqs; got[len(got)-1] != "delete n1" || r.w.confirm != nil {
		t.Fatalf("requests %v, confirm %v", got, r.w.confirm)
	}
	if !r.tt.HasText("No notes. Add what the agent should know about this host.") || r.w.hostDlg == nil {
		t.Error("the list is not empty, or the dialog closed")
	}
}

func TestHostNotesAreNotSentWhenAgentCanReadIsOffAndWaitForASavedHost(t *testing.T) {
	r := hostRig(t)
	r.w.editHost(localHost)
	r.tt.Frame()
	const off = "Agent can read is off, so these notes are not sent. They stay in the vault."
	if r.tt.HasText(off) {
		t.Fatal("the warning shows with reading on")
	}
	r.w.hostDlg.f.agentReadable = false
	r.tt.Frame()
	if !r.tt.HasText(off) {
		t.Error("no warning with reading off")
	}
	r.w.closeHostDialog()
	r.w.newHost()
	r.tt.Frame()
	if !r.tt.HasText("Save the host first, then add notes for the agent.") || r.has("Add a note") {
		t.Error("a host that is not saved shows a field to add a note")
	}
	if len(r.fb.mem.noteReqs) != 0 {
		t.Error("a note was sent for a host that is not saved")
	}
}

func TestANoteWithASecretShowsWhatKeelServeKeptAndHowManyValuesWereReplaced(t *testing.T) {
	r := hostRig(t)
	r.w.editHost(localHost)
	r.tt.Frame()
	r.addNoteTo(t, "root password is "+fakeSecret)
	if !r.tt.HasText("root password is [secret:1]") {
		t.Error("the redacted form is not listed")
	}
	if !r.tt.HasText("Note added. 1 secret-looking value was replaced before saving.") {
		t.Error("the count is not told")
	}
	if r.onScreen(fakeSecret) {
		t.Fatal("the value shows on screen")
	}
	for _, n := range r.w.hostDlg.nb.notes {
		if strings.Contains(n.Text, fakeSecret) {
			t.Fatal("the list holds the value")
		}
	}
}

func TestANoteThatCannotBeSavedSendsNothingAndSaysWhy(t *testing.T) {
	r := hostRig(t)
	r.w.editHost(localHost)
	r.tt.Frame()
	r.addNoteTo(t, "   ")
	if len(r.fb.mem.noteReqs) != 0 {
		t.Fatalf("a blank note was sent: %v", r.fb.mem.noteReqs)
	}
	r.click(t, "Add a note")
	r.tt.Type(strings.Repeat("é", 501))
	r.tt.Frame()
	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
	if len(r.fb.mem.noteReqs) != 0 || !r.tt.HasText("A note is at most 1000 bytes.") {
		t.Errorf("requests %v", r.fb.mem.noteReqs)
	}
}

func TestSettingsAgentYourNotesAndDeleteAllAsksFirst(t *testing.T) {
	r := agentSettingsRig(t)
	r.fb.mem.notes = []keelclient.Note{{ID: "nx", HostID: "h-api", Text: "a host note", Source: "user"}}
	r.w.loadAgentSettings()
	r.tt.Frame()
	if !r.tt.HasText("No notes. Add how you like to work, e.g. Prefer journalctl over tail -f.") {
		t.Errorf("your notes list is not empty (a host note is not one of them)")
	}
	r.addNoteTo(t, "prefer journalctl")
	r.addNoteTo(t, "short answers")
	if got := r.fb.mem.noteReqs; !slices.Equal(got, []string{"add :prefer journalctl", "add :short answers"}) {
		t.Fatalf("requests %v", got)
	}
	if !r.tt.HasText("2 notes · 30 chars") {
		t.Error("count")
	}
	// Delete all notes asks first, with the app's words; Cancel keeps them.
	r.click(t, "Delete all notes…")
	r.settleFrames()
	cf := r.w.confirm
	if cf == nil || cf.title != "Delete all notes?" || cf.action != "Delete all notes" ||
		cf.message != "Every host note and every note about you is deleted from the vault. Conversations and the log stay." {
		t.Fatalf("no confirm, or not the app's: %+v", cf)
	}
	if r.fb.mem.gone != 0 {
		t.Fatal("it deleted before it asked")
	}
	r.click(t, "Cancel")
	r.settleFrames()
	if r.fb.mem.gone != 0 || r.w.confirm != nil || len(r.fb.mem.notes) != 3 {
		t.Fatalf("Cancel: deleted %d, notes %d", r.fb.mem.gone, len(r.fb.mem.notes))
	}
	r.click(t, "Delete all notes…")
	r.settleFrames()
	r.w.confirm.run() // the dialog's action (its button shares its words with the row's)
	r.settleFrames()
	if r.fb.mem.gone != 1 || len(r.fb.mem.notes) != 0 || !r.tt.HasText("Notes deleted") {
		t.Fatalf("deleted %d, notes %v", r.fb.mem.gone, r.fb.mem.notes)
	}
}

// ---- the notes the agent proposes ----

func noteProposal() keelclient.NoteProposal {
	return keelclient.NoteProposal{ID: "np1", For: "host", HostID: "h-api", Host: "api-1", Text: "backups run at 02:00", From: "conversation", Status: "open"}
}

func (r *rig) answersNotes(ns ...keelclient.NoteProposal) {
	r.fb.agent.askReply = func(req keelclient.AskRequest) (keelclient.AskResponse, error) {
		return keelclient.AskResponse{ConversationID: req.ConversationID, Answer: "ok", Notes: ns,
			Trace: []keelclient.TraceStep{{Host: "api-1", Status: "notes", NoteKind: "host", Notes: 2}, {SessionID: "s1", Host: "api-1", Status: "read", Lines: 9}}}, nil
	}
}

func TestANoteProposalIsKeptOnlyByTheCardsSaveAndDismissKeepsNothing(t *testing.T) {
	r := agentRig(t)
	p := noteProposal()
	p.PanesRead = []string{"api-1"}
	r.answersNotes(p)
	r.ask(t, "please remember this")
	if miss := r.lacks("Remember this?", "backups run at 02:00", "Host notes · api-1", "Sent when api-1 is in scope", "From this conversation · agent summary",
		"After reading panes", "The agent read terminal output for this answer, and this text may come from it, not from you. Check it before saving: a saved note is sent to the agent with every question that reads this host.",
		"Nothing is saved until you press Save. Notes live in the vault; edit them in Edit host or Settings · Agent.", "Used host notes · api-1 (2)"); len(miss) > 0 {
		t.Errorf("missing %q", miss)
	}
	if !r.has("Proposed note") || !r.has("Save anyway") || !r.has("Dismiss") || !r.has("Edit note") {
		t.Error("the card lacks a button")
	}
	// Nothing is kept by showing it, by waiting, or by a frame.
	r.tt.Frame()
	r.tt.Frame()
	if len(r.fb.mem.propReqs) != 0 || len(r.fb.mem.notes) != 0 {
		t.Fatalf("the proposal was acted on without a press: %v", r.fb.mem.propReqs)
	}
	// Dismiss keeps nothing.
	r.click(t, "Dismiss")
	if got := r.fb.mem.propReqs; len(got) != 1 || !strings.HasPrefix(got[0], "dismiss ") || len(r.fb.mem.notes) != 0 {
		t.Fatalf("requests %v, notes %v", got, r.fb.mem.notes)
	}
	if !r.tt.HasText("Dismissed. Nothing was saved.") || r.has("Save anyway") {
		t.Error("after Dismiss the card still offers Save")
	}
}

func TestSavingANoteProposalSendsTheUsersEditAndUndoDeletesIt(t *testing.T) {
	r := agentRig(t)
	r.answersNotes(noteProposal())
	r.ask(t, "please remember this")
	conv := r.fb.agent.asks[0].ConversationID
	r.click(t, "Save") // no pane was read for the card: the plain Save
	if got := r.fb.mem.propReqs; !slices.Equal(got, []string{"save " + conv + "/np1:"}) {
		t.Fatalf("requests %v", got)
	}
	if !r.tt.HasText("Saved to host notes · api-1") || !r.has("Undo") || len(r.fb.mem.notes) != 1 {
		t.Fatal("after Save the card does not say so")
	}
	r.click(t, "Undo")
	if got := r.fb.mem.propReqs; len(got) != 2 || got[1] != "undo "+conv+"/np1" || len(r.fb.mem.notes) != 0 {
		t.Fatalf("requests %v", got)
	}
	if !r.has("Save") || r.tt.HasText("Saved to host notes · api-1") {
		t.Error("Undo did not reopen the card")
	}
	// Edit, then Save: the edited text is what is sent, never the shown one.
	r.click(t, "Edit note")
	r.tt.Key(ui.Cmd, ui.KeyA)
	r.tt.Type("backups run at 03:00 with the secret " + fakeSecret)
	r.tt.Frame()
	r.click(t, "Save")
	last := r.fb.mem.propReqs[len(r.fb.mem.propReqs)-1]
	if last != "save "+conv+"/np1:backups run at 03:00 with the secret "+fakeSecret {
		t.Fatalf("sent %q", last)
	}
	if !r.tt.HasText("Saved. 1 secret-looking value was replaced first.") {
		t.Error("the count is not told")
	}
	if r.onScreen(fakeSecret) {
		t.Fatal("the value shows on screen")
	}
}

func TestANoteFromPaneOutputOrAboutYouIsWarnedAndOnlySaveAnywaySaves(t *testing.T) {
	r := agentRig(t)
	p := noteProposal()
	p.FromPane, p.SeenIn, p.From = true, "db-2", "pane"
	r.answersNotes(p, keelclient.NoteProposal{ID: "np2", For: "user", Text: "I like short answers", From: "user", Status: "open", PanesRead: []string{"api-1"}})
	r.ask(t, "remember from the pane")
	if miss := r.lacks("From pane output", "Seen in pane db-2 · terminal output", noteFromPaneWarning, "Seen in pane db-2; it would be saved to the notes of api-1.",
		"Your notes · every host", "The agent read terminal output for this answer, and this note may come from it, not from you. A note about you is sent with questions on every host the agent may read. Save it only if you would have written it yourself.",
		"This answer read api-1; this note would be sent on every host."); len(miss) > 0 {
		t.Errorf("missing %q", miss)
	}
	if r.has("Save") {
		t.Error("a warned note has a plain Save")
	}
	if len(r.fb.mem.propReqs) != 0 {
		t.Fatalf("requests %v", r.fb.mem.propReqs)
	}
}

func TestANoteProposalThatExpiredCanBeReadNotSaved(t *testing.T) {
	r := agentRig(t)
	p := noteProposal()
	p.Expired = true
	r.answersNotes(p)
	r.ask(t, "remember")
	if !r.tt.HasText("Not saved. This proposal expired; add the note yourself in Edit host or Settings · Agent.") || r.has("Save") || r.has("Undo") {
		t.Error("an expired card offers Save or Undo, or lacks its words")
	}
	r.w.saveNoteProposal("np1") // the second lock
	if len(r.fb.mem.propReqs) != 0 {
		t.Fatalf("an expired proposal was saved: %v", r.fb.mem.propReqs)
	}
}

func TestAResumedConversationShowsItsNotesAndRunbooksCards(t *testing.T) {
	r := agentRig(t)
	saved := noteProposal()
	saved.Status, saved.NoteID = "saved", "n9"
	r.w.showTranscript(keelclient.Transcript{ID: "c-saved-1", Title: "Notes", Turns: []keelclient.SavedTurn{{Question: "remember it", Answer: "ok", Notes: []keelclient.NoteProposal{saved},
		Runbooks: []keelclient.RunbookProposal{{ID: "bp1", Name: "Disk", Status: "dismissed"}}}}})
	r.tt.Frame()
	if miss := r.lacks("Saved to host notes · api-1", "Dismissed. Nothing was saved."); len(miss) > 0 {
		t.Errorf("missing %q", miss)
	}
	if len(r.fb.mem.propReqs) != 0 {
		t.Fatalf("showing a transcript acted on a proposal: %v", r.fb.mem.propReqs)
	}
}

func TestNotesAndRunbooksAreNeverWrittenToDisk(t *testing.T) {
	r := agentRig(t)
	dir := t.TempDir()
	r.w.dataDir = dir
	r.answersNotes(noteProposal())
	r.ask(t, "remember it")
	r.click(t, "Save")
	r.w.editHost(localHost)
	r.tt.Frame()
	r.addNoteTo(t, "a host note nobody writes down")
	r.w.closeHostDialog()
	r.w.openPage("runbooks")
	r.tt.Frame()
	r.w.newRunbook()
	r.tt.Frame()
	r.click(t, "Runbook name")
	r.tt.Type("a runbook nobody writes down")
	r.tt.Frame()
	r.w.setAgentOpen(true) // a prefs write after all of it was on screen
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		for _, s := range []string{"backups run at 02:00", "a host note nobody writes down", "a runbook nobody writes down", "Remember this"} {
			if strings.Contains(string(b), s) {
				t.Errorf("%s holds note or runbook text %q", e.Name(), s)
			}
		}
	}
}
