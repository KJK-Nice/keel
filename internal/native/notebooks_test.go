package native

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// Notebooks on the fake backend: the sidebar section, the page, the row menu, the pane (read rendered, edit as Markdown),
// and typing a fence, which only ever goes through typeNow and the Approve before type dialog.

const twoFences = "# Deploy\n\nSteps:\n\n- check disk\n- restart\n\n```sh\ndf -h\n```\n```bash\nuptime\n```\n```go\nx := 1\n```\n"

// nbRig is a window with one session this client drives, at its shell's prompt, and the notebooks below on the server.
func nbRig(t *testing.T, mds ...string) *rig {
	t.Helper()
	fb := okBackend()
	for i, md := range mds {
		fb.addNotebook([]string{"Deploy", "Backups", "Scratch"}[i], md)
	}
	r := newRig(t, fb)
	r.tt.SetSize(1400, 1000)
	r.tt.Frame()
	r.click(t, "host api-1")
	st := drivingState
	r.fb.conns[0].st = &st
	r.screen(false, "user@h $")
	r.fb.conns[0].directRes = keelclient.DirectResult{Typed: true}
	r.tt.Frame()
	return r
}

// nbPane is the open notebook pane of the tab shown.
func (r *rig) nbPane(t *testing.T) *pane {
	t.Helper()
	for _, id := range r.w.tab().root.Leaves() {
		if p := r.w.tab().panes[id]; p != nil && p.nb != nil {
			return p
		}
	}
	t.Fatal("no notebook pane in this tab")
	return nil
}

func (r *rig) frames(n int) {
	for range n {
		r.tt.Frame()
	}
}

func TestTheSidebarListsNotebooksPinnedFirstAndFiltersThem(t *testing.T) {
	r := nbRig(t, "alpha text", "beta text", "gamma text")
	r.fb.nb.list[2].Pinned = true
	r.w.reloadNotebooks()
	r.w.sideSeg = "snippets"
	r.frames(2)
	if miss := r.lacks("Notebooks", "sidebar notebook Scratch", "sidebar notebook Deploy", "sidebar notebook Backups", "New notebook", "All notebooks"); len(miss) > 0 {
		t.Fatalf("missing %q in %q", miss, r.tt.Texts())
	}
	var order []string
	for _, n := range r.w.shownNotebooks("") {
		order = append(order, n.Name)
	}
	if !slices.Equal(order, []string{"Scratch", "Deploy", "Backups"}) && !slices.Equal(order, []string{"Scratch", "Backups", "Deploy"}) {
		t.Fatalf("order %v: the pinned one is first", order)
	}
	r.w.snipQuery = "beta"
	r.frames(2)
	if r.onScreen("sidebar notebook Deploy") || !r.onScreen("sidebar notebook Backups") {
		t.Errorf("the filter reads the name and the first line: %q", r.tt.Texts())
	}
	r.w.snipQuery = "nothing like this"
	r.frames(2)
	if !r.tt.HasText("Nothing matches “nothing like this”") {
		t.Error("no no-match state")
	}
	r.w.notebooks = nil
	r.w.snipQuery = ""
	r.frames(2)
	if !r.tt.HasText("No notebooks yet") {
		t.Error("no 'No notebooks yet'")
	}
}

func TestTheNotebooksPageShowsRowsBadgesAndTheEmptyState(t *testing.T) {
	r := nbRig(t, "first line here\nmore", "b", "c")
	r.fb.nb.list[0].Private, r.fb.nb.list[1].AgentUnreviewed, r.fb.nb.list[2].Source = true, true, "agent"
	r.fb.nb.list[2].Pinned = true
	r.w.openPage("notebooks")
	r.frames(3)
	if miss := r.lacks("Notebooks", notebooksSubtitle, "Recently deleted", "New notebook", "Filter notebooks", "notebook Deploy", "first line here",
		"badge Private", "badge Edited by agent", "badge From agent", "badge Pinned"); len(miss) > 0 {
		t.Fatalf("missing %q in %q", miss, r.tt.Texts())
	}
	r.w.notebooks = nil
	r.frames(2)
	if miss := r.lacks("No notebooks yet", "Write markdown runbooks. Fenced sh and bash blocks fill a terminal's input; nothing is typed until you confirm."); len(miss) > 0 {
		t.Fatalf("empty state lacks %q", miss)
	}
}

func TestEveryRowMenuItemDoesItsOneThing(t *testing.T) {
	r := nbRig(t, "text a", "text b")
	r.w.openPage("notebooks")
	r.frames(3)
	if err := r.tt.RightClick("notebook Deploy"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if got := r.tt.Menu(); !slices.Equal(got, []string{"Open", "Rename", "Pin", "Make private", "History", "-", "Ask the agent about this notebook", "-", "Delete"}) {
		t.Fatalf("menu %q", got)
	}
	// Pin.
	if err := r.tt.ChooseMenuItem("Pin"); err != nil {
		t.Fatal(err)
	}
	r.frames(2)
	if !r.fb.nb.list[0].Pinned || !slices.Contains(r.fb.nb.reqs, "pinned=true") {
		t.Fatalf("pin: %v", r.fb.nb.reqs)
	}
	// Make private says so.
	r.tt.RightClick("notebook Deploy")
	r.tt.Frame()
	if got := r.tt.Menu(); !slices.Contains(got, "Unpin") {
		t.Fatalf("a pinned notebook offers Unpin: %q", got)
	}
	r.tt.ChooseMenuItem("Make private")
	r.frames(2)
	if !r.fb.nb.list[0].Private || !r.tt.HasText("Private: the agent cannot see, read or change this notebook.") {
		t.Fatalf("private: %v", r.fb.nb.reqs)
	}
	r.tt.RightClick("notebook Deploy")
	r.tt.Frame()
	if got := r.tt.Menu(); !slices.Contains(got, "Let the agent read it") {
		t.Fatalf("a private notebook offers 'Let the agent read it': %q", got)
	}
	r.tt.ChooseMenuItem("Let the agent read it")
	r.frames(2)
	if r.fb.nb.list[0].Private || !r.tt.HasText("The agent may read and change this notebook again.") {
		t.Fatalf("private off: %v", r.fb.nb.reqs)
	}
	// Open puts a pane beside the terminal... from a page it is a tab of its own, named after the notebook.
	r.tt.RightClick("notebook Backups")
	r.tt.Frame()
	r.tt.ChooseMenuItem("Open")
	r.frames(2)
	if t2 := r.w.tab(); t2.isPage() || t2.name() != "Backups" || !t2.onlyNotebooks() {
		t.Fatalf("Open from a page: tab %q only notebooks %v", t2.name(), t2.onlyNotebooks())
	}
}

func TestRenameAsksForANameAndPatchesOnlyTheName(t *testing.T) {
	r := nbRig(t, "text a")
	r.w.openPage("notebooks")
	r.frames(2)
	r.tt.RightClick("notebook Deploy")
	r.tt.Frame()
	r.tt.ChooseMenuItem("Rename")
	r.frames(2)
	if !r.tt.HasText("Rename notebook") || !r.tt.Focused("Notebook rename name") {
		t.Fatalf("no dialog or no focus: %q", r.tt.Texts())
	}
	r.tt.Key(ui.Cmd, ui.KeyA)
	r.tt.Type("Release notes")
	r.frames(1)
	r.click(t, "Rename")
	r.frames(2)
	if r.fb.nb.list[0].Name != "Release notes" || !slices.Contains(r.fb.nb.reqs, "name=Release notes") || r.fb.nb.reqsWith("put") != 0 {
		t.Fatalf("rename: name %q reqs %v: a rename is a PATCH, never a save of the text", r.fb.nb.list[0].Name, r.fb.nb.reqs)
	}
	if r.w.nbDlg != nil || !r.onScreen("notebook Release notes") {
		t.Error("the dialog did not close, or the row kept the old name")
	}
}

func TestDeleteAsksFirstAndAsksAgainWhenKeelServeCannotKeepIt(t *testing.T) {
	r := nbRig(t, "text a", "text b")
	r.w.openPage("notebooks")
	r.frames(2)
	r.tt.RightClick("notebook Deploy")
	r.tt.Frame()
	r.tt.ChooseMenuItem("Delete")
	r.frames(2)
	if !r.tt.HasText("Delete notebook “Deploy”?") || r.fb.nb.reqsWith("delete") != 0 {
		t.Fatalf("no question, or deleted before it: %q %v", r.tt.Texts(), r.fb.nb.reqs)
	}
	r.click(t, "Cancel")
	r.frames(2)
	if r.fb.nb.reqsWith("delete") != 0 || r.w.confirm != nil {
		t.Fatal("Cancel deleted")
	}
	r.tt.RightClick("notebook Deploy")
	r.tt.Frame()
	r.tt.ChooseMenuItem("Delete")
	r.frames(2)
	r.click(t, "Remove")
	r.frames(2)
	if _, ok := r.w.notebook("nb1"); ok || !slices.Contains(r.fb.nb.reqs, "delete nb1") {
		t.Fatalf("delete: %v", r.fb.nb.reqs)
	}
	// The history refusal: asks the permanent question and retries with ?permanent=1.
	r.fb.nb.deleteErr = &keelclient.StatusError{Code: 409, Reason: "history", Msg: "Keel cannot keep this notebook in Recently deleted right now. Delete it permanently?"}
	r.tt.RightClick("notebook Backups")
	r.tt.Frame()
	r.tt.ChooseMenuItem("Delete")
	r.frames(2)
	r.click(t, "Remove")
	r.frames(2)
	if !r.tt.HasText("Keel cannot keep “Backups” in Recently deleted right now, so it cannot be restored. Delete it permanently?") {
		t.Fatalf("no permanent question: %q", r.tt.Texts())
	}
	if r.fb.nb.reqsWith("delete nb2 permanent") != 0 {
		t.Fatal("deleted for good before the answer")
	}
	if cf := r.w.confirm; cf == nil || cf.title != "Delete permanently" || cf.action != "Delete permanently" { // its title and its button share words: the question is checked, then its action is run
		t.Fatalf("confirm %+v", cf)
	} else {
		r.w.endConfirm(true)
		cf.run()
	}
	r.frames(2)
	if !slices.Contains(r.fb.nb.reqs, "delete nb2 permanent") {
		t.Fatalf("no permanent delete: %v", r.fb.nb.reqs)
	}
	if _, ok := r.w.notebook("nb2"); ok {
		t.Error("still listed")
	}
}

func TestANotebookOpensBesideTheTerminalOfASessionTab(t *testing.T) {
	r := nbRig(t, twoFences)
	r.w.sideSeg = "snippets"
	r.frames(2)
	r.click(t, "sidebar notebook Deploy")
	r.frames(3)
	tb := r.w.tab()
	if len(tb.panes) != 2 || tb.onlyNotebooks() || r.nbPane(t).nb.id != "nb1" {
		t.Fatalf("panes %d", len(tb.panes))
	}
	if !r.tt.HasText("Notebook") || !r.tt.HasText("Rename notebook") {
		t.Fatalf("no pane: %q", r.tt.Texts())
	}
	// Opened again: the same pane, not a second.
	r.w.openNotebook("nb1")
	r.frames(2)
	if len(tb.panes) != 2 {
		t.Fatalf("a second pane: %d", len(tb.panes))
	}
	// A notebook is not a session: the status bar counts the terminal only.
	if !r.tt.HasText("keel serve · 1 session") {
		t.Error("the status bar counts the notebook as a session")
	}
	if r.fb.nb.reqsWith("put") != 0 || r.fb.nb.reqsWith("post") != 0 {
		t.Errorf("opening saved something: %v", r.fb.nb.reqs)
	}
	if len(r.typed()) != 0 || len(r.directs()) != 0 {
		t.Error("opening typed something")
	}
}
