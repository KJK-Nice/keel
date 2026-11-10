package native

import (
	"fmt"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
	"keel/internal/native/layout"
)

// view draws the whole screen (docs/ui-redesign.md §5.1). It reads and changes the workspace
// on the UI thread only; changes the buttons ask for are queued with act and run at the end.
func (w *workspace) view(c *ui.Context) {
	windowTheme(c)
	if w.alpha < 1 {
		c.Root().Background(ui.Transparent) // the grounds below are painted at the window's alpha
	}
	if w.gate != nil { // Unlock replaces the window; no shortcut acts, sessions keep running
		ui.Column(c).Fill().Children(func() { w.unlockView(c) })
		w.runLater(c)
		return
	}
	if w.first != nil { // First run replaces the window as Unlock does; the palette's import closes with Esc
		if w.first.flow == flowImport {
			c.OnWindowShortcut(0, ui.KeyEscape, func() { w.act(func() { w.afterHosts(w.first) }) })
		}
		ui.Column(c).Fill().Children(func() { w.firstRunView(c) })
		w.runLater(c)
		return
	}
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
	w.hostKeyDialog(c)
	w.quickDialogView(c)
	w.hostDialogView(c)
	w.confirmView(c)
	w.paletteView(c)
	if t := w.tab(); t != nil && w.wantFocus != "" && !w.modalOpen() {
		if p := t.panes[w.wantFocus]; p != nil && (t.zoomed == "" || t.zoomed == p.id) {
			p.handle.Focus(c)
			w.wantFocus = ""
		}
	}
	if w.findPane != nil {
		w.syncFind()
		if w.findFocus {
			w.findHandle.Focus(c)
			w.findFocus = false
		}
	}
	if w.renaming != nil { // the field has the keyboard until a click elsewhere keeps the name
		switch {
		case w.renameH.FocusWithin(c):
			w.renameSeen = true
		case w.renameSeen:
			w.act(func() { w.endRename(true) })
		default:
			w.renameH.Focus(c)
		}
	}
	w.runLater(c)
	w.persist()
}

// runLater runs what the view queued, now that the frame is built.
func (w *workspace) runLater(c *ui.Context) {
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
// altDigits are the keys of the New tab page's cards, ⌥1 to ⌥6.
var altDigits = []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6}

func (w *workspace) shortcuts(c *ui.Context) {
	// ⌘K and ⌘⇧P open the palette (the palette's own dialog closes it again). Shortcuts built here wait
	// while a dialog shows, so while the palette is open no other window key acts.
	c.OnWindowShortcut(ui.Cmd, ui.KeyK, func() { w.act(w.togglePalette) })
	c.OnWindowShortcut(ui.Cmd|ui.Shift, ui.KeyP, func() { w.act(w.togglePalette) })
	digits := [...]ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8}
	for i, k := range digits {
		c.OnWindowShortcut(ui.Cmd, k, func() { w.act(func() { w.selectTab(i) }) })
	}
	c.OnWindowShortcut(ui.Cmd, ui.Key9, func() { w.act(func() { w.selectTab(len(w.tabs) - 1) }) })
	c.OnWindowShortcut(ui.Cmd, ui.KeyW, func() { w.act(w.closeFocused) })
	c.OnWindowShortcut(ui.Cmd, ui.KeyT, func() { w.act(w.newTabKey) })
	c.OnWindowShortcut(ui.Cmd|ui.Shift, ui.KeyO, func() { w.act(func() { w.openQuick("", nil) }) })
	c.OnWindowShortcut(ui.Cmd, ui.KeyN, func() { w.act(w.newHost) }) // New host (docs/shortcuts.md)
	if t := w.tab(); t != nil && t.isNew() {                         // ⌥1 to ⌥6 open the nth card of the New tab page
		for i, k := range altDigits {
			c.OnWindowShortcut(ui.Alt, k, func() { w.act(func() { w.openCard(t, i) }) })
		}
	}
	c.OnWindowShortcut(ui.Cmd, ui.KeyB, func() { w.act(func() { w.sidebar = !w.sidebar }) })
	c.OnWindowShortcut(ui.Cmd|ui.Shift, ui.KeyD, func() { w.act(func() { w.splitFocused(layout.Row) }) })
	c.OnWindowShortcut(ui.Cmd|ui.Shift, ui.KeyE, func() { w.act(func() { w.splitFocused(layout.Col) }) })
	c.OnWindowShortcut(ui.Cmd|ui.Shift, ui.KeyEnter, func() { w.act(w.zoomFocused) })
	c.OnWindowShortcut(ui.Cmd|ui.Shift, ui.KeyT, func() { w.act(w.reopenClosed) })
	// ⌘< and ⌘> are ⌘⇧, and ⌘⇧. on a US keyboard: both spellings are taken (docs/shortcuts.md).
	for _, k := range []ui.Key{ui.KeyBracketLeft, ui.KeyComma} {
		c.OnWindowShortcut(ui.Cmd|ui.Shift, k, func() { w.act(func() { w.cycleTab(-1) }) })
	}
	for _, k := range []ui.Key{ui.KeyBracketRight, ui.KeyPeriod} {
		c.OnWindowShortcut(ui.Cmd|ui.Shift, k, func() { w.act(func() { w.cycleTab(1) }) })
	}
	c.OnWindowShortcut(ui.Cmd, ui.KeyF, func() { w.act(func() { w.openFind(w.focusedPane()) }) })
	if w.findPane != nil {
		c.OnWindowShortcut(ui.Cmd, ui.KeyG, func() { w.act(func() { w.findStep(true) }) })
		c.OnWindowShortcut(ui.Cmd|ui.Shift, ui.KeyG, func() { w.act(func() { w.findStep(false) }) })
		// Escape closes the bar from anywhere in the window, as in the webview (term.js closeOpenFind).
		c.OnWindowShortcut(0, ui.KeyEscape, func() { w.act(func() { w.closeFind(true) }) })
	}
	// Terminal text size: ⌘+ is ⌘= with or without ⇧.
	c.OnWindowShortcut(ui.Cmd, ui.KeyEqual, func() { w.act(func() { w.setTermSize(w.termSize + 1) }) })
	c.OnWindowShortcut(ui.Cmd|ui.Shift, ui.KeyEqual, func() { w.act(func() { w.setTermSize(w.termSize + 1) }) })
	c.OnWindowShortcut(ui.Cmd, ui.KeyMinus, func() { w.act(func() { w.setTermSize(w.termSize - 1) }) })
	c.OnWindowShortcut(ui.Cmd, ui.Key0, func() { w.act(func() { w.setTermSize(termSizeDefault) }) })
}

func (w *workspace) titlebar(c *ui.Context) {
	// TitleBar().Left is 0 when no lights are drawn (full screen, or none on this platform): the row
	// then starts at the normal padding. The view redraws when it changes.
	inset := float32(space2)
	if left := c.TitleBar().Left; left > 0 {
		inset = left + space3
	}
	hairline(ui.Row(c).FillWidth().Height(sizeTitlebar).Shrink(0).Background(w.ground(chrome)), 0, 0, 1, 0).
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
		Label("tab " + t.name()).Cursor(ui.CursorPointer).Key(t).Bind(&t.h).Drag(t)
	switch {
	case selected:
		r.Background(raised)
	case r.Hovered():
		r.Background(hoverGround)
	}
	r.PopoverMenu(w.tabMenu(t))
	renaming := w.renaming == t
	r.Children(func() {
		status := ""
		if p := t.panes[t.focused]; p != nil {
			status = p.status()
		}
		if t.isNew() {
			icon(c, "computer-terminal-01", inkMuted)
		} else {
			dot(c, statusColor(status))
		}
		col := inkMuted
		if selected {
			col = ink
		}
		if renaming {
			in := ui.TextInputBase(c, &w.renameText).Label("Rename tab").TextColor(ink).FontSize(uiFontSize).
				FontWeight(weightMedium).Width(tabMaxWidth / 2).Bind(&w.renameH)
			if !w.renameSeen { // the old name is selected, so typing replaces it
				in.SetTextSelection(0, utf8.RuneCountInString(w.renameText))
			}
			in.OnTextKey(func(k ui.TextKey) bool {
				switch {
				case k.Key == ui.KeyEnter && k.Mods == 0:
					w.renameText = k.Text()
					w.act(func() { w.endRename(true) })
				case k.Key == ui.KeyEscape && k.Mods == 0:
					w.act(func() { w.endRename(false) })
				default:
					return false
				}
				return true
			})
		} else {
			bodyStrong(c, t.name(), col)
		}
		if n := len(t.panes); n > 1 {
			ui.Spacer(c)
			caption(c, fmt.Sprint(n), inkMuted)
		}
	})
	if r.Clicked() && !renaming {
		w.act(func() { w.selectTab(i) })
	}
	// A tab dropped on another lands before it or after it, by which half the pointer let go in.
	if moved, ok := ui.Drop[*tab](r); ok && moved != t {
		x, _, _ := r.PointerPosition()
		slot := i
		if x > t.h.Bounds(c).W/2 {
			slot++
		}
		w.act(func() { w.moveTab(moved, slot) })
	}
}

func (w *workspace) sidebarView(c *ui.Context) {
	hairline(ui.Box(c).Width(sizeSidebar).Shrink(0).FillHeight().Background(w.ground(chrome)), 0, 1, 0, 0).Children(func() {
		ui.Column(c).Grow(1).Padding(space2).Gap(space2).Children(func() {
			ui.Row(c).FillWidth().Shrink(0).Gap(space1).Children(func() {
				ui.Row(c).Grow(1).Shrink(1).MinWidth(0).Height(sizeControl).Padding(0, space2).Gap(space1_5).Radius(radiusSm).
					Background(stage).Border(1, lineStrong).Children(func() {
					icon(c, "search-01", inkFaint)
					ui.TextInputBase(c, &w.filter).Label("Filter hosts").Placeholder("Filter hosts").
						PlaceholderColor(inkFaint).TextColor(ink).FontSize(uiFontSize).Grow(1)
				})
				if iconBtn(c, "add-01", "New host", sizeControl, false) { // btn-new-host
					w.act(w.newHost)
				}
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
		Label("host " + host.Name).Cursor(ui.CursorPointer).Key(host.ID).Focusable()
	hover := r.Hovered()
	if current || hover {
		r.Background(hoverGround)
	}
	r.PopoverMenu(w.hostMenu(host))
	// ⌥↵ on a focused row opens it in a split of the focused pane (term.js host-list keydown); ↵ alone clicks the
	// focused row, which opens it.
	r.ObserveInput(func(ev ui.InputEvent) {
		if ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyEnter && ev.Mods == ui.Alt {
			w.act(func() { w.openHostInSplit(host) })
		}
	})
	r.Children(func() {
		ui.Row(c).Size(sizeTileSm, sizeTileSm).Radius(radiusXs).Justify(ui.Center).Shrink(0).
			Background(hostTint(host.Color).Alpha(tileTintAlpha)).Children(func() {
			iconSized(c, "server", hostTint(host.Color), iconSizeSm)
		})
		body(c, host.Name, ink).Grow(1).Shrink(1).MinWidth(0)
		if hover || r.FocusWithin() {
			iconMenuBtn(c, "more-horizontal", "Actions for "+host.Name, sizeControlSm, w.hostMenu(host))
		} else if current {
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
		w.centered(c, "") // the window keeps a tab: this is only the moment before the first listing
	case t.isNew():
		w.startPageView(c, t)
	default:
		if t.panes[t.zoomed] == nil || len(t.panes) < 2 {
			t.zoomed = ""
		}
		// Focus view shows the zoomed pane alone, full-bleed like a one-pane tab; the others keep their sessions.
		multi := len(t.panes) > 1 && t.zoomed == ""
		tiles := func() { w.node(c, t, t.root, multi, layout.Row, true, 1) }
		if t.zoomed != "" {
			tiles = func() {
				w.tile(c, t, t.panes[t.zoomed], false, func(e ui.Element) ui.Element { return e.Grow(1).Shrink(1).Fill() })
			}
		}
		switch {
		case multi && w.alpha < 1:
			// Translucent: the chrome ground is painted only in the inset and the gaps, never under a
			// tile, so a tile's own ground is not tinted twice.
			ui.Column(c).Grow(1).FillHeight().Children(func() {
				w.inset(c, true)
				ui.Row(c).FillWidth().Grow(1).Shrink(1).Children(func() {
					w.inset(c, false)
					ui.Box(c).Grow(1).FillHeight().Shrink(1).Children(tiles)
					w.inset(c, false)
				})
				w.inset(c, true)
			})
		case multi:
			// The padding and the gaps between tiles are the chrome ground (the rings paint over it).
			ui.Box(c).Grow(1).FillHeight().Padding(space1_5).Background(chrome).Children(tiles)
		default:
			// A single pane paints its own stage ground once; the area is clear.
			ui.Box(c).Grow(1).FillHeight().Children(tiles)
		}
	}
}

// inset is a strip of chrome ground around the tiles: across the stage when wide, else down its side.
func (w *workspace) inset(c *ui.Context, wide bool) {
	b := ui.Box(c).Shrink(0).Background(w.ground(chrome))
	if wide {
		b.FillWidth().Height(space1_5)
	} else {
		b.FillHeight().Width(space1_5)
	}
}

func (w *workspace) centered(c *ui.Context, msg string) {
	ui.Column(c).Grow(1).FillHeight().Center().Background(w.ground(stage)).Children(func() {
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
	if multi && w.alpha < 1 {
		// Translucent: nothing is painted under a tile, so the area outside its rounded corners would be
		// clear. A wrapper the size of the tile paints those four corner pieces in chrome, once.
		fit(ui.Box(c)).Draw(func(pt *ui.Painter, r ui.Rect) { cornerPieces(pt, r, radiusMd, w.ground(chrome)) }).Children(func() {
			w.tileBody(c, t, p, multi, func(e ui.Element) ui.Element { return e.Fill() })
		})
		return
	}
	w.tileBody(c, t, p, multi, fit)
}

// cornerPieces fills the four pieces of r that lie outside a rounded corner of radius rad.
func cornerPieces(pt *ui.Painter, r ui.Rect, rad float32, col ui.Color) {
	const k = 0.5523 // a quarter circle as one cubic Bézier
	x0, y0, x1, y1 := r.X, r.Y, r.X+r.W, r.Y+r.H
	var path ui.Path
	for _, cn := range [4][4]float32{{x0, y0, 1, 1}, {x1, y0, -1, 1}, {x1, y1, -1, -1}, {x0, y1, 1, -1}} {
		x, y, sx, sy := cn[0], cn[1], cn[2], cn[3]
		path.MoveTo(x, y).LineTo(x+sx*rad, y).
			CubeTo(x+sx*rad*(1-k), y, x, y+sy*rad*(1-k), x, y+sy*rad).Close()
	}
	pt.FillPath(&path, col)
}

func (w *workspace) tileBody(c *ui.Context, t *tab, p *pane, multi bool, fit func(ui.Element) ui.Element) {
	focused := t.focused == p.id
	tl := fit(ui.Column(c)).Label("pane " + p.id).Key(p.id)
	if multi {
		tl.Radius(radiusMd).Clip()
		if focused {
			tl.Shadow(0, 0, 0, focusRingWidth, focus.Alpha(focusRingAlpha))
		}
	}
	tl.Children(func() {
		barGround, hostCol := w.ground(surface), inkMuted
		if focused {
			hostCol = ink
		}
		if !multi && !focused {
			barGround = w.ground(chrome)
		}
		st := p.conn.Snapshot()
		bar := ui.Row(c).FillWidth().Height(sizePanebar).Shrink(0).Padding(0, space1, 0, space3).Gap(space2).Background(barGround).Label("pane bar " + p.id)
		bar.PopoverMenu(w.paneMenu(t, p))
		if bar.Clicked() {
			w.act(func() { p.handle.Focus(c); t.focused = p.id })
		}
		bar.Children(func() {
			sub := st.Subtitle
			dot(c, statusColor(st.Status))
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
			if len(st.Attached) > 1 {
				roleChip(c, st.Role)
			}
			ui.Row(c).Gap(space0_5).Shrink(0).Children(func() {
				if iconBtn(c, "split-h", "Split right", sizeControlSm, false) {
					w.act(func() { w.split(t, p.id, layout.Row) })
				}
				if iconBtn(c, "split-v", "Split down", sizeControlSm, false) {
					w.act(func() { w.split(t, p.id, layout.Col) })
				}
				zoomed := t.zoomed == p.id
				zoomTip, zoomGlyph := "Focus view", "zoom"
				if zoomed {
					zoomTip, zoomGlyph = "Exit focus view", "unzoom"
				}
				if iconBtn(c, zoomGlyph, zoomTip, sizeControlSm, zoomed) {
					w.act(func() { w.toggleZoom(t, p.id) })
				}
				iconMenuBtn(c, "more", "More pane actions", sizeControlSm, w.paneMenu(t, p))
				if iconBtn(c, "cancel-01", "Close pane", sizeControlSm, false) {
					w.act(func() { w.closePane(t, p.id) })
				}
			})
		})
		w.paneBanners(c, t, p, st)
		if w.findPane == p {
			w.findBar(c, p)
		}
		pane := ui.Box(c).Grow(1).FillWidth().Background(w.ground(stage))
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
	if w.alpha < 1 {
		g.Background(w.ground(chrome)) // the area behind the tiles is clear when translucent
	}
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
	hairline(ui.Row(c).FillWidth().Height(statusbarSize).Shrink(0).Padding(0, space3).Gap(space4).Background(w.ground(chrome)), 1, 0, 0, 0).Children(func() {
		ui.Row(c).Gap(space1_5).Children(func() {
			dot(c, tide)
			noun := "sessions"
			if w.paneCount() == 1 {
				noun = "session" // term.js:573
			}
			caption(c, fmt.Sprintf("keel serve · %d %s", w.paneCount(), noun), inkMuted)
		})
		if w.err != "" {
			caption(c, w.err, flare)
		}
		if w.notice != "" { // what an import added or refused, until the next tab change
			col := tide
			if w.noticeBad {
				col = flare
			}
			caption(c, w.notice, col)
		}
		ui.Spacer(c)
		if p := w.focusedPane(); p != nil {
			cols, rows := p.size()
			caption(c, fmt.Sprintf("%s · %d×%d · UTF-8", p.host.Name, cols, rows), inkMuted)
		} else if t := w.tab(); t != nil && t.isNew() {
			status := "New tab" // term.js renderTabs
			if t.page.connecting != "" {
				status = "Connecting…"
			}
			caption(c, status, inkMuted)
		}
	})
}
