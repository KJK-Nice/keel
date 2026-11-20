package native

import (
	"context"
	"fmt"
	"strings"

	"slime/internal/native/slimeclient"
)

// The notes and runbooks half of fakeBackend: what the window asks of slime serve, recorded, and answered as the test says.
// Nothing here reads a pane or types. Text with fakeSecret in it is "redacted" the way slime serve would: replaced by
// [secret:1], and counted.

const fakeSecret = "fake-password-do-not-use"

type fakeMemory struct {
	notes    []slimeclient.Note
	noteSeq  int
	noteErr  error
	noteReqs []string // "add host:text", "edit id:text", "delete id", "delete-all"
	unread   bool
	gone     int // DeleteAllNotes

	propReqs []string // "save conv/id:text", "undo conv/id", "dismiss conv/id" (notes) and "save-book …"
	propErr  error

	runbooks []slimeclient.Runbook
	rbSeq    int
	rbReqs   []string // "post name", "put id name", "delete id", "pick id on", "draft conv"
	rbSaved  []slimeclient.RunbookInput
	rbErr    error
	draft    slimeclient.RunbookDraft
	draftErr error

	run       *slimeclient.ChatRunbook
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

func (f *fakeBackend) Notes(context.Context) (slimeclient.NoteList, error) {
	f.rec("notes")
	if f.mem.noteErr != nil {
		return slimeclient.NoteList{}, f.mem.noteErr
	}
	return slimeclient.NoteList{Notes: append([]slimeclient.Note{}, f.mem.notes...), Unreadable: f.mem.unread}, nil
}

func (f *fakeBackend) SaveNote(_ context.Context, in slimeclient.NoteInput) (slimeclient.SavedNote, error) {
	f.rec("save note")
	text, n := f.redactFake(in.Text)
	if in.ID != "" {
		f.mem.noteReqs = append(f.mem.noteReqs, "edit "+in.ID+":"+in.Text)
		for i := range f.mem.notes {
			if f.mem.notes[i].ID == in.ID {
				f.mem.notes[i].Text = text
				return slimeclient.SavedNote{Note: f.mem.notes[i], Redacted: n}, nil
			}
		}
		return slimeclient.SavedNote{}, &slimeclient.StatusError{Code: 404, Status: "404 Not Found"}
	}
	if in.GroupID != "" { // a folder's note (§17): about every host in it, never also a host's
		f.mem.noteReqs = append(f.mem.noteReqs, "add group:"+in.GroupID+":"+in.Text)
	} else {
		f.mem.noteReqs = append(f.mem.noteReqs, "add "+in.HostID+":"+in.Text)
	}
	if f.mem.noteErr != nil {
		return slimeclient.SavedNote{}, f.mem.noteErr
	}
	f.mem.noteSeq++
	note := slimeclient.Note{ID: fmt.Sprintf("n%d", f.mem.noteSeq), HostID: in.HostID, GroupID: in.GroupID, Text: text, Source: "user"}
	f.mem.notes = append(f.mem.notes, note)
	return slimeclient.SavedNote{Note: note, Redacted: n}, nil
}

func (f *fakeBackend) DeleteNote(_ context.Context, id string) error {
	f.rec("delete note")
	f.mem.noteReqs = append(f.mem.noteReqs, "delete "+id)
	var keep []slimeclient.Note
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

func (f *fakeBackend) SaveProposedNote(_ context.Context, conv, id, text string) (slimeclient.SavedNote, error) {
	f.rec("save proposed note")
	f.mem.propReqs = append(f.mem.propReqs, "save "+conv+"/"+id+":"+text)
	if f.mem.propErr != nil {
		return slimeclient.SavedNote{}, f.mem.propErr
	}
	kept, n := f.redactFake(text)
	if kept == "" {
		kept = "proposed text"
	}
	f.mem.noteSeq++
	note := slimeclient.Note{ID: fmt.Sprintf("n%d", f.mem.noteSeq), Text: kept, Source: "agent"}
	f.mem.notes = append(f.mem.notes, note)
	return slimeclient.SavedNote{Note: note, Redacted: n, Proposal: &slimeclient.NoteProposal{ID: id, For: "host", HostID: "h-api", Host: "api-1", Status: "saved", NoteID: note.ID, Text: kept}}, nil
}

func (f *fakeBackend) UndoProposedNote(_ context.Context, conv, id string) (slimeclient.NoteProposal, error) {
	f.rec("undo proposed note")
	f.mem.propReqs = append(f.mem.propReqs, "undo "+conv+"/"+id)
	f.mem.notes = nil
	return slimeclient.NoteProposal{ID: id, For: "host", HostID: "h-api", Host: "api-1", Status: "open", Text: "proposed text"}, nil
}

func (f *fakeBackend) DismissProposedNote(_ context.Context, conv, id string) (slimeclient.NoteProposal, error) {
	f.rec("dismiss proposed note")
	f.mem.propReqs = append(f.mem.propReqs, "dismiss "+conv+"/"+id)
	return slimeclient.NoteProposal{ID: id, For: "host", HostID: "h-api", Host: "api-1", Status: "dismissed", Text: "proposed text"}, nil
}

// ---- runbooks ----

func (f *fakeBackend) Runbooks(context.Context) (slimeclient.RunbookList, error) {
	f.rec("runbooks")
	return slimeclient.RunbookList{Runbooks: append([]slimeclient.Runbook{}, f.mem.runbooks...)}, nil
}

func (f *fakeBackend) SaveRunbook(_ context.Context, id string, in slimeclient.RunbookInput) (slimeclient.SavedRunbook, error) {
	f.rec("save runbook")
	f.mem.rbSaved = append(f.mem.rbSaved, in)
	if f.mem.rbErr != nil {
		return slimeclient.SavedRunbook{}, f.mem.rbErr
	}
	redacted := 0
	rb := slimeclient.Runbook{ID: id, Name: in.Name, When: in.When, Hosts: in.Hosts, AgentMayPick: in.AgentMayPick, Source: "user"}
	if in.DraftID != "" {
		rb.Source, rb.SourceRef = f.mem.draft.Source, f.mem.draft.SourceRef
	}
	for i, s := range in.Steps {
		cmd, n := f.redactFake(s.Command)
		redacted += n
		rb.Steps = append(rb.Steps, slimeclient.RunbookStep{ID: fmt.Sprintf("st%d", i+1), Title: s.Title, Description: s.Description, Command: cmd, Expect: s.Expect, Risk: "readonly", Role: s.Role, OneHost: s.OneHost, Captures: s.Captures})
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
	return slimeclient.SavedRunbook{Runbook: rb, Redacted: redacted}, nil
}

func (f *fakeBackend) DeleteRunbook(_ context.Context, id string) error {
	f.rec("delete runbook")
	f.mem.rbReqs = append(f.mem.rbReqs, "delete "+id)
	var keep []slimeclient.Runbook
	for _, r := range f.mem.runbooks {
		if r.ID != id {
			keep = append(keep, r)
		}
	}
	f.mem.runbooks = keep
	return nil
}

func (f *fakeBackend) SetRunbookPick(_ context.Context, id string, on bool) (slimeclient.Runbook, error) {
	f.rec("set runbook pick")
	f.mem.rbReqs = append(f.mem.rbReqs, fmt.Sprintf("pick %s %v", id, on))
	for i := range f.mem.runbooks {
		if f.mem.runbooks[i].ID == id {
			f.mem.runbooks[i].AgentMayPick = on
			return f.mem.runbooks[i], nil
		}
	}
	return slimeclient.Runbook{}, &slimeclient.StatusError{Code: 404, Status: "404 Not Found"}
}

func (f *fakeBackend) DraftRunbook(_ context.Context, conv string) (slimeclient.RunbookDraft, error) {
	f.rec("draft runbook")
	f.mem.rbReqs = append(f.mem.rbReqs, "draft "+conv)
	return f.mem.draft, f.mem.draftErr
}

func (f *fakeBackend) RunbookState(context.Context, string) (slimeclient.RunState, error) {
	f.rec("runbook state")
	f.mem.stateGets++
	return slimeclient.RunState{Runbook: f.mem.run, NoRunbook: f.mem.noRunbook}, nil
}

func (f *fakeBackend) StepAction(_ context.Context, conv, step, action string) (slimeclient.RunState, error) {
	f.rec("step action")
	f.mem.actions = append(f.mem.actions, conv+"/"+step+":"+action)
	if f.mem.runErr != nil {
		return slimeclient.RunState{}, f.mem.runErr
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
	return slimeclient.RunState{Runbook: f.mem.run, NoRunbook: f.mem.noRunbook}, nil
}

func (f *fakeBackend) StopRunbook(context.Context, string) (slimeclient.RunState, error) {
	f.rec("stop runbook")
	f.mem.stops++
	f.mem.noRunbook = true
	if f.mem.run != nil {
		f.mem.run.Status = "stopped"
	}
	return slimeclient.RunState{Runbook: f.mem.run, NoRunbook: true}, nil
}

func (f *fakeBackend) MoveRunbook(context.Context, string) (slimeclient.RunState, error) {
	f.rec("move runbook")
	f.mem.moves++
	if f.mem.run != nil {
		f.mem.run.PinHost, f.mem.run.PinSessionID, f.mem.run.Blocked = "", "", ""
	}
	return slimeclient.RunState{Runbook: f.mem.run, NoRunbook: f.mem.noRunbook}, nil
}

func (f *fakeBackend) SaveProposedRunbook(_ context.Context, conv, id string) (slimeclient.SavedRunbook, error) {
	f.rec("save proposed runbook")
	f.mem.propReqs = append(f.mem.propReqs, "save-book "+conv+"/"+id)
	if f.mem.propErr != nil {
		return slimeclient.SavedRunbook{}, f.mem.propErr
	}
	f.mem.rbSeq++
	rb := slimeclient.Runbook{ID: fmt.Sprintf("rb%d", f.mem.rbSeq), Name: "proposed", Source: "agent", AgentMayPick: false, Steps: []slimeclient.RunbookStep{{ID: "st1", Title: "One", Command: "echo fake"}}}
	f.mem.runbooks = append(f.mem.runbooks, rb)
	return slimeclient.SavedRunbook{Runbook: rb, Proposal: &slimeclient.RunbookProposal{ID: id, Name: "proposed", Status: "saved", RunbookID: rb.ID}}, nil
}

func (f *fakeBackend) UndoProposedRunbook(_ context.Context, conv, id string) (slimeclient.RunbookProposal, error) {
	f.rec("undo proposed runbook")
	f.mem.propReqs = append(f.mem.propReqs, "undo-book "+conv+"/"+id)
	f.mem.runbooks = nil
	return slimeclient.RunbookProposal{ID: id, Name: "proposed", Status: "open", PanesRead: []string{"api-1"}, Steps: []slimeclient.RunbookStep{{Title: "One", Command: "echo fake"}}}, nil
}

func (f *fakeBackend) DismissProposedRunbook(_ context.Context, conv, id string) (slimeclient.RunbookProposal, error) {
	f.rec("dismiss proposed runbook")
	f.mem.propReqs = append(f.mem.propReqs, "dismiss-book "+conv+"/"+id)
	return slimeclient.RunbookProposal{ID: id, Name: "proposed", Status: "dismissed"}, nil
}
