package native

import (
	"context"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"keel/internal/native/keelclient"
	"keel/internal/native/layout"
)

// Notebooks (docs/ui-redesign.md §18; cmd/keel/web/js/term.js openNotebook, persistNotebook, notebookChanged and the
// rest around them). A notebook is one Markdown document kept by keel serve. The pane shows it rendered and, on Edit, as
// plain Markdown in a text area: the stored text is never passed through a rich text model (docs/native-notebook-spike.md).
//
// What this file does and does not do:
//   - The list of notebooks and the text of every open pane live in memory; the workspace file keeps a notebook pane's id
//     and place only (saved.go), never its text.
//   - A save sends the whole state with the revision the pane loaded. keel serve keeps the user's text over an agent edit
//     the pane had not loaded and puts the agent's version in History.
//   - Nothing here writes to a session. A shell fence reaches a terminal only through typeNow or the Approve before type
//     dialog (notebooktype.go); opening, rendering, editing, saving, restoring and deleting type nothing.

const (
	notebookLimitName = 120        // store.MaxNotebookNameBytes
	notebookLimitText = 256 * 1024 // store.MaxNotebookBytes
	notebookReadLimit = 64 * 1024  // a longer notebook opens in Edit: rendering it takes seconds

	// defaultNotebookMD is the starter text of a new notebook (term.js DEFAULT_NOTEBOOK_MD). The name is the title, so
	// it has no H1.
	defaultNotebookMD = "Write a runbook here.\n\n## Check\n\n1. Numbered lists render as steps\n2. Fenced ```sh or ```bash blocks type into the terminal beside the notebook after you confirm\n\n- Bullet lists render too\n"

	msgNoTermBeside = "Split a terminal beside this notebook first" // term.js insertNotebookIntoInput
	msgOverAgent    = "Saved your typing over an agent edit this pane had not loaded. The agent's version is in History."
	msgAgentTyping  = "The agent edited a notebook you are typing in. Your typing is kept; the agent's version is in History."
)

// notebookPane is the state of one open notebook.
type notebookPane struct {
	id      string // "" until the first save
	title   string // the Rename notebook field
	md      string // the document: the Markdown as typed
	cellID  string
	rev     int // the revision this pane loaded or saved last
	pinned  bool
	editing bool // the text area is shown instead of the rendered document

	titleSeen, mdSeen string // what the last frame saw, to find an edit
	auto              *autosave
	saving            bool
	failed            bool // the last save failed: typing that is only here
	gone              bool // the notebook was deleted or the pane closed: no save may bring it back
	waiters           []func(ok bool)
	toldRev           int // the server revision the typing-over-an-edit toast was shown for

	focusEdit bool // give the text area the keyboard on the next frame
	titleH    ui.Handle
	editH     ui.Handle
	opts      *markdown.Options
	clicked   map[int]time.Time // fence index -> when Copy was pressed (the label shows "Copied" for 2 s)
}

func newNotebookState() *notebookPane {
	return &notebookPane{auto: newAutosave(), clicked: map[int]time.Time{}}
}

// load takes a notebook record as the pane's text, title, revision and pin.
func (n *notebookPane) load(rec keelclient.Notebook) {
	n.id, n.title, n.rev, n.pinned = rec.ID, notebookName(rec), rec.Rev, rec.Pinned
	n.md = rec.Markdown()
	n.cellID = ""
	if len(rec.Cells) > 0 {
		n.cellID = rec.Cells[0].ID
	}
	if len(rec.Cells) == 0 {
		n.md = defaultNotebookMD // term.js copyNotebookCells: a notebook with no cells starts from the starter
	}
	n.titleSeen, n.mdSeen = n.title, n.md
	n.failed = false
}

// dirty is typing that keel serve does not have: a save is pending or running, or the last one failed.
func (n *notebookPane) dirty() bool { return n.auto.pending() || n.failed }

// payload is the whole state a save sends.
func (n *notebookPane) payload() keelclient.NotebookSave {
	name := strings.TrimSpace(n.title)
	if name == "" {
		name = "Notebook"
	}
	return keelclient.NotebookSave{Name: name, Cells: []keelclient.NotebookCell{{ID: n.cellID, Body: n.md}}, Pinned: n.pinned, Rev: n.rev}
}

// nbConn is the connection of a notebook pane: there is no session. It reports nothing and refuses everything, so a
// loop over a tab's panes can call it and no write can reach it.
type nbConn struct{}

//lint:ignore ST1005 shown to the user in the app's words
var errNotebookPane = errors.New("A notebook is not a terminal")

func (nbConn) Read([]byte) (int, error)                 { return 0, io.EOF }
func (nbConn) Write([]byte) (int, error)                { return 0, errNotebookPane }
func (nbConn) Close() error                             { return nil }
func (nbConn) Resize(int, int) error                    { return nil }
func (nbConn) SessionID() string                        { return "" }
func (nbConn) Snapshot() keelclient.State               { return keelclient.State{} }
func (nbConn) TakeControl() error                       { return errNotebookPane }
func (nbConn) RequestControl() error                    { return errNotebookPane }
func (nbConn) GrantControl(uint64) error                { return errNotebookPane }
func (nbConn) Events() []keelclient.Event               { return nil }
func (nbConn) OnChange(func())                          {}
func (nbConn) SuggestDetect(int, string) error          { return errNotebookPane }
func (nbConn) SuggestAsk(int, string) error             { return errNotebookPane }
func (nbConn) SuggestEvents() []keelclient.SuggestEvent { return nil }
func (nbConn) RestoreWords(string) error                { return errNotebookPane }
func (nbConn) SuggestInsert(context.Context, keelclient.SuggestionRef) error {
	return errNotebookPane
}
func (nbConn) ApprovedInject(context.Context, string, *keelclient.AgentRef, ...keelclient.InjectOption) error {
	return errNotebookPane
}
func (nbConn) DirectType(context.Context, string, keelclient.DirectRef, *keelclient.AgentRef) (keelclient.DirectResult, error) {
	return keelclient.DirectResult{}, errNotebookPane
}

// ---- the list ----

func (w *workspace) notebook(id string) (keelclient.Notebook, bool) {
	for _, n := range w.notebooks {
		if n.ID == id {
			return n, true
		}
	}
	return keelclient.Notebook{}, false
}

// shownNotebooks is the list under the filter (name and first line), sorted as the app sorts it.
func (w *workspace) shownNotebooks(q string) (out []keelclient.Notebook) {
	for _, n := range sortedNotebooks(w.notebooks) {
		if sideMatches(n.Name+" "+notebookPreview(n), q) {
			out = append(out, n)
		}
	}
	return out
}

// rememberNotebook puts a notebook keel serve answered with into the list at once (term.js rememberNotebook).
func (w *workspace) rememberNotebook(rec keelclient.Notebook) {
	if i := slices.IndexFunc(w.notebooks, func(n keelclient.Notebook) bool { return n.ID == rec.ID }); i >= 0 {
		w.notebooks[i] = rec
		return
	}
	w.notebooks = append([]keelclient.Notebook{rec}, w.notebooks...)
}

// reloadNotebooks reads the list again, and lets the open panes follow it.
func (w *workspace) reloadNotebooks() {
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		nbs, err := w.be.Notebooks(c)
		w.post(func() {
			switch {
			case w.closed:
			case err != nil:
				w.fail("notebooks", err)
			default:
				w.notebooks = nbs
				w.followNotebooks()
			}
		})
	})
}

// notebookPanes lists the open notebook panes.
func (w *workspace) notebookPanes() (out []*pane) {
	for _, t := range w.tabs {
		for _, id := range t.root.Leaves() {
			if p := t.panes[id]; p != nil && p.nb != nil {
				out = append(out, p)
			}
		}
	}
	return out
}

// followNotebooks is notebookChanged for every open pane: a pane with no typing shows what keel serve has; a pane with
// typing keeps it (its next save carries the revision it started from, so keel serve keeps the user's text and the agent's
// version in History), and says so once when the agent made the change.
func (w *workspace) followNotebooks() {
	for _, p := range w.notebookPanes() {
		n := p.nb
		rec, ok := w.notebook(n.id)
		if !ok || n.gone || rec.Rev <= n.rev {
			continue
		}
		if n.dirty() {
			if rec.LastBy == byAgent && n.toldRev < rec.Rev {
				n.toldRev = rec.Rev
				w.say(msgAgentTyping)
			}
			continue
		}
		n.load(rec)
	}
}

// ---- opening ----

// newNotebookPane makes a pane for rec, or for a new notebook (nil), which starts in Edit with the starter text.
func (w *workspace) newNotebookPane(rec *keelclient.Notebook) *pane {
	w.seq++
	p := &pane{id: strconv.Itoa(w.seq), conn: nbConn{}, nb: newNotebookState()}
	n := p.nb
	n.opts = &markdown.Options{}
	if rec != nil {
		n.load(*rec)
		// An empty notebook opens in Edit. So does one too big to render quickly: markdown.View lays out every block, which
		// took about 6 s for 256 KiB (about 1.5 s for 64 KiB) while the text area takes 40 ms.
		// shortcut: no windowing of the rendered view, upgrade when MyGo's Markdown view windows long documents.
		n.editing = strings.TrimSpace(n.md) == "" || len(n.md) > notebookReadLimit
	} else {
		n.title, n.md = "Notebook", defaultNotebookMD
		n.titleSeen, n.mdSeen = n.title, n.md
		n.editing, n.focusEdit = true, true
	}
	return p
}

// firstTerm is the terminal a notebook's fences type into: the tab's first terminal with a session (term.js termTarget).
func (t *tab) firstTerm() *pane {
	for _, id := range t.root.Leaves() {
		if p := t.panes[id]; p != nil && p.nb == nil {
			return p
		}
	}
	return nil
}

// onlyNotebooks is a tab whose panes are all notebooks.
func (t *tab) onlyNotebooks() bool { return len(t.panes) > 0 && t.firstTerm() == nil }

// placeNotebook shows a notebook pane: beside the terminal of a session tab, else in a tab of its own (term.js
// openNotebook).
func (w *workspace) placeNotebook(np *pane) {
	t := w.tab()
	if t != nil && !t.isNew() && !t.isPage() && !t.remote && t.root != nil && len(t.panes) > 0 {
		sibling := t.firstTerm()
		if sibling == nil {
			sibling = t.panes[t.focused]
		}
		if sibling != nil {
			t.root = t.root.Split(sibling.id, np.id, layout.Row)
			t.panes[np.id] = np
			t.focused, t.zoomed = np.id, ""
			w.wantFocus = np.id
			return
		}
	}
	w.tabs = append(w.tabs, &tab{root: layout.Leaf(np.id), panes: map[string]*pane{np.id: np}, focused: np.id})
	w.selectTab(len(w.tabs) - 1)
}

// openNotebook shows notebook id: its pane when one is open, else a new pane.
func (w *workspace) openNotebook(id string) {
	rec, ok := w.notebook(id)
	if !ok {
		w.say("Notebook not found")
		return
	}
	for i, t := range w.tabs {
		for _, pid := range t.root.Leaves() {
			if p := t.panes[pid]; p != nil && p.nb != nil && p.nb.id == id {
				w.selectTab(i)
				t.focused = p.id
				w.wantFocus = p.id
				return
			}
		}
	}
	w.placeNotebook(w.newNotebookPane(&rec))
}

// newNotebook is New notebook: a split beside the terminal of a session tab, else a tab of its own; it is saved at once
// (term.js newNotebook, splitNotebook).
func (w *workspace) newNotebook() {
	np := w.newNotebookPane(nil)
	w.placeNotebook(np)
	np.nb.auto.edit(time.Now())
	w.nbPump(np)
}

// splitNotebook is the palette's Split notebook: the same as New notebook.
func (w *workspace) splitNotebook() { w.newNotebook() }

// ---- saving ----

// nbPump starts a save of p when there is one to make and none is running; the answer starts another when the pane
// changed meanwhile (autosave.end).
func (w *workspace) nbPump(p *pane) {
	n := p.nb
	if n.saving {
		return
	}
	if !n.auto.begin() {
		w.nbSettle(p)
		return
	}
	n.saving = true
	in, id := n.payload(), n.id
	name := in.Name
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		var res keelclient.SavedNotebook
		var err error
		if id == "" {
			var rec keelclient.Notebook
			rec, err = w.be.CreateNotebook(c, in)
			res.Notebook = rec
		} else {
			res, err = w.be.SaveNotebook(c, id, in)
		}
		w.post(func() {
			n.saving = false
			again := n.auto.end()
			if w.closed {
				return
			}
			if err != nil {
				n.failed = true
				w.warn(nbErr(err, "Could not save notebook"))
				w.nbSettle(p)
				return
			}
			n.failed = false
			n.id, n.rev, n.pinned = res.ID, res.Rev, res.Pinned
			if len(res.Cells) > 0 {
				n.cellID = res.Cells[0].ID
			}
			if strings.TrimSpace(n.title) == name { // not typed over since: the name keel serve kept
				n.title = res.Name
				n.titleSeen = n.title
			}
			w.rememberNotebook(res.Notebook)
			if res.ReplacedAgentEdit {
				w.say(msgOverAgent)
			}
			if again {
				w.nbPump(p)
				return
			}
			w.nbSettle(p)
		})
	})
}

// nbSettle answers the callers waiting for p's saves once nothing is left to save.
func (w *workspace) nbSettle(p *pane) {
	n := p.nb
	if n.saving || n.auto.pending() {
		return
	}
	run := n.waiters
	n.waiters = nil
	for _, f := range run {
		f(!n.failed)
	}
}

// nbFlush saves what was typed and then calls done (term.js flushNotebookSaves). done runs on the UI thread, at once when
// nothing is pending; ok is false when the save failed.
func (w *workspace) nbFlush(p *pane, done func(ok bool)) {
	n := p.nb
	if n.gone || (!n.auto.pending() && !n.saving) {
		if done != nil {
			done(!n.failed)
		}
		return
	}
	if done != nil {
		n.waiters = append(n.waiters, done)
	}
	w.nbPump(p)
}

// nbFlushID flushes every open pane of notebook id, then calls done.
func (w *workspace) nbFlushID(id string, done func()) {
	var open []*pane
	for _, p := range w.notebookPanes() {
		if p.nb.id == id {
			open = append(open, p)
		}
	}
	left := len(open)
	if left == 0 {
		done()
		return
	}
	for _, p := range open {
		w.nbFlush(p, func(bool) {
			if left--; left == 0 {
				done()
			}
		})
	}
}

// nbClosed is a notebook pane going away: what was typed is saved first (the pane closed right after typing used to lose it).
func (w *workspace) nbClosed(p *pane) {
	if p.nb.gone {
		return
	}
	w.nbFlush(p, nil)
	p.nb.gone = true
}

// flushNotebooksNow saves every open notebook before the window closes, and waits for the answers.
func (w *workspace) flushNotebooksNow() {
	for _, p := range w.notebookPanes() {
		n := p.nb
		if n.gone || !n.auto.pending() || n.saving {
			continue
		}
		n.auto.begin()
		c, cancel := ctx()
		if n.id == "" {
			_, _ = w.be.CreateNotebook(c, n.payload())
		} else {
			_, _ = w.be.SaveNotebook(c, n.id, n.payload())
		}
		cancel()
		n.auto.end()
	}
}

// nbErr is the server's reason for a refused save or write, else the fallback.
func nbErr(err error, fallback string) string {
	var se *keelclient.StatusError
	if errors.As(err, &se) && se.Msg != "" {
		return se.Msg
	}
	return fallback
}

// ---- the user's own changes: rename, pin, Private, reviewed, delete ----

// applyNotebook takes a record keel serve answered a change with into the list and the panes that show it. A pane with
// typing keeps it.
func (w *workspace) applyNotebook(rec keelclient.Notebook) {
	w.rememberNotebook(rec)
	for _, p := range w.notebookPanes() {
		if n := p.nb; n.id == rec.ID && !n.dirty() {
			n.load(rec)
		}
	}
}

func (w *workspace) renameNotebook(id, name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Notebook"
	}
	w.nbFlushID(id, func() {
		w.work(func() {
			c, cancel := ctx()
			defer cancel()
			rec, err := w.be.PatchNotebook(c, id, &name, nil) // the name only: a window with an old copy never writes old text back
			w.post(func() {
				if w.closed {
					return
				}
				if err != nil {
					w.warn(nbErr(err, "Could not rename notebook"))
					return
				}
				w.applyNotebook(rec)
				w.nbDlg = nil
			})
		})
	})
}

func (w *workspace) toggleNotebookPin(id string) {
	rec, ok := w.notebook(id)
	if !ok {
		return
	}
	pin := !rec.Pinned
	w.nbFlushID(id, func() {
		w.work(func() {
			c, cancel := ctx()
			defer cancel()
			saved, err := w.be.PatchNotebook(c, id, nil, &pin)
			w.post(func() {
				if w.closed {
					return
				}
				if err != nil {
					w.warn(nbErr(err, "Could not pin notebook"))
					return
				}
				w.applyNotebook(saved)
			})
		})
	})
}

func (w *workspace) toggleNotebookPrivate(id string) {
	rec, ok := w.notebook(id)
	if !ok {
		return
	}
	next := !rec.Private
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		saved, err := w.be.PutNotebookPrivacy(c, id, next)
		w.post(func() {
			if w.closed {
				return
			}
			if err != nil {
				w.warn(nbErr(err, "Could not change Private"))
				return
			}
			w.noteMarks(saved)
			if next {
				w.say("Private: the agent cannot see, read or change this notebook.")
			} else {
				w.say("The agent may read and change this notebook again.")
			}
		})
	})
}

// noteMarks takes a notebook's flags (Private, Edited by agent) without touching its text (term.js notebookMarksChanged).
func (w *workspace) noteMarks(rec keelclient.Notebook) {
	cur, ok := w.notebook(rec.ID)
	if !ok {
		w.rememberNotebook(rec)
		return
	}
	cur.Private, cur.AgentUnreviewed, cur.AgentEditedAt = rec.Private, rec.AgentUnreviewed, rec.AgentEditedAt
	w.rememberNotebook(cur)
}

func (w *workspace) markNotebookReviewed(id string) {
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		saved, err := w.be.MarkNotebookReviewed(c, id)
		w.post(func() {
			if w.closed {
				return
			}
			if err != nil {
				w.warn(nbErr(err, "Could not mark the notebook reviewed"))
				return
			}
			w.noteMarks(saved)
		})
	})
}

func (w *workspace) askDeleteNotebook(id string) {
	rec, ok := w.notebook(id)
	if !ok {
		return
	}
	w.askConfirm(&hostConfirm{title: "Confirm", message: "Delete notebook “" + notebookName(rec) + "”?", action: "Remove", run: func() { w.deleteNotebook(id, false) }})
}

// deleteNotebook deletes after the user confirmed. Pending edits land first (a save after the delete would put the notebook
// back); when keel serve cannot keep it in Recently deleted it asks before deleting for good (term.js deleteNotebook).
func (w *workspace) deleteNotebook(id string, permanent bool) {
	rec, _ := w.notebook(id)
	w.nbFlushID(id, func() {
		w.work(func() {
			c, cancel := ctx()
			defer cancel()
			err := w.be.DeleteNotebook(c, id, permanent)
			w.post(func() {
				if w.closed {
					return
				}
				switch {
				case err == nil:
					w.removeNotebook(id)
				case !permanent && keelclient.IsHistoryRefusal(err):
					w.askConfirm(&hostConfirm{title: "Delete permanently", action: "Delete permanently",
						message: "Keel cannot keep “" + notebookName(rec) + "” in Recently deleted right now, so it cannot be restored. Delete it permanently?",
						run:     func() { w.deleteNotebook(id, true) }})
				default:
					w.warn(nbErr(err, "Could not delete notebook"))
				}
			})
		})
	})
}

// removeNotebook drops a deleted notebook from the list and closes its panes.
func (w *workspace) removeNotebook(id string) {
	w.notebooks = slices.DeleteFunc(w.notebooks, func(n keelclient.Notebook) bool { return n.ID == id })
	for _, p := range w.notebookPanes() {
		if p.nb.id != id {
			continue
		}
		p.nb.gone = true
		for _, t := range w.tabs {
			if t.panes[p.id] == p {
				w.closePane(t, p.id)
				break
			}
		}
	}
}
