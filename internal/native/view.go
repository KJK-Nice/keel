package native

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
	"keel/internal/native/layout"
)

// view draws the whole screen (docs/ui-redesign.md §5.1). It reads and changes the workspace
// on the UI thread only; changes the buttons ask for are queued with act and run at the end.
func (w *workspace) view(c *ui.Context) {
	windowTheme(c)
	w.shortcuts(c)
	if t := w.tab(); t != nil {
		for id, p := range t.panes { // the ring and the dim follow keyboard focus
			if p.handle.FocusWithin(c) {
				t.focused = id
			}
		}
	}
	ui.Box(c).Fill().Children(func() {
		w.titlebar(c)
		ui.Row(c).FillWidth().Grow(1).Shrink(1).Children(func() {
			if w.sidebar {
				w.sidebarView(c)
			}
			w.stage(c)
		})
		w.statusbar(c)
	})
	if t := w.tab(); t != nil && w.wantFocus != "" {
		if p := t.panes[w.wantFocus]; p != nil {
			p.handle.Focus(c)
			w.wantFocus = ""
		}
	}
	if len(w.later) > 0 {
		run := w.later
		w.later = nil
		for _, fn := range run {
			fn()
		}
		c.Invalidate()
	}
}

// shortcuts declares the keys of docs/shortcuts.md that N1 has. A window shortcut is taken
// before the terminal sees the key.
func (w *workspace) shortcuts(c *ui.Context) {
	digits := [...]ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8}
	for i, k := range digits {
		c.OnWindowShortcut(ui.Cmd, k, func() { w.act(func() { w.selectTab(i) }) })
	}
	c.OnWindowShortcut(ui.Cmd, ui.Key9, func() { w.act(func() { w.selectTab(len(w.tabs) - 1) }) })
	c.OnWindowShortcut(ui.Cmd, ui.KeyW, func() { w.act(w.closeFocused) })
	c.OnWindowShortcut(ui.Cmd, ui.KeyB, func() { w.act(func() { w.sidebar = !w.sidebar }) })
	c.OnWindowShortcut(ui.Cmd|ui.Shift, ui.KeyD, func() { w.act(func() { w.splitFocused(layout.Row) }) })
	c.OnWindowShortcut(ui.Cmd|ui.Shift, ui.KeyE, func() { w.act(func() { w.splitFocused(layout.Col) }) })
}

func (w *workspace) titlebar(c *ui.Context) {
	// TitleBar().Left is 0 when no lights are drawn (full screen, or none on this platform): the row
	// then starts at the normal padding. The view redraws when it changes.
	inset := float32(space2)
	if left := c.TitleBar().Left; left > 0 {
		inset = left + space3
	}
	hairline(ui.Row(c).FillWidth().Height(sizeTitlebar).Shrink(0).Background(chrome), 0, 0, 1, 0).
		Padding(0, space2, 0, inset).Gap(space1_5).DragWindow().Children(func() {
		if iconBtn(c, "panel-left", "Toggle sidebar", sizeControlSm, w.sidebar) {
			w.act(func() { w.sidebar = !w.sidebar })
		}
		for i, t := range w.tabs {
			w.tabPill(c, i, t)
		}
		if iconBtn(c, "add-01", "New tab", sizeControlSm, false) {
			w.act(w.newTab)
		}
	})
}

func (w *workspace) tabPill(c *ui.Context, i int, t *tab) {
	selected := i == w.active
	r := ui.Row(c).Height(sizeTab).MaxWidth(tabMaxWidth).Padding(0, space2).Gap(space1_5).Radius(radiusSm).
		Label("tab " + t.name()).Cursor(ui.CursorPointer).Key(t)
	switch {
	case selected:
		r.Background(raised)
	case r.Hovered():
		r.Background(hoverGround)
	}
	r.Children(func() {
		status := ""
		if p := t.panes[t.focused]; p != nil {
			status = p.status()
		}
		dot(c, statusColor(status))
		col := inkMuted
		if selected {
			col = ink
		}
		bodyStrong(c, t.name(), col)
		if n := len(t.panes); n > 1 {
			ui.Spacer(c)
			caption(c, fmt.Sprint(n), inkMuted)
		}
	})
	if r.Clicked() {
		w.act(func() { w.selectTab(i) })
	}
}

func (w *workspace) sidebarView(c *ui.Context) {
	hairline(ui.Box(c).Width(sizeSidebar).Shrink(0).FillHeight().Background(chrome), 0, 1, 0, 0).Children(func() {
		ui.Column(c).Grow(1).Padding(space2).Gap(space2).Children(func() {
			ui.Row(c).FillWidth().Height(sizeControl).Shrink(0).Padding(0, space2).Gap(space1_5).Radius(radiusSm).
				Background(stage).Border(1, lineStrong).Children(func() {
				icon(c, "search-01", inkFaint)
				ui.TextInputBase(c, &w.filter).Label("Filter hosts").Placeholder("Filter hosts").
					PlaceholderColor(inkFaint).TextColor(ink).FontSize(uiFontSize).Grow(1)
			})
			ui.Column(c).FillWidth().Grow(1).Gap(space2).Children(func() {
				for _, g := range w.groups() {
					w.group(c, g)
				}
			})
		})
	})
}

func (w *workspace) group(c *ui.Context, g hostGroup) {
	open := !w.collapsed[g.name]
	ui.Column(c).FillWidth().Key(g.name).Children(func() {
		h := ui.Row(c).FillWidth().Height(sizeControl).Padding(0, space2).Gap(space1_5).Label("group " + g.name).Cursor(ui.CursorPointer)
		h.Children(func() {
			chev := "chevron-down"
			if !open {
				chev = "chevron-right"
			}
			icon(c, chev, inkMuted)
			label(c, g.name, inkMuted)
			ui.Spacer(c)
			caption(c, fmt.Sprint(len(g.hosts)), inkMuted)
		})
		if h.Clicked() {
			w.act(func() { w.collapsed[g.name] = open })
		}
		if !open {
			return
		}
		for _, host := range g.hosts {
			w.hostRow(c, host)
		}
	})
}

func (w *workspace) hostRow(c *ui.Context, host keelclient.Host) {
	current := false
	if p := w.focusedPane(); p != nil {
		current = p.host.ID == host.ID
	}
	r := ui.Row(c).FillWidth().Height(sizeControl).Padding(0, space2).Gap(space2).Radius(radiusSm).
		Label("host " + host.Name).Cursor(ui.CursorPointer).Key(host.ID)
	if current || r.Hovered() {
		r.Background(hoverGround)
	}
	r.Children(func() {
		ui.Row(c).Size(sizeTileSm, sizeTileSm).Radius(radiusXs).Justify(ui.Center).Shrink(0).
			Background(hostTint(host.Color).Alpha(tileTintAlpha)).Children(func() {
			iconSized(c, "server", hostTint(host.Color), iconSizeSm)
		})
		body(c, host.Name, ink)
		if current {
			ui.Spacer(c)
			dot(c, tide)
			caption(c, fmt.Sprint(w.panesOn(host.ID)), inkMuted)
		}
	})
	if r.Clicked() {
		w.act(func() { w.openHost(host) })
	}
}

// stage is the area right of the sidebar: the active tab's panes, or a message.
func (w *workspace) stage(c *ui.Context) {
	t := w.tab()
	switch {
	case w.blocked != "":
		w.centered(c, w.blocked)
	case t == nil:
		w.centered(c, "No sessions. Pick a host in the sidebar to open one.")
	default:
		multi := len(t.panes) > 1
		area := ui.Box(c).Grow(1).FillHeight().Background(stage)
		if multi {
			// The padding and the gaps between tiles are the chrome ground (the rings paint over it).
			area.Padding(space1_5).Background(chrome)
		}
		area.Children(func() { w.node(c, t, t.root, multi, layout.Row, true, 1) })
	}
}

func (w *workspace) centered(c *ui.Context, msg string) {
	ui.Column(c).Grow(1).FillHeight().Center().Background(stage).Children(func() {
		body(c, msg, inkMuted)
	})
}

// node draws a subtree: a split is two children with a gap between them that drags its ratio.
// parent is the direction of the split this node is a child of (what it fills across).
func (w *workspace) node(c *ui.Context, t *tab, n *layout.Node, multi bool, parent layout.Dir, root bool, grow float32) {
	fit := func(e ui.Element) ui.Element {
		e = e.Grow(grow).Shrink(1)
		switch {
		case root:
			return e.Fill()
		case parent == layout.Row:
			return e.FillHeight()
		}
		return e.FillWidth()
	}
	if n.A == nil {
		w.tile(c, t, t.panes[n.Pane], multi, fit)
		return
	}
	cont := ui.Row(c)
	if n.Dir == layout.Col {
		cont = ui.Column(c)
	}
	fit(cont).Bind(w.splitHandle(n)).Children(func() {
		w.node(c, t, n.A, multi, n.Dir, false, n.Ratio)
		w.gap(c, n)
		w.node(c, t, n.B, multi, n.Dir, false, 1-n.Ratio)
	})
}

// tile is one pane: a pane bar on surface above the body on stage. In a split tab it is a rounded tile
// and the ring and the dim follow keyboard focus (the body's handle), so a click inside a terminal moves them.
func (w *workspace) tile(c *ui.Context, t *tab, p *pane, multi bool, fit func(ui.Element) ui.Element) {
	focused := t.focused == p.id
	tl := fit(ui.Column(c)).Label("pane " + p.id).Key(p.id)
	if multi {
		tl.Radius(radiusMd).Clip()
		if focused {
			tl.Shadow(0, 0, 0, focusRingWidth, focus.Alpha(focusRingAlpha))
		}
	}
	tl.Children(func() {
		barGround, hostCol := surface, inkMuted
		if focused {
			hostCol = ink
		}
		if !multi && !focused {
			barGround = chrome
		}
		bar := ui.Row(c).FillWidth().Height(sizePanebar).Shrink(0).Padding(0, space1, 0, space3).Gap(space2).Background(barGround)
		if bar.Clicked() {
			w.act(func() { p.handle.Focus(c); t.focused = p.id })
		}
		bar.Children(func() {
			status, _, sub := p.conn.State()
			dot(c, statusColor(status))
			// The tools never shrink; user gives way first, then the host name (Shrink is the weight).
			bodyStrong(c, p.host.Name, hostCol).Shrink(2).MinWidth(0)
			if sub == "" {
				sub = p.host.Address
				if p.host.Username != "" {
					sub = p.host.Username + "@" + sub
				}
			}
			mono(c, sub, inkMuted).Shrink(8).MinWidth(0)
			ui.Spacer(c)
			ui.Row(c).Gap(space0_5).Shrink(0).Children(func() {
				if iconBtn(c, "split-h", "Split right", sizeControlSm, false) {
					w.act(func() { w.split(t, p.id, layout.Row) })
				}
				if iconBtn(c, "split-v", "Split down", sizeControlSm, false) {
					w.act(func() { w.split(t, p.id, layout.Col) })
				}
				if iconBtn(c, "cancel-01", "Close pane", sizeControlSm, false) {
					w.act(func() { w.closePane(t, p.id) })
				}
			})
		})
		pane := ui.Box(c).Grow(1).FillWidth().Background(stage)
		if multi && !focused {
			pane.Opacity(dimAlpha)
		}
		pane.Children(func() { p.view(c).Bind(&p.handle) })
	})
}

// gap is the 6px chrome gap between two tiles and the split's drag handle: no hairline.
func (w *workspace) gap(c *ui.Context, n *layout.Node) {
	row := n.Dir == layout.Row
	g := ui.Box(c).Shrink(0).Label("split handle")
	min := float32(minTileHeight)
	if row {
		g.Width(space1_5).FillHeight().Cursor(ui.CursorResizeColumn)
		min = minTileWidth
	} else {
		g.Height(space1_5).FillWidth().Cursor(ui.CursorResizeRow)
	}
	dx, dy, ok := g.Dragged()
	if !ok {
		return
	}
	b := w.splitHandle(n).Bounds(c)
	d, avail := dy, b.H-space1_5
	if row {
		d, avail = dx, b.W-space1_5
	}
	if avail > 0 {
		n.Ratio = layout.ClampRatio(n.Ratio+d/avail, avail, min)
	}
}

func (w *workspace) statusbar(c *ui.Context) {
	hairline(ui.Row(c).FillWidth().Height(statusbarSize).Shrink(0).Padding(0, space3).Gap(space4).Background(chrome), 1, 0, 0, 0).Children(func() {
		ui.Row(c).Gap(space1_5).Children(func() {
			dot(c, tide)
			caption(c, fmt.Sprintf("keel serve · %d sessions", w.paneCount()), inkMuted)
		})
		if w.err != "" {
			caption(c, w.err, flare)
		}
		ui.Spacer(c)
		if p := w.focusedPane(); p != nil {
			cols, rows := p.size()
			caption(c, fmt.Sprintf("%s · %d×%d · UTF-8", p.host.Name, cols, rows), inkMuted)
		}
	})
}
