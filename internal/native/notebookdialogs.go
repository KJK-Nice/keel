package native

import (
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The Rename notebook dialog, a notebook's History and Notebooks · Recently deleted: internal/ui/dialogs.templ
// notebookRenameDialog and notebookHistoryDialog, with cmd/keel/web/js/ui/notebookhistory.js and term.js
// saveNotebookRename. History and Recently deleted are one dialog: the versions on the left, the lines that changed from
// the selected version to now (or the deleted notebook as text) on the right.

// The dialog's words (ui/notebookhistory.js).
const (
	historyDesc   = "Keel keeps the version before every agent edit and every restore, and one when you start editing again. Up to 20 versions, for 30 days."
	historyNone   = "No earlier versions yet. Keel keeps one before every agent edit and restore, and when you start editing again."
	historyLeft   = "No earlier versions left."
	trashTitle    = "Recently deleted"
	trashDesc     = "Deleted notebooks stay here for 30 days (at most 50). Restore puts one back in Notebooks."
	trashNone     = "Nothing was deleted in the last 30 days."
	loadingWord   = "Loading…"
	restoredWord  = "Restored. The version it replaced is in History."
	purgeVersion  = "Delete this version for good? It cannot be restored."
	purgeNotebook = "Delete this notebook for good? It cannot be restored."
)

// nbDialog is the open dialog: kind "rename", "history" or "trash".
type nbDialog struct {
	kind string
	id   string // the notebook, for rename and history

	name  string // rename: the field
	focus bool   // the field has been given the keyboard

	title, desc string
	versions    []keelclient.NotebookVersion
	selected    string
	texts       map[string]string // version id -> its text
	textErr     map[string]string
	empty       string // what the list says instead of rows ("Loading…", the empty words, an error)
	busy        bool
	now         time.Time

	nameH, cancelH, closeH, restoreH, purgeH ui.Handle
}

// ---- rename ----

func (w *workspace) openRenameNotebook(id string) {
	rec, ok := w.notebook(id)
	if !ok {
		return
	}
	w.nbDlg = &nbDialog{kind: "rename", id: id, name: notebookName(rec), focus: true}
}

// ---- History and Recently deleted ----

// openNotebookHistory saves what was typed, then lists the notebook's versions and selects the newest.
func (w *workspace) openNotebookHistory(id string) {
	rec, ok := w.notebook(id)
	if !ok {
		return
	}
	w.nbFlushID(id, func() {
		d := &nbDialog{kind: "history", id: id, title: "History · " + notebookName(rec), desc: historyDesc, texts: map[string]string{}, textErr: map[string]string{}, empty: loadingWord, now: time.Now()}
		w.nbDlg = d
		w.work(func() {
			c, cancel := ctx()
			defer cancel()
			vs, err := w.be.NotebookHistory(c, id)
			w.post(func() { w.nbLoaded(d, vs, err, historyNone, "Could not load History") })
		})
	})
}

func (w *workspace) openNotebookTrash() {
	d := &nbDialog{kind: "trash", title: trashTitle, desc: trashDesc, texts: map[string]string{}, textErr: map[string]string{}, empty: loadingWord, now: time.Now()}
	w.nbDlg = d
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		vs, err := w.be.DeletedNotebooks(c)
		w.post(func() { w.nbLoaded(d, vs, err, trashNone, "Could not load Recently deleted") })
	})
}

func (w *workspace) nbLoaded(d *nbDialog, vs []keelclient.NotebookVersion, err error, none, failed string) {
	if w.closed || w.nbDlg != d {
		return
	}
	if err != nil {
		d.empty = nbErr(err, failed)
		return
	}
	d.versions, d.empty = vs, ""
	if len(vs) == 0 {
		d.empty = none
		return
	}
	w.nbSelect(d, vs[0].ID)
}

// nbSelect selects a version and reads its text.
func (w *workspace) nbSelect(d *nbDialog, vid string) {
	d.selected = vid
	if _, ok := d.texts[vid]; ok {
		return
	}
	id, trash := d.id, d.kind == "trash"
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		var v keelclient.NotebookVersion
		var err error
		if trash {
			v, err = w.be.DeletedNotebook(c, vid)
		} else {
			v, err = w.be.NotebookVersion(c, id, vid)
		}
		w.post(func() {
			if w.closed {
				return
			}
			if err != nil {
				d.textErr[vid] = nbErr(err, "Could not load this version")
				return
			}
			d.texts[vid] = versionText(v)
		})
	})
}

// nbRestore is Restore this version (saves first, posts the rev the page shows) or Restore notebook.
func (w *workspace) nbRestore(d *nbDialog) {
	vid := d.selected
	if vid == "" || d.busy {
		return
	}
	d.busy = true
	if d.kind == "trash" {
		w.work(func() {
			c, cancel := ctx()
			defer cancel()
			saved, err := w.be.RestoreDeletedNotebook(c, vid)
			w.post(func() {
				d.busy = false
				if w.closed {
					return
				}
				if err != nil {
					w.warn(nbErr(err, "Could not restore"))
					return
				}
				w.rememberNotebook(saved)
				w.nbDlg = nil
				w.say("Restored “" + notebookName(saved) + "” to Notebooks.")
			})
		})
		return
	}
	id := d.id
	w.nbFlushID(id, func() {
		rec, _ := w.notebook(id)
		w.work(func() {
			c, cancel := ctx()
			defer cancel()
			saved, err := w.be.RestoreNotebookVersion(c, id, vid, rec.Rev)
			w.post(func() {
				d.busy = false
				if w.closed {
					return
				}
				if err != nil {
					w.warn(nbErr(err, "Could not restore"))
					return
				}
				w.applyNotebook(saved)
				w.nbDlg = nil
				w.say(restoredWord)
			})
		})
	})
}

// nbAskPurge is Delete this version or Delete forever: it asks first.
func (w *workspace) nbAskPurge(d *nbDialog) {
	vid := d.selected
	if vid == "" || d.busy {
		return
	}
	trash := d.kind == "trash"
	title, msg := "Delete version", purgeVersion
	if trash {
		title, msg = "Delete forever", purgeNotebook
	}
	w.askConfirm(&hostConfirm{title: title, message: msg, action: "Delete", run: func() { w.nbPurge(d, vid) }})
}

func (w *workspace) nbPurge(d *nbDialog, vid string) {
	trash, id := d.kind == "trash", d.id
	d.busy = true
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		var err error
		if trash {
			err = w.be.PurgeDeletedNotebook(c, vid)
		} else {
			err = w.be.PurgeNotebookVersion(c, id, vid)
		}
		w.post(func() {
			d.busy = false
			if w.closed {
				return
			}
			if err != nil {
				w.warn(nbErr(err, "Could not delete"))
				return
			}
			rest := d.versions[:0:0]
			for _, v := range d.versions {
				if v.ID != vid {
					rest = append(rest, v)
				}
			}
			d.versions = rest
			delete(d.texts, vid)
			d.selected = ""
			switch {
			case len(rest) > 0:
				w.nbSelect(d, rest[0].ID)
			case trash:
				d.empty = trashNone
			default:
				d.empty = historyLeft
			}
		})
	})
}

// ---- drawing ----

func (w *workspace) notebookDialogView(c *ui.Context) {
	d := w.nbDlg
	if d == nil || w.confirm != nil {
		return
	}
	if d.kind == "rename" {
		w.renameNotebookView(c, d)
		return
	}
	open := true
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(scrim)
		panel.Width(sizeReading+sizeList).Padding(space4).Gap(space3).Background(raised).Border(1, line).Radius(radiusMd).Label("dialog " + d.title)
		text(c, d.title, ink, titleSize, weightSemibold)
		ui.Text(c, d.desc).TextColor(inkMuted).FontSize(uiFontSize).MaxLines(0)
		ui.Row(c).FillWidth().Gap(space3).Height(sizeRow * 8).MinHeight(0).Children(func() {
			ui.Scroll(c).Width(sizeList).Shrink(0).FillHeight().Label("Versions").Children(func() {
				ui.Column(c).FillWidth().Gap(space1).Children(func() { w.versionRows(c, d) })
			})
			ui.Scroll(c).Grow(1).MinWidth(0).FillHeight().Label("Changes").Children(func() {
				ui.Column(c).FillWidth().Gap(space1).Children(func() { w.versionDiff(c, d) })
			})
		})
		purge, restore := "Delete this version", "Restore this version"
		if d.kind == "trash" {
			purge, restore = "Delete forever", "Restore notebook"
		}
		off := d.selected == "" || d.busy
		ui.Row(c).FillWidth().Gap(space2).Justify(ui.End).Children(func() {
			if dialogBtn(c, purge, btnGhost, &d.purgeH, off) {
				w.act(func() { w.nbAskPurge(d) })
			}
			if dialogBtn(c, "Close", btnGhost, &d.closeH, false) {
				open = false
			}
			if dialogBtn(c, restore, btnPrimary, &d.restoreH, off) {
				w.act(func() { w.nbRestore(d) })
			}
		})
	})
	if !open {
		w.act(func() {
			if w.nbDlg == d {
				w.nbDlg = nil
			}
		})
	}
}

// versionRows is the list of versions, or what stands in for it.
func (w *workspace) versionRows(c *ui.Context, d *nbDialog) {
	if d.empty != "" {
		help(c, d.empty)
		return
	}
	for _, v := range d.versions {
		l := versionLabel(v, d.now)
		title, when := l.What, l.When
		who := l.Who
		if d.kind == "trash" {
			title, who, when = orElse(v.Name, "Notebook"), "Deleted", "Deleted "+whenText(v.KeptAt, d.now)
		}
		sel := v.ID == d.selected
		r := ui.Column(c).FillWidth().Padding(space1_5, space2).Gap(space0_5).Radius(radiusSm).Cursor(ui.CursorPointer).Key(v.ID).Label("version " + title + " " + when).Selected(sel)
		if sel || r.Hovered() {
			r.Background(hoverGround)
		}
		r.Children(func() {
			ui.Row(c).Gap(space2).AlignItems(ui.Center).Children(func() {
				if v.By == byAgent {
					outlineBadge(c, who)
				} else {
					tag(c, who, hoverGround, inkMuted, false)
				}
				body(c, title, ink).MinWidth(0)
			})
			caption(c, when, inkFaint)
			if l.Summary != "" && d.kind != "trash" {
				caption(c, l.Summary, inkMuted)
			}
		})
		if r.Clicked() {
			vid := v.ID
			w.act(func() { w.nbSelect(d, vid) })
		}
	}
}

// versionDiff is the right side: the lines that changed from the selected version to now, or a deleted notebook's text.
func (w *workspace) versionDiff(c *ui.Context, d *nbDialog) {
	if d.selected == "" {
		return
	}
	if msg := d.textErr[d.selected]; msg != "" {
		help(c, msg)
		return
	}
	txt, ok := d.texts[d.selected]
	if !ok {
		return
	}
	if d.kind == "trash" {
		ui.Text(c, txt).TextColor(ink).FontSize(captionSize).Font(monoStack).MaxLines(0).Label("deleted notebook text")
		return
	}
	cur, _ := w.notebook(d.id)
	diff := diffLines(txt, cur.Markdown(), diffMaxCells)
	ui.Text(c, "From this version to now: "+diffSummary(diff)).TextColor(inkMuted).FontSize(captionSize).Label("diff summary")
	for _, r := range diffView(diff, 2) {
		if r.Op == "gap" {
			word := "lines"
			if r.Count == 1 {
				word = "line"
			}
			caption(c, strconv.Itoa(r.Count)+" unchanged "+word, inkFaint)
			continue
		}
		mark, col, bg := " ", ink, ui.Transparent
		switch r.Op {
		case "add":
			mark, col, bg = "+", tide, tideSoft
		case "del":
			mark, col, bg = "−", flare, flareSoft
		}
		line := r.Text
		if line == "" {
			line = " "
		}
		ui.Row(c).FillWidth().Gap(space2).Padding(0, space1_5).Background(bg).Label("diff " + r.Op).Children(func() {
			text(c, mark, col, captionSize, weightRegular).Font(monoStack).Shrink(0)
			ui.Text(c, line).TextColor(col).FontSize(captionSize).Font(monoStack).MaxLines(0).Grow(1).Shrink(1).MinWidth(0)
		})
	}
}

// renameNotebookView is Rename notebook: the name and Rename.
func (w *workspace) renameNotebookView(c *ui.Context, d *nbDialog) {
	open := true
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(scrim)
		panel.Width(dialogWidth).Padding(space4).Gap(space3).Background(raised).Border(1, line).Radius(radiusMd).Label("dialog Rename notebook")
		panel.ObserveInput(func(ev ui.InputEvent) {
			if ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyEnter && ev.Mods == 0 && d.nameH.FocusWithin(c) {
				w.act(func() { w.renameNotebook(d.id, d.name) })
			}
		})
		text(c, "Rename notebook", ink, titleSize, weightSemibold)
		labeled(c, "Name", &d.nameH, func() { inputBox(c, "Notebook rename name", &d.name, "Notebook", &d.nameH, false, false, 0, nil) })
		ui.Row(c).FillWidth().Gap(space2).Justify(ui.End).Children(func() {
			if textBtn(c, "Cancel", btnGhost) {
				open = false
			}
			if textBtn(c, "Rename", btnPrimary) {
				w.act(func() { w.renameNotebook(d.id, d.name) })
			}
		})
	})
	if d.focus && w.nbDlg == d {
		d.nameH.Focus(c)
		d.focus = false
	}
	if !open {
		w.act(func() {
			if w.nbDlg == d {
				w.nbDlg = nil
			}
		})
	}
}
