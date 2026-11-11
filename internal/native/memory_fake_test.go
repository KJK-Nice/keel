package native

import (
	"context"
	"fmt"
	"strings"

	"keel/internal/native/keelclient"
)

// The notes and runbooks half of fakeBackend: what the window asks of keel serve, recorded, and answered as the test says.
// Nothing here reads a pane or types. Text with fakeSecret in it is "redacted" the way keel serve would: replaced by
// [secret:1], and counted.

const fakeSecret = "fake-password-do-not-use"

type fakeMemory struct {
	notes    []keelclient.Note
	noteSeq  int
	noteErr  error
	noteReqs []string // "add host:text", "edit id:text", "delete id", "delete-all"
	unread   bool
	gone     int // DeleteAllNotes

	propReqs []string // "save conv/id:text", "undo conv/id", "dismiss conv/id" (notes) and "save-book …"
	propErr  error

	runbooks []keelclient.Runbook
	rbSeq    int
	rbReqs   []string // "post name", "put id name", "delete id", "pick id on", "draft conv"
	rbSaved  []keelclient.RunbookInput
	rbErr    error
	draft    keelclient.RunbookDraft
	draftErr error

	run       *keelclient.ChatRunbook
	noRunbook bool
	stateGets int
	actions   []string // "conv/step:action"
	stops     int
	moves     int
	runErr    error
}

func (f *fakeBackend) redactFake(s string) (string, int) {
	n := strings.Count(s, fakeSecret)
	return strings.ReplaceAll(s, fakeSecret, "[secret:1]"), n
}

func (f *fakeBackend) Notes(context.Context) (keelclient.NoteList, error) {
	f.rec("notes")
	if f.mem.noteErr != nil {
		return keelclient.NoteList{}, f.mem.noteErr
	}
	return keelclient.NoteList{Notes: append([]keelclient.Note{}, f.mem.notes...), Unreadable: f.mem.unread}, nil
}

func (f *fakeBackend) SaveNote(_ context.Context, in keelclient.NoteInput) (keelclient.SavedNote, error) {
	f.rec("save note")
	text, n := f.redactFake(in.Text)
	if in.ID != "" {
		f.mem.noteReqs = append(f.mem.noteReqs, "edit "+in.ID+":"+in.Text)
		for i := range f.mem.notes {
			if f.mem.notes[i].ID == in.ID {
				f.mem.notes[i].Text = text
				return keelclient.SavedNote{Note: f.mem.notes[i], Redacted: n}, nil
			}
		}
		return keelclient.SavedNote{}, &keelclient.StatusError{Code: 404, Status: "404 Not Found"}
	}
	f.mem.noteReqs = append(f.mem.noteReqs, "add "+in.HostID+":"+in.Text)
	if f.mem.noteErr != nil {
		return keelclient.SavedNote{}, f.mem.noteErr
	}
	f.mem.noteSeq++
	note := keelclient.Note{ID: fmt.Sprintf("n%d", f.mem.noteSeq), HostID: in.HostID, Text: text, Source: "user"}
	f.mem.notes = append(f.mem.notes, note)
	return keelclient.SavedNote{Note: note, Redacted: n}, nil
}

func (f *fakeBackend) DeleteNote(_ context.Context, id string) error {
	f.rec("delete note")
	f.mem.noteReqs = append(f.mem.noteReqs, "delete "+id)
	var keep []keelclient.Note
	for _, n := range f.mem.notes {
		if n.ID != id {
			keep = append(keep, n)
		}
	}
	f.mem.notes = keep
	return nil
}

func (f *fakeBackend) DeleteAllNotes(context.Context) (int, error) {
	f.rec("delete all notes")
	f.mem.noteReqs = append(f.mem.noteReqs, "delete-all")
	n := len(f.mem.notes)
	f.mem.notes, f.mem.gone = nil, f.mem.gone+1
	return n, nil
}

func (f *fakeBackend) SaveProposedNote(_ context.Context, conv, id, text string) (keelclient.SavedNote, error) {
	f.rec("save proposed note")
	f.mem.propReqs = append(f.mem.propReqs, "save "+conv+"/"+id+":"+text)
	if f.mem.propErr != nil {
		return keelclient.SavedNote{}, f.mem.propErr
	}
	kept, n := f.redactFake(text)
	if kept == "" {
		kept = "proposed text"
	}
	f.mem.noteSeq++
	note := keelclient.Note{ID: fmt.Sprintf("n%d", f.mem.noteSeq), Text: kept, Source: "agent"}
	f.mem.notes = append(f.mem.notes, note)
	return keelclient.SavedNote{Note: note, Redacted: n, Proposal: &keelclient.NoteProposal{ID: id, For: "host", HostID: "h-api", Host: "api-1", Status: "saved", NoteID: note.ID, Text: kept}}, nil
}

func (f *fakeBackend) UndoProposedNote(_ context.Context, conv, id string) (keelclient.NoteProposal, error) {
	f.rec("undo proposed note")
	f.mem.propReqs = append(f.mem.propReqs, "undo "+conv+"/"+id)
	f.mem.notes = nil
	return keelclient.NoteProposal{ID: id, For: "host", HostID: "h-api", Host: "api-1", Status: "open", Text: "proposed text"}, nil
}

func (f *fakeBackend) DismissProposedNote(_ context.Context, conv, id string) (keelclient.NoteProposal, error) {
	f.rec("dismiss proposed note")
	f.mem.propReqs = append(f.mem.propReqs, "dismiss "+conv+"/"+id)
	return keelclient.NoteProposal{ID: id, For: "host", HostID: "h-api", Host: "api-1", Status: "dismissed", Text: "proposed text"}, nil
}

// ---- runbooks ----

func (f *fakeBackend) Runbooks(context.Context) (keelclient.RunbookList, error) {
	f.rec("runbooks")
	return keelclient.RunbookList{Runbooks: append([]keelclient.Runbook{}, f.mem.runbooks...)}, nil
}

func (f *fakeBackend) SaveRunbook(_ context.Context, id string, in keelclient.RunbookInput) (keelclient.SavedRunbook, error) {
	f.rec("save runbook")
	f.mem.rbSaved = append(f.mem.rbSaved, in)
	if f.mem.rbErr != nil {
		return keelclient.SavedRunbook{}, f.mem.rbErr
	}
	redacted := 0
	rb := keelclient.Runbook{ID: id, Name: in.Name, When: in.When, Hosts: in.Hosts, AgentMayPick: in.AgentMayPick, Source: "user"}
	if in.DraftID != "" {
		rb.Source, rb.SourceRef = f.mem.draft.Source, f.mem.draft.SourceRef
	}
	for i, s := range in.Steps {
		cmd, n := f.redactFake(s.Command)
		redacted += n
		rb.Steps = append(rb.Steps, keelclient.RunbookStep{ID: fmt.Sprintf("st%d", i+1), Title: s.Title, Description: s.Description, Command: cmd, Expect: s.Expect, Risk: "readonly", Role: s.Role, OneHost: s.OneHost, Captures: s.Captures})
	}
	if id == "" {
		f.mem.rbSeq++
		rb.ID = fmt.Sprintf("rb%d", f.mem.rbSeq)
		f.mem.rbReqs = append(f.mem.rbReqs, "post "+in.Name)
		f.mem.runbooks = append(f.mem.runbooks, rb)
	} else {
		f.mem.rbReqs = append(f.mem.rbReqs, "put "+id+" "+in.Name)
		for i := range f.mem.runbooks {
			if f.mem.runbooks[i].ID == id {
				f.mem.runbooks[i] = rb
			}
		}
	}
	return keelclient.SavedRunbook{Runbook: rb, Redacted: redacted}, nil
}

func (f *fakeBackend) DeleteRunbook(_ context.Context, id string) error {
	f.rec("delete runbook")
	f.mem.rbReqs = append(f.mem.rbReqs, "delete "+id)
	var keep []keelclient.Runbook
	for _, r := range f.mem.runbooks {
		if r.ID != id {
			keep = append(keep, r)
		}
	}
	f.mem.runbooks = keep
	return nil
}

func (f *fakeBackend) SetRunbookPick(_ context.Context, id string, on bool) (keelclient.Runbook, error) {
	f.rec("set runbook pick")
	f.mem.rbReqs = append(f.mem.rbReqs, fmt.Sprintf("pick %s %v", id, on))
	for i := range f.mem.runbooks {
		if f.mem.runbooks[i].ID == id {
			f.mem.runbooks[i].AgentMayPick = on
			return f.mem.runbooks[i], nil
		}
	}
	return keelclient.Runbook{}, &keelclient.StatusError{Code: 404, Status: "404 Not Found"}
}

func (f *fakeBackend) DraftRunbook(_ context.Context, conv string) (keelclient.RunbookDraft, error) {
	f.rec("draft runbook")
	f.mem.rbReqs = append(f.mem.rbReqs, "draft "+conv)
	return f.mem.draft, f.mem.draftErr
}

func (f *fakeBackend) RunbookState(context.Context, string) (keelclient.RunState, error) {
	f.rec("runbook state")
	f.mem.stateGets++
	return keelclient.RunState{Runbook: f.mem.run, NoRunbook: f.mem.noRunbook}, nil
}

func (f *fakeBackend) StepAction(_ context.Context, conv, step, action string) (keelclient.RunState, error) {
	f.rec("step action")
	f.mem.actions = append(f.mem.actions, conv+"/"+step+":"+action)
	if f.mem.runErr != nil {
		return keelclient.RunState{}, f.mem.runErr
	}
	if r := f.mem.run; r != nil {
		for i := range r.Steps {
			if r.Steps[i].ID != step {
				continue
			}
			switch action {
			case "done":
				r.Steps[i].State, r.Steps[i].Result = "done", "user"
			case "skip":
				r.Steps[i].State, r.Steps[i].Result = "skipped", "user"
			case "retry":
				r.Steps[i].State, r.Steps[i].Result, r.Steps[i].Exit = "next", "", nil
			}
		}
		done := true
		for _, s := range r.Steps {
			done = done && (s.State == "done" || s.State == "skipped")
		}
		if done {
			r.Status = "finished"
		}
	}
	return keelclient.RunState{Runbook: f.mem.run, NoRunbook: f.mem.noRunbook}, nil
}

func (f *fakeBackend) StopRunbook(context.Context, string) (keelclient.RunState, error) {
	f.rec("stop runbook")
	f.mem.stops++
	f.mem.noRunbook = true
	if f.mem.run != nil {
		f.mem.run.Status = "stopped"
	}
	return keelclient.RunState{Runbook: f.mem.run, NoRunbook: true}, nil
}

func (f *fakeBackend) MoveRunbook(context.Context, string) (keelclient.RunState, error) {
	f.rec("move runbook")
	f.mem.moves++
	if f.mem.run != nil {
		f.mem.run.PinHost, f.mem.run.PinSessionID, f.mem.run.Blocked = "", "", ""
	}
	return keelclient.RunState{Runbook: f.mem.run, NoRunbook: f.mem.noRunbook}, nil
}

func (f *fakeBackend) SaveProposedRunbook(_ context.Context, conv, id string) (keelclient.SavedRunbook, error) {
	f.rec("save proposed runbook")
	f.mem.propReqs = append(f.mem.propReqs, "save-book "+conv+"/"+id)
	if f.mem.propErr != nil {
		return keelclient.SavedRunbook{}, f.mem.propErr
	}
	f.mem.rbSeq++
	rb := keelclient.Runbook{ID: fmt.Sprintf("rb%d", f.mem.rbSeq), Name: "proposed", Source: "agent", AgentMayPick: false, Steps: []keelclient.RunbookStep{{ID: "st1", Title: "One", Command: "echo fake"}}}
	f.mem.runbooks = append(f.mem.runbooks, rb)
	return keelclient.SavedRunbook{Runbook: rb, Proposal: &keelclient.RunbookProposal{ID: id, Name: "proposed", Status: "saved", RunbookID: rb.ID}}, nil
}

func (f *fakeBackend) UndoProposedRunbook(_ context.Context, conv, id string) (keelclient.RunbookProposal, error) {
	f.rec("undo proposed runbook")
	f.mem.propReqs = append(f.mem.propReqs, "undo-book "+conv+"/"+id)
	f.mem.runbooks = nil
	return keelclient.RunbookProposal{ID: id, Name: "proposed", Status: "open", PanesRead: []string{"api-1"}, Steps: []keelclient.RunbookStep{{Title: "One", Command: "echo fake"}}}, nil
}

func (f *fakeBackend) DismissProposedRunbook(_ context.Context, conv, id string) (keelclient.RunbookProposal, error) {
	f.rec("dismiss proposed runbook")
	f.mem.propReqs = append(f.mem.propReqs, "dismiss-book "+conv+"/"+id)
	return keelclient.RunbookProposal{ID: id, Name: "proposed", Status: "dismissed"}, nil
}
