package native

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// History and Recently deleted, in the window, and the palette's notebook items.

func nbHistoryRig(t *testing.T) *rig {
	t.Helper()
	r := nbRig(t, "line a\nline b\n")
	r.fb.nb.history["nb1"] = []keelclient.NotebookVersion{
		{ID: "v2", NotebookID: "nb1", Name: "Deploy", By: "agent", Event: "before-agent-edit", Summary: "Adds a step", At: time.Now().Add(-2 * time.Minute), Cells: []keelclient.NotebookCell{{ID: "c", Body: "line a"}}},
		{ID: "v1", NotebookID: "nb1", Name: "Deploy", By: "user", Event: "session", At: time.Now().Add(-90 * time.Minute), Cells: []keelclient.NotebookCell{{ID: "c", Body: "line a\nline b\nline c"}}},
	}
	return r
}

func TestHistoryListsTheVersionsAndDiffsTheSelectedOneAgainstNow(t *testing.T) {
	r := nbHistoryRig(t)
	r.w.openNotebookHistory("nb1")
	r.frames(3)
	if miss := r.lacks("History · Deploy", historyDesc, "Before the agent's edit", "Then: Adds a step", "2 min ago", "Your version", "Delete this version", "Close", "Restore this version"); len(miss) > 0 {
		t.Fatalf("missing %q in %q", miss, r.tt.Texts())
	}
	// The newest is selected: "line a" against now ("line a\nline b\n"): two lines added (the empty last one counts).
	if !r.tt.HasText("From this version to now: 2 lines added · 0 removed") {
		t.Fatalf("diff head: %q", r.tt.Texts())
	}
	if !r.tt.HasText("line b") {
		t.Error("the added line is not shown")
	}
	// Selecting the older version: "line c" removed.
	r.click(t, "Your version")
	r.frames(2)
	if !r.tt.HasText("From this version to now: 1 line added · 1 removed") || !r.tt.HasText("line c") {
		t.Fatalf("older diff: %q", r.tt.Texts())
	}
	if r.fb.nb.reqsWith("restore") != 0 || r.fb.nb.reqsWith("purge") != 0 {
		t.Error("opening History changed something")
	}
}

func TestHistorySavesFirstAndRestorePostsTheRevAndSaysSo(t *testing.T) {
	r := nbHistoryRig(t)
	np := r.openNB(t, "nb1")
	r.click(t, "Edit")
	r.frames(2)
	r.tt.Type("typed ")
	r.frames(1)
	r.w.openNotebookHistory("nb1") // saves what was typed first
	r.frames(3)
	if r.fb.nb.reqsWith("put nb1") != 1 {
		t.Fatalf("not saved before History: %v", r.fb.nb.reqs)
	}
	rev := r.fb.nb.list[0].Rev
	r.click(t, "Restore this version")
	r.frames(3)
	if !slices.ContainsFunc(r.fb.nb.reqs, func(s string) bool {
		return strings.HasPrefix(s, "restore nb1 v") && strings.HasSuffix(s, " rev="+strconv.Itoa(rev))
	}) {
		t.Fatalf("restore: %v (the rev the page shows is %d)", r.fb.nb.reqs, rev)
	}
	if !r.tt.HasText(restoredWord) || r.w.nbDlg != nil {
		t.Errorf("no toast, or the dialog stayed: %q", r.tt.Texts())
	}
	// The newest kept version is the one the user's save replaced: the text before they typed.
	if np.nb.md != "line a\nline b\n" || strings.Contains(np.nb.md, "typed") {
		t.Errorf("the open pane shows %q, not the restored text", np.nb.md)
	}
}

func TestDeleteThisVersionAsksFirst(t *testing.T) {
	r := nbHistoryRig(t)
	r.w.openNotebookHistory("nb1")
	r.frames(3)
	r.click(t, "Delete this version")
	r.frames(2)
	if cf := r.w.confirm; cf == nil || cf.message != purgeVersion || cf.title != "Delete version" || cf.action != "Delete" {
		t.Fatalf("confirm %+v", cf)
	}
	if r.fb.nb.reqsWith("purge") != 0 {
		t.Fatal("purged before the answer")
	}
	r.click(t, "Cancel")
	r.frames(2)
	if r.fb.nb.reqsWith("purge") != 0 || r.w.nbDlg == nil {
		t.Fatal("Cancel purged, or closed the dialog")
	}
	r.click(t, "Delete this version")
	r.frames(2)
	r.w.confirm.run()
	r.w.confirm = nil
	r.frames(2)
	if !slices.Contains(r.fb.nb.reqs, "purge nb1 v2") || len(r.w.nbDlg.versions) != 1 || r.w.nbDlg.selected != "v1" {
		t.Fatalf("reqs %v dialog %+v", r.fb.nb.reqs, r.w.nbDlg)
	}
}

func TestAnEmptyHistoryAndAnEmptyTrashSaySo(t *testing.T) {
	r := nbRig(t, "x")
	r.w.openNotebookHistory("nb1")
	r.frames(3)
	if !r.tt.HasText(historyNone) {
		t.Fatalf("history empty text: %q", r.tt.Texts())
	}
	r.w.nbDlg = nil
	r.w.openNotebookTrash()
	r.frames(3)
	if miss := r.lacks("Recently deleted", trashDesc, trashNone); len(miss) > 0 {
		t.Fatalf("trash lacks %q", miss)
	}
}

func TestRecentlyDeletedRestoresAndDeletesForeverAfterAsking(t *testing.T) {
	r := nbRig(t, "kept\n", "gone one\n")
	r.fb.nb.trash = []keelclient.NotebookVersion{
		{ID: "t2", NotebookID: "nb2", Name: "Backups", Event: "deleted", KeptAt: time.Now().Add(-time.Hour), Cells: []keelclient.NotebookCell{{Body: "gone one\nand more"}}},
		{ID: "t3", NotebookID: "nb3", Name: "Old", Event: "deleted", KeptAt: time.Now().Add(-48 * time.Hour), Cells: []keelclient.NotebookCell{{Body: "older"}}},
	}
	r.fb.nb.list = r.fb.nb.list[:1]
	r.w.reloadNotebooks()
	r.w.openNotebookTrash()
	r.frames(3)
	if miss := r.lacks("Recently deleted", "Backups", "Old", "gone one", "Delete forever", "Restore notebook"); len(miss) > 0 {
		t.Fatalf("missing %q in %q", miss, r.tt.Texts())
	}
	// Delete forever asks first.
	r.click(t, "Delete forever")
	r.frames(2)
	if cf := r.w.confirm; cf == nil || cf.message != purgeNotebook {
		t.Fatalf("confirm %+v", r.w.confirm)
	}
	if r.fb.nb.reqsWith("purge-deleted") != 0 {
		t.Fatal("purged before the answer")
	}
	r.w.confirm = nil
	r.frames(2)
	// Restore notebook puts the selected one back in Notebooks.
	r.click(t, "Old")
	r.frames(2)
	r.click(t, "Restore notebook")
	r.frames(3)
	if !slices.Contains(r.fb.nb.reqs, "restore-deleted t3") {
		t.Fatalf("reqs %v", r.fb.nb.reqs)
	}
	if !r.tt.HasText("Restored “Old” to Notebooks.") {
		t.Errorf("no toast: %q", r.tt.Texts())
	}
	if _, ok := r.w.notebook("nb3"); !ok {
		t.Error("the restored notebook is not in the list")
	}
}

func TestThePaletteOpensNotebooksAndNothingTypes(t *testing.T) {
	r := nbRig(t, twoFences)
	r.fb.nb.list[0].Pinned = true
	r.w.reloadNotebooks()
	r.tt.Key(ui.Cmd, ui.KeyK)
	r.tt.Frame()
	r.tt.Type("Open notebook")
	r.tt.Frame()
	var rows []string
	for _, it := range r.rows() {
		rows = append(rows, it.title+"|"+it.hint)
	}
	if !slices.Contains(rows, "Open notebook Deploy|Pinned") {
		t.Fatalf("rows %q", rows)
	}
	r.tt.Key(0, ui.KeyEnter)
	r.frames(3)
	if len(r.w.tab().panes) != 2 || r.nbPane(t).nb.id != "nb1" {
		t.Fatal("the palette did not open the notebook beside the terminal")
	}
	// New notebook and Split notebook and the page.
	r.tt.Key(ui.Cmd, ui.KeyK)
	r.tt.Frame()
	r.tt.Type("Split notebook")
	r.tt.Frame()
	r.tt.Key(0, ui.KeyEnter)
	r.frames(3)
	if len(r.w.tab().panes) != 3 {
		t.Fatalf("Split notebook: %d panes", len(r.w.tab().panes))
	}
	r.tt.Key(ui.Cmd, ui.KeyK)
	r.tt.Frame()
	r.tt.Type("Notebooks")
	r.tt.Frame()
	titles := []string{}
	for _, it := range r.rows() {
		titles = append(titles, it.title)
	}
	if !slices.Contains(titles, "Notebooks") || !slices.Contains(titles, "New notebook") {
		t.Fatalf("titles %q", titles)
	}
	r.tt.Key(0, ui.KeyEscape)
	r.frames(2)
	if len(r.typed()) != 0 || len(r.directs()) != 0 {
		t.Error("the palette typed")
	}
	// ⌘N on the page is New notebook.
	r.w.openPage("notebooks")
	r.frames(2)
	n := len(r.fb.nb.list)
	r.tt.Key(ui.Cmd, ui.KeyN)
	r.frames(3)
	if len(r.fb.nb.list) != n+1 || r.w.hostDlg != nil {
		t.Errorf("⌘N on the page did not make a notebook: %d -> %d", n, len(r.fb.nb.list))
	}
}

func TestClosingANotebookPaneSavesWhatWasTypedAndTypesNothing(t *testing.T) {
	r := nbRig(t, "text\n")
	np := r.openNB(t, "nb1")
	r.click(t, "Edit")
	r.frames(2)
	r.tt.Type("late ")
	r.frames(1)
	tb := r.w.tab()
	r.w.closePane(tb, np.id)
	r.frames(2)
	if r.fb.nb.reqsWith("put nb1") != 1 {
		t.Fatalf("what was typed was not saved on close: %v", r.fb.nb.reqs)
	}
	if len(tb.panes) != 1 || len(r.typed()) != 0 || len(r.directs()) != 0 {
		t.Error("closing the pane changed the tab, or typed")
	}
	if len(r.fb.killed) != 0 {
		t.Errorf("closing a notebook killed %v", r.fb.killed)
	}
}
