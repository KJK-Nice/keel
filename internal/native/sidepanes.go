package native

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
	"keel/internal/native/layout"
)

// The sidebar's Panes section (docs/ui-redesign.md §5.1 "Panes in the sidebar"; term.js panesTab, applySidebarSeg,
// revealPanes, pickPane, paneRowInfo, paintPanesSection). The segment exists only while the active tab has two or more
// panes; its count follows the label; when that ends (a pane closed, another tab selected) the sidebar falls back to
// Hosts. A map of the tab's split tree (each cell sized like its pane, the focused one with an accent line on top) and
// "This tab" with one row per pane. A row or a cell focuses its pane and leaves focus view first. Clicking into a
// pane of a split tab shows this section without moving the keyboard out of the terminal. The filter reads the rows,
// never the map.
//
// A notebook pane is a row and a map cell like a terminal's, named by its title (term.js paneRowInfo for a notebook). The row
// menu is the pane menu without Focus view (the bar has that) and without Share (a notebook has no session).

const (
	paneMapHeight = sizeRow + sizeControlLg // .k-panemap: calc(var(--size-row) + var(--size-control-lg))
	paneRowHeight = sizeControlLg + space2  // button.k-pane-row
)

// panesTab is the tab the Panes section lists: the active session tab, only while it has two or more panes.
func (w *workspace) panesTab() *tab {
	if t := w.tab(); t != nil && !t.isNew() && !t.isPage() && len(t.panes) > 1 {
		return t
	}
	return nil
}

// sidePanes is the Panes section showing.
func (w *workspace) sidePanes() bool { return w.sideSeg == "panes" && w.panesTab() != nil }

// fallBackToHosts is applySidebarSeg: Panes without a split tab becomes Hosts, and stays Hosts.
func (w *workspace) fallBackToHosts() {
	if w.sideSeg == "panes" && w.panesTab() == nil {
		w.sideSeg = "hosts"
	}
}

// revealPanes shows the Panes section when the user focuses a pane of a split tab. It changes only the section;
// the keyboard stays where it is.
func (w *workspace) revealPanes(t *tab) {
	if t != nil && t == w.panesTab() && w.sideSeg != "panes" {
		w.sideSeg = "panes"
	}
}

// pickPane is a row or a map cell: the pane gets the keyboard, and a tab in focus view leaves it first so the pane
// shows.
func (w *workspace) pickPane(t *tab, id string) {
	if t.panes[id] == nil {
		return
	}
	if t.zoomed != "" && t.zoomed != id {
		t.zoomed = ""
	}
	t.focused = id
	if t == w.tab() {
		w.wantFocus = id
	}
}

// paneRow is paneRowInfo: what a row shows from what the pane knows.
type paneRow struct {
	id, name, sub string
	host          keelclient.Host
	driving       bool // shared with you driving
	agent         bool // the agent can read it
	notebook      bool // a notebook pane: no host, no session
}

// statusWord is term.js dotFor's label for a session's state.
func statusWord(status string) string {
	switch status {
	case "ready":
		return "Connected"
	case "connecting":
		return "Connecting"
	case "error", "exited":
		return "Disconnected"
	}
	return "Idle"
}

// paneRows lists the tab's panes in reading order.
func (w *workspace) paneRows(t *tab) (out []paneRow) {
	for _, id := range t.root.Leaves() {
		p := t.panes[id]
		if p == nil {
			continue
		}
		if p.nb != nil { // a notebook: its title, and "Notebook" where a session has its state
			out = append(out, paneRow{id: id, name: orElse(strings.TrimSpace(p.nb.title), "Notebook"), sub: "Notebook", notebook: true})
			continue
		}
		st := p.conn.Snapshot()
		sub := st.Subtitle
		if sub == "" {
			sub = statusWord(st.Status)
		}
		shared := len(st.Attached) > 1
		out = append(out, paneRow{
			id: id, name: p.host.Name, sub: sub, host: p.host,
			driving: shared && st.Role == "driver",
			agent:   p.sessionID() != "" && p.host.ID != "" && p.host.AgentMayRead(),
		})
	}
	return out
}

// shownPaneRows is the list under the filter (name and sub line). The map is never filtered.
func shownPaneRows(rows []paneRow, q string) (out []paneRow) {
	for _, r := range rows {
		if sideMatches(r.name+" "+r.sub, q) {
			out = append(out, r)
		}
	}
	return out
}

// ---- drawing ----

// sidebarPanes is the section: the map, then "This tab" and the rows.
func (w *workspace) sidebarPanes(c *ui.Context, t *tab) {
	rows := w.paneRows(t)
	focused := t.focused
	if t.panes[focused] == nil && len(rows) > 0 {
		focused = rows[0].id
	}
	ui.Column(c).FillWidth().Gap(space1).Children(func() {
		// .k-panemap: a 1px line-coloured frame whose gutters are the lines between cells.
		ui.Box(c).FillWidth().Height(paneMapHeight).Padding(1).Radius(radiusSm).Background(line).Label("Layout of this tab").Children(func() {
			w.paneMapNode(c, t, t.root, rows, focused, layout.Row, true, 1)
		})
		ui.Row(c).FillWidth().Padding(0, space2).Gap(space1_5).Height(sizeControl).Children(func() {
			iconSized(c, "panes-layout", inkMuted, iconSize-space0_5)
			menuLabel(c, "This tab")
			ui.Spacer(c)
			caption(c, fmt.Sprint(len(rows)), inkMuted)
		})
		shown := shownPaneRows(rows, w.paneQuery)
		for _, r := range shown {
			w.paneRowView(c, t, r, r.id == focused)
		}
		if len(shown) == 0 {
			if emptySide(c, emptyNoMatch("panes-layout", w.paneQuery)) {
				w.act(func() { w.paneQuery = "" })
			}
		}
	})
}

// paneMapNode draws a subtree of the split as nested rows and columns sharing space by the split's ratio: each cell is
// sized like its pane. Cell numbers are reading order.
func (w *workspace) paneMapNode(c *ui.Context, t *tab, n *layout.Node, rows []paneRow, focused string, parent layout.Dir, root bool, grow float32) {
	fit := func(e ui.Element) ui.Element {
		e = e.Grow(grow).Shrink(1).Basis(0)
		switch {
		case root:
			return e.Fill()
		case parent == layout.Row:
			return e.FillHeight()
		}
		return e.FillWidth()
	}
	if n.A == nil {
		for i, r := range rows {
			if r.id == n.Pane {
				w.paneMapCell(c, t, r, i+1, r.id == focused, fit)
			}
		}
		return
	}
	cont := ui.Row(c)
	if n.Dir == layout.Col {
		cont = ui.Column(c)
	}
	fit(cont).Children(func() {
		w.paneMapNode(c, t, n.A, rows, focused, n.Dir, false, n.Ratio)
		w.paneMapNode(c, t, n.B, rows, focused, n.Dir, false, 1-n.Ratio)
	})
}

// paneMapCell is button.k-panemap-cell in its slot: the number and the name; the focused cell is on surface with a
// 2px accent line on top.
func (w *workspace) paneMapCell(c *ui.Context, t *tab, r paneRow, n int, current bool, fit func(ui.Element) ui.Element) {
	fit(ui.Box(c)).Padding(1).Children(func() {
		cell := ui.Column(c).Fill().Padding(space1, space1_5).Radius(radiusXs).Background(controlFill(stage)).Clip().Cursor(ui.CursorPointer).
			Label(fmt.Sprintf("Focus pane %d, %s", n, r.name)).Tooltip(r.name).Selected(current)
		col := inkFaint
		switch {
		case current:
			cell.Background(controlFill(surface))
			col = ink
		case cell.Hovered():
			cell.Background(hoverGround)
			col = inkMuted
		}
		cell.Children(func() {
			if current {
				ui.Box(c).FillWidth().Height(2).Background(accentFill).Shrink(0).Label("focused pane " + r.id)
			}
			ui.Row(c).Gap(space1).Children(func() {
				text(c, fmt.Sprint(n), inkFaint, kbdSize, weightRegular).Font(monoStack).Shrink(0)
				text(c, r.name, col, kbdSize, weightRegular).Font(monoStack).MinWidth(0)
			})
		})
		if cell.Clicked() {
			id := r.id
			w.act(func() { w.pickPane(t, id) })
		}
	})
}

// paneRowView is button.k-pane-row: the host's tile, the name and sub line, and the meta icons (steering when you
// drive a shared pane, the agent's mark when it can read the pane).
func (w *workspace) paneRowView(c *ui.Context, t *tab, r paneRow, current bool) {
	row := ui.Row(c).FillWidth().Height(paneRowHeight).Padding(0, space2).Gap(space2).Radius(radiusSm).Key(r.id).
		Label("pane row " + r.name).Tooltip(r.name).Cursor(ui.CursorPointer).Focusable().Selected(current)
	nameCol, subCol := ink, inkFaint
	accentOn := false
	if current {
		if nameCol, accentOn = w.chosenRow(c, row); accentOn {
			subCol = nameCol
		}
	} else if row.Hovered() {
		row.Background(hoverGround)
	}
	row.PopoverMenu(w.paneMenuFor(t, t.panes[r.id], true))
	row.Children(func() {
		glyph := "server"
		switch {
		case r.notebook:
			glyph = "notebook-pen"
		case r.host.IsLocal() || r.host.ID == "":
			glyph = "computer-terminal-01"
		}
		rowTile(c, glyph, hostTint(r.host.Color), accentOn, nameCol)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space0_5).Children(func() {
			if current {
				bodyStrong(c, r.name, nameCol).MinWidth(0)
			} else {
				body(c, r.name, inkMuted).MinWidth(0)
			}
			text(c, r.sub, subCol, itemSub, weightRegular).Font(monoStack).MinWidth(0)
		})
		if r.driving || r.agent {
			ui.Row(c).Gap(space1_5).Shrink(0).Children(func() {
				if r.driving {
					ui.Row(c).Label("You are driving").Tooltip("You are driving").Children(func() { iconSized(c, "steering", inkFaint, iconSizeSm+space0_5) })
				}
				if r.agent {
					ui.Row(c).Label("The agent can read this pane").Tooltip("The agent can read this pane").Children(func() { iconSized(c, "ai-magic", haze, iconSizeSm+space0_5) })
				}
			})
		}
	})
	if row.Clicked() {
		id := r.id
		w.act(func() { w.pickPane(t, id) })
	}
}
