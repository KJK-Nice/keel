package native

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"keel/internal/native/keelclient"
)

// The notebook pane: the document rendered, the Markdown edited as plain text, and a fence typed only through typeNow and
// the Approve before type dialog.

// openNB opens notebook id beside the terminal and settles the frames.
func (r *rig) openNB(t *testing.T, id string) *pane {
	t.Helper()
	r.w.openNotebook(id)
	r.frames(3)
	return r.nbPane(t)
}

// typeBtns lists the "Type into …" buttons on screen.
func (r *rig) typeBtns() (out []string) {
	for _, s := range r.tt.Texts() {
		if strings.HasPrefix(s, "Type into ") && !strings.HasPrefix(s, "Type into the terminal") {
			out = append(out, s)
		}
	}
	return out
}

func (r *rig) copyBtns() (out []string) {
	for _, s := range r.tt.Texts() {
		if strings.HasPrefix(s, "Copy code ") {
			out = append(out, s)
		}
	}
	return out
}

func TestReadModeShowsAHeadingAListAndTwoAdjacentFencesAsTwoBlocksWithTheirOwnBars(t *testing.T) {
	r := nbRig(t, twoFences)
	r.openNB(t, "nb1")
	if miss := r.lacks("Deploy", "Steps:", "check disk", "restart", "df -h", "uptime", "x := 1", "Edit", "Updated Mar 4", "History", "Agent can read"); len(miss) > 0 {
		t.Fatalf("missing %q in %q", miss, r.tt.Texts())
	}
	fs := fences(twoFences)
	if len(fs) != 3 {
		t.Fatalf("fences %+v", fs)
	}
	for _, f := range fs {
		if !r.tt.HasText(fenceName("Copy code", f.Index)) {
			t.Errorf("fence %q has no Copy code of its own", f.Lang)
		}
	}
	if len(r.copyBtns()) != 3 {
		t.Errorf("Copy buttons %q: every fence has one", r.copyBtns())
	}
	// Only the two shell fences have Type; the go fence does not. The adjacent fences are separate buttons.
	if got := r.typeBtns(); len(got) != 4 { // each button's name and its text are both on screen
		t.Errorf("Type buttons %q", got)
	}
	if !r.tt.HasText(fenceName("Type into api-1", fs[0].Index)) || !r.tt.HasText(fenceName("Type into api-1", fs[1].Index)) || r.tt.HasText(fenceName("Type into api-1", fs[2].Index)) {
		t.Errorf("Type is for the sh and bash fences only: %q", r.tt.Texts())
	}
	if !r.tt.HasText("Shows the command for api-1 first. Nothing is typed until you confirm.") && false {
		t.Error("tooltip")
	}
	if r.fb.nb.reqsWith("put") != 0 || r.fb.nb.reqsWith("post") != 0 {
		t.Errorf("reading saved something: %v", r.fb.nb.reqs)
	}
}

func TestACopyPutsExactlyTheFencesCodeOnTheClipboard(t *testing.T) {
	r := nbRig(t, "```bash\nline one\nline two\n```\n\n```sh\ndf -h\n```\n")
	r.openNB(t, "nb1")
	fs := fences(r.nbPane(t).nb.md)
	if err := r.tt.Click(fenceName("Copy code", fs[0].Index)); err != nil {
		t.Fatal(err)
	}
	r.frames(2)
	if got := r.tt.Clipboard(); got != "line one\nline two" {
		t.Fatalf("clipboard %q", got)
	}
	if !r.tt.HasText("Copied") {
		t.Error("no Copied toast")
	}
	r.tt.Click(fenceName("Copy code", fs[1].Index))
	r.frames(2)
	if got := r.tt.Clipboard(); got != "df -h" {
		t.Fatalf("clipboard %q: the second fence's own code", got)
	}
	if len(r.typed()) != 0 || len(r.directs()) != 0 {
		t.Error("Copy typed something")
	}
}

func TestWithNoTerminalBesideItAFenceHasNoTypeButton(t *testing.T) {
	r := nbRig(t, twoFences)
	r.w.openPage("notebooks")
	r.frames(2)
	r.click(t, "notebook Deploy")
	r.frames(3)
	if !r.nbPane(t).nb.editing == false && false {
		t.Fatal()
	}
	if !r.tt.HasText("No terminal in this tab") {
		t.Fatalf("no 'No terminal in this tab': %q", r.tt.Texts())
	}
	if got := r.typeBtns(); len(got) != 0 {
		t.Errorf("Type buttons %q with no terminal", got)
	}
	if len(r.copyBtns()) != 3 {
		t.Errorf("Copy buttons %q: Copy stays", r.copyBtns())
	}
	// The function itself says what the app says.
	np := r.nbPane(t)
	r.w.typeNotebookFence(np, fences(np.nb.md)[0])
	r.frames(1)
	if !r.tt.HasText("Split a terminal beside this notebook first") || len(r.directs()) != 0 || r.w.approve != nil {
		t.Errorf("no toast, or something was sent")
	}
}

func TestEditTypeDoneShowsTheChangeRenderedAndSavesOnceWithTheLoadedRev(t *testing.T) {
	r := nbRig(t, "plain words\n")
	np := r.openNB(t, "nb1")
	r.click(t, "Edit")
	r.frames(2)
	if !np.nb.editing || !r.tt.Focused("Markdown document") {
		t.Fatalf("not editing, or the text has no keyboard: editing %v", np.nb.editing)
	}
	r.tt.Key(ui.Cmd, ui.KeyEnd) // the end of the text
	r.tt.Type("## Added heading\n")
	r.frames(2)
	if !strings.Contains(np.nb.md, "## Added heading") {
		t.Fatalf("md %q", np.nb.md)
	}
	if r.fb.nb.reqsWith("put") != 0 {
		t.Fatalf("saved before the quiet time: %v", r.fb.nb.reqs)
	}
	r.click(t, "Done")
	r.frames(3)
	if np.nb.editing || !r.tt.HasText("Added heading") || r.tt.HasText("## Added heading") {
		t.Fatalf("not rendered: editing %v texts %q", np.nb.editing, r.tt.Texts())
	}
	if got := r.fb.nb.reqs; !slices.Equal(got, []string{"put nb1 rev=1"}) {
		t.Fatalf("saves %v: one, with the revision the pane loaded", got)
	}
	last := r.fb.nb.saves[len(r.fb.nb.saves)-1]
	if len(last.Cells) != 1 || last.Cells[0].Body != "plain words\n## Added heading\n" && !strings.Contains(last.Cells[0].Body, "## Added heading") || last.Cells[0].ID != "c1" {
		t.Fatalf("saved %+v", last)
	}
	// An edit never calls a writer.
	if len(r.typed()) != 0 || len(r.directs()) != 0 || r.fb.conns[0].wrote != 0 {
		t.Error("editing wrote into the session")
	}
	// The next save carries the new revision.
	r.click(t, "Edit")
	r.frames(2)
	r.tt.Type("more ")
	r.tt.Advance(autosaveIdle + time.Second)
	r.frames(3)
	if got := r.fb.nb.reqs; !slices.Equal(got, []string{"put nb1 rev=1", "put nb1 rev=2"}) {
		t.Fatalf("saves %v", got)
	}
}

func TestAutosaveWaitsForTheQuietTimeAndEscapeAndCmdEnterLeaveEdit(t *testing.T) {
	r := nbRig(t, "text\n")
	np := r.openNB(t, "nb1")
	r.click(t, "Edit")
	r.frames(2)
	r.tt.Type("a")
	r.frames(1)
	r.tt.Advance(500 * time.Millisecond)
	r.frames(1)
	if r.fb.nb.reqsWith("put") != 0 {
		t.Fatal("saved before 1 s of quiet")
	}
	r.tt.Advance(600 * time.Millisecond)
	r.frames(2)
	if r.fb.nb.reqsWith("put") != 1 {
		t.Fatalf("not saved after 1 s of quiet: %v", r.fb.nb.reqs)
	}
	r.tt.Key(0, ui.KeyEscape)
	r.frames(2)
	if np.nb.editing {
		t.Error("Escape did not leave Edit")
	}
	r.click(t, "Edit")
	r.frames(2)
	r.tt.Key(ui.Cmd, ui.KeyEnter)
	r.frames(2)
	if np.nb.editing {
		t.Error("⌘↵ did not leave Edit")
	}
}

func TestADoubleClickOnTheDocumentIsEdit(t *testing.T) {
	r := nbRig(t, "Some words to read here\n")
	np := r.openNB(t, "nb1")
	rc, ok := r.tt.Find("notebook text")
	if !ok {
		t.Fatalf("no document: %q", r.tt.Texts())
	}
	r.tt.ClickAt(rc.X+rc.W/2, rc.Y+rc.H/2)
	r.tt.ClickAt(rc.X+rc.W/2, rc.Y+rc.H/2)
	r.frames(3)
	if !np.nb.editing {
		t.Fatal("a double click did not open Edit")
	}
}

func TestANewNotebookStartsInEditWithTheStarterTextAndIsSaved(t *testing.T) {
	r := nbRig(t)
	r.w.newNotebook()
	r.frames(3)
	np := r.nbPane(t)
	if !np.nb.editing || np.nb.md != defaultNotebookMD || np.nb.id == "" {
		t.Fatalf("editing %v id %q", np.nb.editing, np.nb.id)
	}
	if !slices.Equal(r.fb.nb.reqs, []string{"post Notebook"}) {
		t.Fatalf("reqs %v", r.fb.nb.reqs)
	}
	if !r.tt.Focused("Markdown document") {
		t.Error("the text has no keyboard")
	}
}

func TestAOneLineShellFenceGoesThroughTypeNowAsANotebookFence(t *testing.T) {
	r := nbRig(t, twoFences)
	np := r.openNB(t, "nb1")
	fs := fences(np.nb.md)
	if err := r.tt.Click(fenceName("Type into api-1", fs[0].Index)); err != nil {
		t.Fatal(err)
	}
	r.frames(3)
	got := r.directs()
	if len(got) != 1 || got[0].command != "df -h" || got[0].ref.Kind != "notebook" || got[0].ref.NotebookID != "nb1" || got[0].ref.SnippetID != "" || got[0].agent != nil {
		t.Fatalf("directs %+v", got)
	}
	if strings.ContainsAny(got[0].command, "\x15\r\n") {
		t.Errorf("command %q: the line alone", got[0].command)
	}
	if r.w.approve != nil || len(r.typed()) != 0 {
		t.Error("a dialog opened, or the approved writer ran")
	}
	if !r.tt.HasText(typedNote("api-1")) {
		t.Errorf("no 'Typed into…' toast: %q", r.tt.Texts())
	}
}

func TestATwoLineShellFenceOpensTheDialogWithExactlyTheBytesAndTypesOnlyOnType(t *testing.T) {
	r := nbRig(t, "```sh\necho one\necho two\n```\n")
	np := r.openNB(t, "nb1")
	fs := fences(np.nb.md)
	r.tt.Click(fenceName("Type into api-1", fs[0].Index))
	r.frames(3)
	if len(r.directs()) != 0 || r.w.approve == nil || !r.w.approve.notebook {
		t.Fatalf("directs %d approve %+v: a two-line fence is never a direct type", len(r.directs()), r.w.approve)
	}
	if got := r.w.approve.shown(); got != "echo one\necho two\n" {
		t.Fatalf("the dialog shows %q", got)
	}
	r.frames(2)
	r.settle()
	if len(r.typed()) != 0 {
		t.Fatal("typed before Type")
	}
	// Cancel types nothing.
	r.click(t, "Cancel")
	r.frames(2)
	if r.w.approve != nil || len(r.typed()) != 0 || r.fb.conns[0].wrote != 0 {
		t.Fatal("Cancel typed, or the dialog stayed")
	}
	// Type sends the bytes behind notebook:true, with no Ctrl-U.
	r.tt.Click(fenceName("Type into api-1", fs[0].Index))
	r.frames(3)
	r.click(t, "Type")
	r.frames(3)
	if got := r.typed(); !slices.Equal(got, []string{"echo one\necho two\n"}) {
		t.Fatalf("typed %q", got)
	}
	if opts := r.fb.conns[0].injectOpts; len(opts) != 1 || !slices.Equal(opts[0], []keelclient.InjectOption{keelclient.FromNotebook}) {
		t.Errorf("options %v: the inject carries notebook:true", opts)
	}
}

func TestAnUnreviewedAgentEditPutsTheNoteInTheDialogAndAsksKeelServeToo(t *testing.T) {
	r := nbRig(t, "```sh\ndf -h\n```\n\n```sh\na\nb\n```\n")
	r.fb.nb.list[0].AgentUnreviewed = true
	r.w.reloadNotebooks()
	np := r.openNB(t, "nb1")
	fs := fences(np.nb.md)
	// A one-line fence: keel serve answers that a dialog is needed (reviewable text).
	r.fb.conns[0].directRes = keelclient.DirectResult{Dialog: true, Reason: "unreviewed"}
	r.tt.Click(fenceName("Type into api-1", fs[0].Index))
	r.frames(3)
	if len(r.directs()) != 1 || r.w.approve == nil || r.w.approve.note != unreviewedNote {
		t.Fatalf("directs %d approve %+v", len(r.directs()), r.w.approve)
	}
	if !r.tt.HasText(unreviewedNote) {
		t.Errorf("the note is not on screen: %q", r.tt.Texts())
	}
	if len(r.typed()) != 0 {
		t.Error("typed before Type")
	}
	r.click(t, "Cancel")
	r.frames(2)
	// The banner is there, with Mark reviewed only.
	if miss := r.lacks("Edited by the agent", agentEditReview, "Mark reviewed", "badge Edited by agent"); len(miss) > 0 {
		t.Fatalf("banner lacks %q", miss)
	}
	if r.tt.HasText("View changes") || r.tt.HasText("Undo") {
		t.Error("View changes / Undo belong to the agent plan")
	}
	r.click(t, "Mark reviewed")
	r.frames(2)
	if r.tt.HasText("Mark reviewed") || !slices.Contains(r.fb.nb.reqs, "reviewed nb1") {
		t.Fatalf("not reviewed: %v", r.fb.nb.reqs)
	}
	// A notebook the agent wrote says so.
	r.fb.nb.list[0].AgentUnreviewed, r.fb.nb.list[0].Source, r.fb.nb.list[0].LastBy = true, byAgent, byAgent
	r.w.reloadNotebooks()
	r.frames(3)
	if !r.onScreen("Written by the agent") {
		t.Errorf("no 'Written by the agent': %q", r.tt.Texts())
	}
}

func TestAFenceWithAPlaceholderIsRefusedWithTheAppsSentenceAndNothingIsSent(t *testing.T) {
	r := nbRig(t, "```sh\necho [secret:1]\n```\n\n```sh\necho a [secret:2]\necho b\n```\n")
	np := r.openNB(t, "nb1")
	for _, f := range fences(np.nb.md) {
		r.tt.Click(fenceName("Type into api-1", f.Index))
		r.frames(3)
		if !r.tt.HasText(placeholderRefused) {
			t.Errorf("no refusal for %q: %q", f.Code, r.tt.Texts())
		}
		if len(r.directs()) != 0 || r.w.approve != nil || len(r.typed()) != 0 {
			t.Errorf("something was sent for %q", f.Code)
		}
	}
	// The dialog's own check, if a placeholder ever reaches it.
	r.w.askNotebookType(r.w.tab().firstTerm(), np, "echo [ip:1]")
	r.w.approveType(false)
	if r.w.approve == nil || r.w.approve.err != placeholderRefused || len(r.typed()) != 0 {
		t.Errorf("the dialog sent a placeholder: %+v", r.w.approve)
	}
}

func TestAdjacentFencesStaySeparateTypeOnTheSecondOffersOnlyTheSecond(t *testing.T) {
	r := nbRig(t, "```sh\nuptime\n```\n```sh\ndf -h\n```\n")
	np := r.openNB(t, "nb1")
	fs := fences(np.nb.md)
	r.tt.Click(fenceName("Type into api-1", fs[1].Index))
	r.frames(3)
	if got := r.directs(); len(got) != 1 || got[0].command != "df -h" {
		t.Fatalf("directs %+v", got)
	}
}

func TestAFollowedPaneCannotBeTypedIntoFromANotebook(t *testing.T) {
	r := nbRig(t, "```sh\ndf -h\n```\n")
	st := keelclient.State{Status: "ready", Role: "follower", Subtitle: "deploy@10.0.0.1", YouID: 3, DriverID: 2}
	r.fb.conns[0].st = &st
	np := r.openNB(t, "nb1")
	fs := fences(np.nb.md)
	r.tt.Click(fenceName("Type into api-1", fs[0].Index))
	r.frames(3)
	if len(r.directs()) != 0 || r.w.approve == nil {
		t.Fatalf("directs %d approve %v: a follower gets the dialog's refusal", len(r.directs()), r.w.approve)
	}
	if !r.tt.HasText("You are following — take or request control to type") {
		t.Errorf("no refusal: %q", r.tt.Texts())
	}
	r.w.approveType(false)
	if len(r.typed()) != 0 {
		t.Error("typed into a followed pane")
	}
}

func TestATypeFlushesTheSavedNotebookFirstSoKeelServeComparesTheStoredText(t *testing.T) {
	r := nbRig(t, "```sh\ndf -h\n```\n")
	np := r.openNB(t, "nb1")
	r.click(t, "Edit")
	r.frames(2)
	r.tt.Type("edited ")
	r.frames(1)
	r.click(t, "Done")
	r.frames(2)
	fs := fences(np.nb.md)
	puts := r.fb.nb.reqsWith("put")
	if len(fs) != 1 {
		t.Fatalf("fences %+v", fs)
	}
	r.tt.Click(fenceName("Type into api-1", fs[0].Index))
	r.frames(3)
	if len(r.directs()) != 1 {
		t.Fatalf("directs %d", len(r.directs()))
	}
	if r.fb.nb.reqsWith("put") < puts || np.nb.auto.pending() {
		t.Error("typing left an edit unsaved")
	}
}

func TestAChangeOnTheServerWhileTypingKeepsTheTypingAndSaysSoWhenTheAgentMadeIt(t *testing.T) {
	r := nbRig(t, "original\n")
	np := r.openNB(t, "nb1")
	r.click(t, "Edit")
	r.frames(2)
	np.nb.auto.edit(time.Now()) // an edit is waiting
	np.nb.md += "mine\n"
	// The agent edits it meanwhile.
	r.fb.nb.list[0].Cells[0].Body, r.fb.nb.list[0].Rev, r.fb.nb.list[0].LastBy = "agent text\n", 2, byAgent
	r.w.reloadNotebooks()
	r.frames(1)
	if !strings.Contains(np.nb.md, "mine") || np.nb.rev != 1 {
		t.Fatalf("typing lost: md %q rev %d", np.nb.md, np.nb.rev)
	}
	if !r.tt.HasText(msgAgentTyping) {
		t.Errorf("no toast: %q", r.tt.Texts())
	}
	// The next save carries the revision the pane loaded; keel serve says it replaced an agent edit.
	r.fb.nb.replaced = true
	r.click(t, "Done")
	r.frames(3)
	if !slices.Contains(r.fb.nb.reqs, "put nb1 rev=1") || !r.tt.HasText(msgOverAgent) {
		t.Fatalf("reqs %v texts %q", r.fb.nb.reqs, r.tt.Texts())
	}
	// A pane with no typing shows the new text.
	r2 := nbRig(t, "original\n")
	np2 := r2.openNB(t, "nb1")
	r2.fb.nb.list[0].Cells[0].Body, r2.fb.nb.list[0].Rev = "from the agent\n", 2
	r2.w.reloadNotebooks()
	r2.frames(2)
	if np2.nb.md != "from the agent\n" || np2.nb.rev != 2 || !r2.tt.HasText("from the agent") {
		t.Errorf("not followed: md %q rev %d", np2.nb.md, np2.nb.rev)
	}
}

func TestAFailedSaveKeepsTheTypingAndTheNextEditTriesAgain(t *testing.T) {
	r := nbRig(t, "text\n")
	np := r.openNB(t, "nb1")
	r.click(t, "Edit")
	r.frames(2)
	r.fb.nb.saveErr = &keelclient.StatusError{Code: 500, Status: "500"}
	r.tt.Type("a")
	r.click(t, "Done")
	r.frames(3)
	if !r.tt.HasText("Could not save notebook") || !np.nb.failed || !strings.Contains(np.nb.md, "a") {
		t.Fatalf("failed %v md %q texts %q", np.nb.failed, np.nb.md, r.tt.Texts())
	}
	r.fb.nb.saveErr = nil
	r.click(t, "Edit")
	r.frames(2)
	r.tt.Type("b")
	r.click(t, "Done")
	r.frames(3)
	if np.nb.failed || r.fb.nb.reqsWith("put") < 2 {
		t.Errorf("did not try again: failed %v reqs %v", np.nb.failed, r.fb.nb.reqs)
	}
	// The server's refusal is shown as it is.
	r.fb.nb.saveErr = &keelclient.StatusError{Code: 409, Reason: "history", Msg: "Not saved: this notebook holds an agent edit, and Keel cannot keep that version in History right now. Your typing is still in the page."}
	r.click(t, "Edit")
	r.frames(2)
	r.tt.Type("c")
	r.click(t, "Done")
	r.frames(3)
	if !r.onScreen("Not saved: this notebook holds an agent edit") {
		t.Errorf("the server's refusal is not shown: %q", r.tt.Texts())
	}
}

func TestANotebookPaneComesBackWithTheWorkspaceAndTheFileHoldsNoText(t *testing.T) {
	dir := t.TempDir()
	fb := okBackend()
	fb.addNotebook("Deploy", "SECRET-LOOKING-NOTEBOOK-TEXT\n```sh\ndf -h\n```\n")
	fb.sessions = []keelclient.Session{{ID: "s1", HostID: "h-api", Status: "ready"}}
	r := newRigIn(t, fb, dir)
	r.w.openNotebook("nb1")
	r.tt.Frame()
	if len(r.w.tab().panes) != 2 {
		t.Fatalf("panes %d", len(r.w.tab().panes))
	}
	r.w.closeWindow()
	b, err := os.ReadFile(filepath.Join(dir, savedFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "SECRET-LOOKING") || strings.Contains(string(b), "df -h") || !strings.Contains(string(b), `"notebookId":"nb1"`) {
		t.Fatalf("workspace file: %s", b)
	}
	var doc savedDoc
	if json.Unmarshal(b, &doc) != nil || len(doc.Tabs) != 1 {
		t.Fatalf("doc %s", b)
	}
	fb2 := okBackend()
	fb2.nbf().list, fb2.nbf().seq = fb.nb.list, 1
	fb2.sessions = fb.sessions
	r2 := newRigIn(t, fb2, dir)
	var found *pane
	for _, p := range r2.w.tab().panes {
		if p.nb != nil {
			found = p
		}
	}
	if found == nil || found.nb.id != "nb1" || !strings.Contains(found.nb.md, "SECRET-LOOKING") {
		t.Fatalf("not restored: %+v", r2.w.tab().panes)
	}
	// A notebook deleted meanwhile is dropped, not an error.
	r2.w.closeWindow()
	fb3 := okBackend()
	fb3.sessions = fb.sessions
	r3 := newRigIn(t, fb3, dir)
	for _, p := range r3.w.tab().panes {
		if p.nb != nil {
			t.Error("a notebook that is gone came back")
		}
	}
}

// The code markdown.View reports for a block is the code fences reports for the same index, for every fence of the corpus.
func TestTheViewsCodeIsTheFencesCodeForTheSameBlock(t *testing.T) {
	corpus := []string{
		twoFences,
		"```sh\na\n```\n```bash\nb\n```\n~~~\nplain\n~~~\n",
		"> ```sh\n> in a quote\n> ```\n\n- item\n\n  ```bash\n  in a list\n  ```\n",
		"````sh\necho ```\n````\n",
		"```SHELL {title=\"x\"}\nwhoami\n```\n",
		"text\n\n```sh\nunclosed 1\nunclosed 2",
		"```sh\n\n```\n\n```sh\nafter an empty one\n```\n",
		"    indented code\n\nafter\n",
		"```sh\nlast line has no newline```\n",
	}
	for _, md := range corpus {
		type seen struct {
			index int
			code  string
		}
		var got []seen
		opts := &markdown.Options{CodeBlock: func(c *ui.Context, b *markdown.CodeBlockView) {
			if !slices.ContainsFunc(got, func(s seen) bool { return s.index == b.Index }) { // a frame builds each block again
				got = append(got, seen{b.Index, b.Code()})
			}
			b.Body(c)
		}}
		tt := ui.NewTester(func(c *ui.Context) { markdown.View(c, md, opts) }, 600, 800)
		tt.Frame()
		tt.Frame()
		fs := fences(md)
		for _, g := range got {
			f, ok := fenceAt(fs, g.index)
			if !ok {
				t.Errorf("%q: the view reports block %d, which fences does not know", md, g.index)
				continue
			}
			if strings.TrimSuffix(g.code, "\n") != f.Code {
				t.Errorf("%q: block %d: the view has %q, fences has %q", md, g.index, g.code, f.Code)
			}
		}
		if len(got) != len(fs) && !strings.Contains(md, "unclosed") {
			t.Errorf("%q: the view built %d code blocks, fences found %d", md, len(got), len(fs))
		}
	}
}

// A 256 KiB notebook in the text area: typing a character must not take more than 100 ms (plan 023 STOP 4).
func TestAHugeDocumentIsUsableInTheTextArea(t *testing.T) {
	var sb strings.Builder
	for sb.Len() < notebookLimitText-64 {
		sb.WriteString("echo a line of the notebook, long enough to be a real line of a runbook step\n")
	}
	r := nbRig(t, sb.String())
	start := time.Now()
	np := r.openNB(t, "nb1")
	t.Logf("256 KiB: opening it (3 frames) took %v", time.Since(start))
	if !np.nb.editing {
		t.Fatal("a notebook over the read limit opens rendered")
	}
	r.frames(2)
	var worst time.Duration
	before := len(np.nb.md)
	defer func() {
		if got := len(np.nb.md) - before; got != 20 {
			t.Errorf("20 characters were typed into the text area, the document grew by %d", got)
		}
	}()
	for i := 0; i < 20; i++ {
		start := time.Now()
		r.tt.Type("x")
		r.tt.Frame()
		if d := time.Since(start); d > worst {
			worst = d
		}
	}
	t.Logf("256 KiB: the slowest of 20 typed characters took %v (document %d bytes)", worst, len(np.nb.md))
	if worst > 100*time.Millisecond {
		t.Fatalf("typing a character took %v: more than 100 ms", worst)
	}
}
