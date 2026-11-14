package native

import (
	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The Notebooks page, the sidebar's Notebooks section and the row menu, in the Logs page's layout (logsview.go). The
// document itself is the notebook pane (notebookpane.go).

const notebooksSubtitle = "Markdown runbooks. Fenced sh and bash blocks fill a terminal's input; nothing is typed until you confirm."

// notebooksPage is the page's state: its filter.
type notebooksPage struct {
	filter  string
	filterH ui.Handle
}

func (p *notebooksPage) load(w *workspace)                { w.reloadNotebooks() }
func (p *notebooksPage) view(w *workspace, c *ui.Context) { w.notebooksView(c, p) }
func (p *notebooksPage) newItem(w *workspace)             { w.newNotebook() }

// notebookMenu is notebookCardMenu: Open, Rename, Pin or Unpin, Make private or Let the agent read it, History, Ask the
// agent about this notebook (not for a private one), Delete. Nothing in the menu types.
func (w *workspace) notebookMenu(nb keelclient.Notebook) func(m *ui.Menu) {
	return func(m *ui.Menu) {
		id := nb.ID
		if menuItem(m, "Open").Chosen() {
			w.act(func() { w.openNotebook(id) })
		}
		if menuItem(m, "Rename").Chosen() {
			w.act(func() { w.openRenameNotebook(id) })
		}
		pin := "Pin"
		if nb.Pinned {
			pin = "Unpin"
		}
		if menuItem(m, pin).Chosen() {
			w.act(func() { w.toggleNotebookPin(id) })
		}
		priv := "Make private"
		if nb.Private {
			priv = "Let the agent read it"
		}
		if menuItem(m, priv).Chosen() {
			w.act(func() { w.toggleNotebookPrivate(id) })
		}
		if menuItem(m, "History").Chosen() {
			w.act(func() { w.openNotebookHistory(id) })
		}
		if !nb.Private {
			// §6 rule 12: a private notebook does not exist for the agent, so it is never offered to it.
			m.Separator()
			if menuItem(m, "Ask the agent about this notebook").Chosen() {
				w.act(func() { w.askAboutNotebook(id) })
			}
		}
		m.Separator()
		if menuItem(m, "Delete").Chosen() {
			w.act(func() { w.askDeleteNotebook(id) })
		}
	}
}

// ---- the page ----

func (w *workspace) notebooksView(c *ui.Context, p *notebooksPage) {
	ui.Column(c).Grow(1).FillWidth().FillHeight().Background(w.ground(surface)).Children(func() {
		pageHead(c, "Notebooks", notebooksSubtitle, func() {
			ui.Row(c).Gap(space2).Shrink(0).Children(func() {
				if textBtnIcon(c, "restore-bin", "Recently deleted", btnGhost) {
					w.act(w.openNotebookTrash)
				}
				if textBtnIcon(c, "add-01", "New notebook", btnPrimary) {
					w.act(w.newNotebook)
				}
			})
		})
		ui.Row(c).FillWidth().Padding(0, space8, space3).Gap(space2).Wrap().Shrink(0).Children(func() {
			searchField(c, &p.filter, "Filter notebooks", &p.filterH).Width(sizeSidebar)
		})
		shown := w.shownNotebooks(p.filter)
		switch {
		case len(w.notebooks) == 0 && w.live:
			if pr, _ := emptyPage(c, emptyNone("notebook-pen", "No notebooks yet", notebooksNone, "New notebook")); pr {
				w.act(w.newNotebook)
			}
		case len(shown) == 0 && w.live:
			if pr, _ := emptyPage(c, emptyNoMatch("search-01", p.filter)); pr {
				w.act(func() { p.filter = "" })
			}
		default:
			ui.Scroll(c).FillWidth().Grow(1).Shrink(1).Label("notebook list").Children(func() {
				for _, nb := range shown {
					w.notebookRow(c, nb)
				}
			})
		}
	})
}

// notebookRow is a row of NotebookList: the tile, the name and the first line, the badges and the menu. A click opens it.
func (w *workspace) notebookRow(c *ui.Context, nb keelclient.Notebook) {
	r := hairline(ui.Row(c).FillWidth().MinHeight(sizeControlLg).Padding(space2, space6, space2, space8).Gap(space3).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Key(nb.ID).Label("notebook "+notebookName(nb)), 0, 0, 1, 0)
	if r.Hovered() {
		r.Background(hoverGround)
	}
	r.PopoverMenu(w.notebookMenu(nb))
	r.Children(func() {
		ui.Row(c).Size(sizeControl, sizeControl).Radius(radiusSm).Justify(ui.Center).Shrink(0).Background(inkMuted.Alpha(tileTintAlpha)).Children(func() {
			iconSized(c, "notebook-pen", inkMuted, iconSize-space0_5)
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space0_5).Children(func() {
			bodyStrong(c, notebookName(nb), ink).MinWidth(0)
			text(c, notebookPreview(nb), inkMuted, itemSub, weightRegular).Font(monoStack).MinWidth(0)
		})
		w.notebookBadges(c, nb)
	})
	if r.Clicked() {
		id := nb.ID
		w.act(func() { w.openNotebook(id) })
	}
}

// notebookBadges are the row's badges: Private, Edited by agent, From agent, Pinned.
func (w *workspace) notebookBadges(c *ui.Context, nb keelclient.Notebook) {
	ui.Row(c).Gap(space1_5).AlignItems(ui.Center).Shrink(0).Children(func() {
		if nb.Private {
			ui.Row(c).Height(space4).Padding(0, space1_5).Gap(space1).Radius(radiusXs).Justify(ui.Center).Shrink(0).Border(1, lineStrong).
				Tooltip("Private: the agent cannot read or change it").Label("badge Private").Children(func() {
				iconSized(c, "square-lock-02", inkMuted, iconSizeSm)
				text(c, "Private", inkMuted, kbdSize, weightSemibold)
			})
		}
		if nb.AgentUnreviewed {
			tag(c, "Edited by agent", signalSoft, signal)
		}
		if nb.Source == byAgent {
			outlineBadge(c, "From agent")
		}
		if nb.Pinned {
			neutralBadge(c, "Pinned")
		}
	})
}

// ---- the sidebar's Notebooks section ----

// sidebarNotebooks is the Notebooks half of the Snippets segment: a row per notebook (the tile, the name, the pin and the
// menu), the empty and no-match words, New notebook and All notebooks. The segment's one filter reads the rows.
func (w *workspace) sidebarNotebooks(c *ui.Context) {
	ui.Column(c).FillWidth().Gap(space0_5).Children(func() {
		ui.Row(c).FillWidth().Padding(space2, space2, 0).Children(func() { menuLabel(c, "Notebooks") })
		shown := w.shownNotebooks(w.snipQuery)
		for _, nb := range shown {
			w.sideNotebookRow(c, nb)
		}
		switch {
		case len(w.notebooks) == 0 && w.live:
			emptySide(c, emptyNone("notebook-pen", "No notebooks yet", notebooksNone, "")) // New notebook is the button below
		case len(shown) == 0 && w.live:
			if emptySide(c, emptyNoMatch("search-01", w.snipQuery)) {
				w.act(func() { w.snipQuery = "" })
			}
		}
		ui.Row(c).FillWidth().Gap(space1).Wrap().Children(func() {
			if textBtn(c, "New notebook", btnGhost) {
				w.act(w.newNotebook)
			}
			if textBtn(c, "All notebooks", btnGhost) {
				w.act(func() { w.openPage("notebooks") })
			}
		})
	})
}

func (w *workspace) sideNotebookRow(c *ui.Context, nb keelclient.Notebook) {
	name := notebookName(nb)
	r := ui.Row(c).FillWidth().Height(sizeRow).Padding(0, space2).Gap(space2).Radius(radiusSm).Key(nb.ID).
		Label("sidebar notebook " + name).Tooltip(name).Cursor(ui.CursorPointer).Focusable()
	hover := r.Hovered()
	if hover {
		r.Background(hoverGround)
	}
	r.PopoverMenu(w.notebookMenu(nb))
	pinClicked := false
	r.Children(func() {
		ui.Row(c).Size(sizeTileSm, sizeTileSm).Radius(radiusXs).Justify(ui.Center).Shrink(0).Background(inkMuted.Alpha(tileTintAlpha)).Children(func() {
			iconSized(c, "notebook-pen", inkMuted, iconSizeSm)
		})
		body(c, name, ink).Grow(1).Shrink(1).MinWidth(0)
		if hover || r.FocusWithin() || nb.Pinned {
			pin := "Pin"
			if nb.Pinned {
				pin = "Unpin"
			}
			pinClicked = iconBtn(c, "pin", pin+" "+name, sizeControlSm, nb.Pinned)
		}
	})
	switch {
	case pinClicked:
		id := nb.ID
		w.act(func() { w.toggleNotebookPin(id) })
	case r.Clicked():
		id := nb.ID
		w.act(func() { w.openNotebook(id) })
	}
}
