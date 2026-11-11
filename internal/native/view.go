package native

import (
	"fmt"
	"runtime"
	"slices"
	"strconv"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
	"keel/internal/native/layout"
)

// view draws the whole screen (docs/ui-redesign.md §5.1). It reads and changes the workspace
// on the UI thread only; changes the buttons ask for are queued with act and run at the end.
func (w *workspace) view(c *ui.Context) {
	w.resolveTheme(c)
	setUISize(w.win.UISize)
	windowTheme(c, w.dark)
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
	w.fallBackToHosts()
	w.shareFrame(c)
	w.suggestFrame(c)
	w.shortcuts(c)
	w.updateCaps(c)
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
			if w.sidebar && !w.joinedActive() { // a joined tab shows no Vault sidebar (§5.7)
				w.sidebarView(c)
			}
			w.stage(c)
			if w.agentShown() { // a joined tab has no agent panel (§5.7)
				w.agentView(c)
			}
		})
		w.statusbar(c)
	})
	w.toastsView(c) // under the dialogs: a dialog opened over a toast keeps its buttons
	w.sharePrompts(c)
	w.hostKeyDialog(c)
	w.quickDialogView(c)
	w.joinDialogView(c)
	w.hostDialogView(c)
	w.identityDialogView(c)
	w.snippetDialogView(c)
	w.notebookDialogView(c)
	w.folderDialogView(c)
	w.forwardDialogView(c)
	w.forwardKeyView(c)
	w.approveView(c)
	w.confirmView(c)
	w.paletteView(c)
	w.reviewFocus(c)
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
	w.folderRenameFocus(c)
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
	c.OnWindowShortcut(ui.Cmd, ui.KeyJ, func() { w.act(w.openJoin) })
	c.OnWindowShortcut(ui.Cmd, ui.KeyComma, func() { w.act(func() { w.openPage("settings") }) })
	c.OnWindowShortcut(ui.Cmd, ui.KeyN, func() { w.act(w.newItem) }) // New host, or the page's own New (docs/shortcuts.md)
	if t := w.tab(); t != nil && t.isNew() {                         // ⌥1 to ⌥6 open the nth card of the New tab page
		for i, k := range altDigits {
			c.OnWindowShortcut(ui.Alt, k, func() { w.act(func() { w.openCard(t, i) }) })
		}
	}
	// ⌘I toggles the agent panel; elsewhere Ctrl+⇧I, because Ctrl+I is Tab in a terminal (lib/shortcutmap.js).
	agentMods := ui.Cmd
	if runtime.GOOS != "darwin" {
		agentMods |= ui.Shift
	}
	c.OnWindowShortcut(agentMods, ui.KeyI, func() { w.act(w.toggleAgent) })
	c.OnWindowShortcut(ui.Cmd, ui.KeyB, func() {
		if !w.joinedActive() {
			w.act(func() { w.sidebar = !w.sidebar })
		}
	})
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
		if !w.joinedActive() && w.capBtn(c, "B", "panel-left", "Toggle sidebar", sizeControlSm, w.sidebar, false) { // no toggle in a joined tab
			w.act(func() { w.sidebar = !w.sidebar })
		}
		for i, t := range w.tabs {
			w.tabPill(c, i, t)
		}
		if w.capBtn(c, "T", "add-01", "New tab", sizeControlSm, false, false) {
			w.act(w.newTab)
		}
		ui.Spacer(c)
		if s := w.barStatus(); s != "" { // #bar-status in .bar-meta: ink-muted, 0.917rem
			text(c, s, inkMuted, rem(barMetaPx), weightRegular)
		}
		w.shareControls(c) // the watchers badge and the Share button of a session tab
		w.agentButton(c)
	})
}

// barStatus is #bar-status (term.js renderTabs): what the window shows at the right of the title bar. A page shows
// nothing; the New tab page says "New tab" (or "Connecting…" while it connects); a session says whose hands are on it.
func (w *workspace) barStatus() string {
	t := w.tab()
	switch {
	case t == nil || t.isPage():
		return ""
	case t.isNew():
		if t.page.connecting != "" {
			return "Connecting…"
		}
		return "New tab"
	}
	p := t.panes[t.focused]
	if p == nil {
		return "No session"
	}
	if p.nb != nil {
		return "Notebook"
	}
	if st := p.conn.Snapshot(); st.Role == "driver" {
		return paneSubtitle(p, st) + " · driving"
	}
	return paneSubtitle(p, p.conn.Snapshot()) + " · following"
}

// paneSubtitle is the mono line after the host's name in a pane's bar: the title the shell set, else user@address.
func paneSubtitle(p *pane, st keelclient.State) string {
	if st.Subtitle != "" {
		return st.Subtitle
	}
	if p.host.Username != "" {
		return p.host.Username + "@" + p.host.Address
	}
	return p.host.Address
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
		var lead ui.Element
		switch {
		case t.isNew():
			lead = icon(c, "computer-terminal-01", inkMuted)
		case t.isPage():
			lead = icon(c, pageDefs[t.pg.name].tabIcon, inkMuted)
		case t.remote: // the joined-share link (lib/tabstrip.js)
			lead = icon(c, "link", inkMuted)
		default:
			lead = dot(c, statusColor(status))
		}
		if tabKey(i, len(w.tabs)) != "" { // the cap stands in for it: nothing of it shows beside the cap
			lead.Opacity(1 - w.capAlpha)
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
		if k := tabKey(i, len(w.tabs)); k != "" {
			w.keyCapLead(c, k)
		}
		if n := len(t.panes); n > 1 {
			ui.Spacer(c)
			ui.Row(c).Height(rem(tabCountPx)).Padding(0, space1).Radius(radiusXs).Justify(ui.Center).Shrink(0).Background(hoverGround). // k-badge--mono tab-count
																			Children(func() { text(c, fmt.Sprint(n), inkMuted, rem(tabCountFontPx), weightRegular).Font(monoStack) })
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

// sideSnippets is the sidebar's Snippets section showing (Hosts is the default).
func (w *workspace) sideSnippets() bool { return w.sideSeg == "snippets" }

// sideForwards is the sidebar's Forwards section showing.
func (w *workspace) sideForwards() bool { return w.sideSeg == "forwards" }

func (w *workspace) sidebarView(c *ui.Context) {
	hairline(ui.Column(c).Width(sizeSidebar).Shrink(0).FillHeight().Background(w.ground(chrome)), 0, 1, 0, 0).Children(func() {
		// .k-sidebar-head: 12 8 8, the tools row and the section switch 8 apart.
		ui.Column(c).FillWidth().Shrink(0).Padding(space3, space2, space2).Gap(space2).Children(func() {
			ui.Row(c).FillWidth().Shrink(0).Gap(space1_5).Children(func() { // the filter's 4 and the tools' 2
				ui.Row(c).Grow(1).Shrink(1).MinWidth(0).Height(sizeControl).Padding(0, space2).Gap(space1_5).Radius(radiusSm).
					Background(stage).Border(1, lineStrong).Children(func() {
					icon(c, "search-01", inkFaint)
					switch {
					case w.sidePanes():
						ui.TextInputBase(c, &w.paneQuery).Label("Filter panes").Placeholder("Filter panes").
							PlaceholderColor(inkFaint).TextColor(ink).FontSize(uiFontSize).Grow(1).Bind(&w.paneH)
					case w.sideSnippets(): // each section has its own filter (term.js sidebarQuery)
						ui.TextInputBase(c, &w.snipQuery).Label("Filter snippets").Placeholder("Filter snippets").
							PlaceholderColor(inkFaint).TextColor(ink).FontSize(uiFontSize).Grow(1).Bind(&w.snipH)
					case w.sideForwards():
						ui.TextInputBase(c, &w.fwdQuery).Label("Filter forwards").Placeholder("Filter forwards").
							PlaceholderColor(inkFaint).TextColor(ink).FontSize(uiFontSize).Grow(1).Bind(&w.fwdH)
					default:
						ui.TextInputBase(c, &w.filter).Label("Filter hosts").Placeholder("Filter hosts").
							PlaceholderColor(inkFaint).TextColor(ink).FontSize(uiFontSize).Grow(1)
					}
				})
				ui.Row(c).Shrink(0).Gap(space0_5).Children(func() {
					if w.capBtn(c, "N", "add-01", "New host", sizeControlSm, false, false) { // btn-new-host: k-btn--sm k-btn--icon
						w.act(w.newHost)
					}
					iconMenuBtn(c, "more-horizontal", "More host actions", sizeControlSm, w.hostActionsMenu)
				})
			})
			// k-seg "Vault section": Hosts · Panes · Snippets · Forwards. Panes shows only while the tab has 2+ panes.
			segs := [][2]string{{"hosts", "Hosts"}, {"snippets", "Snippets"}, {"forwards", "Forwards"}}
			if pt := w.panesTab(); pt != nil {
				segs = slices.Insert(segs, 1, [2]string{"panes", "Panes " + strconv.Itoa(len(pt.panes))})
			}
			cur := w.sideSeg
			if cur == "" { // Hosts is the default section, and the one pressed
				cur = "hosts"
			}
			segControl(c, "Vault section", cur, segs, func(v string) { w.sideSeg = v })
		})
		// .k-sidebar-body: 0 6 8, scrolling.
		body := func(label string, kids func()) {
			ui.Scroll(c).FillWidth().Grow(1).Shrink(1).Label(label).Children(func() {
				ui.Column(c).FillWidth().Padding(0, space1_5, space2).Children(kids)
			})
		}
		switch {
		case w.sidePanes():
			body("sidebar panes", func() { w.sidebarPanes(c, w.panesTab()) })
		case w.sideSnippets():
			body("sidebar snippets", func() { w.sidebarSnippets(c) })
		case w.sideForwards():
			body("sidebar forwards", func() { w.sidebarForwards(c) })
		default:
			body("sidebar hosts", func() {
				if w.hasFolders() { // keel serve has folders: the nested tree (foldersview.go)
					w.folderTreeView(c)
					return
				}
				ui.Column(c).FillWidth().Gap(space2).Children(func() {
					for _, g := range w.groups() {
						w.group(c, g)
					}
				})
			})
		}
		w.sidebarFoot(c)
	})
}

// hostActionsMenu is the "More host actions" menu next to New host (sidebar.templ): Quick connect, Join session, New
// group and Local terminal.
func (w *workspace) hostActionsMenu(m *ui.Menu) {
	if m.Item("Quick connect").Shortcut(ui.Cmd|ui.Shift, ui.KeyO).Chosen() {
		w.act(func() { w.openQuick("", nil) })
	}
	if m.Item("Join session").Shortcut(ui.Cmd, ui.KeyJ).Chosen() {
		w.act(w.openJoin)
	}
	if m.Item("New group").Chosen() {
		w.act(func() { w.newFolder("") })
	}
	if m.Item("Local terminal").Chosen() {
		w.act(w.newLocalTab)
	}
}

// sidebarFoot is k-sidebar-foot: Keychain and Logs open their pages; Settings is the icon button on the right, with ⌘,
// (its key cap shows only while ⌘ is held in the app).
func (w *workspace) sidebarFoot(c *ui.Context) {
	hairline(ui.Row(c).FillWidth().Shrink(0).Padding(space1_5).Gap(space0_5), 1, 0, 0, 0).Children(func() {
		for _, name := range footPages {
			def := pageDefs[name]
			glyph := "key-01"
			if name == "logs" {
				glyph = "logs"
			}
			if textBtnIcon(c, glyph, def.title, btnGhost) {
				w.act(func() { w.openPage(name) })
			}
		}
		ui.Spacer(c)
		if w.capBtn(c, ",", "settings-01", "Settings", sizeControlSm, false, false) {
			w.act(func() { w.openPage("settings") })
		}
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
	if w.hasFolders() { // a host drags onto a folder (foldersview.go)
		r.Drag(&treeDrag{hosts: []string{host.ID}})
	}
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
	case t.isPage():
		w.pageView(c, t)
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
			// MinWidth/MinHeight 0 all the way down: the stage takes the room the window gives it, whatever the
			// tiles' content (the pane bar's natural width) would like.
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).MinHeight(0).FillHeight().Children(func() {
				w.inset(c, true)
				ui.Row(c).FillWidth().Grow(1).Shrink(1).MinHeight(0).Children(func() {
					w.inset(c, false)
					ui.Box(c).Grow(1).FillHeight().Shrink(1).MinWidth(0).Children(tiles)
					w.inset(c, false)
				})
				w.inset(c, true)
			})
		case multi:
			// The padding and the gaps between tiles are the chrome ground (the rings paint over it).
			ui.Box(c).Grow(1).Shrink(1).MinWidth(0).MinHeight(0).FillHeight().Padding(space1_5).Background(chrome).Children(tiles)
		default:
			// A single pane paints its own stage ground once; the area is clear.
			ui.Box(c).Grow(1).Shrink(1).MinWidth(0).MinHeight(0).FillHeight().Children(tiles)
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
		// MinWidth/MinHeight 0: a tile's content (the pane bar's tools) must not hold a side at its natural
		// width, or nested splits stop sharing the room in their ratio and run off the stage.
		e = e.Grow(grow).Shrink(1).MinWidth(0).MinHeight(0)
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
	if p.nb != nil { // a notebook has its own bar and body (notebookpane.go)
		w.tileNotebook(c, t, p, multi, fit)
		return
	}
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
			w.act(func() { p.handle.Focus(c); t.focused = p.id; w.revealPanes(t) })
		}
		bar.Children(func() {
			sub := st.Subtitle
			dot(c, statusColor(st.Status))
			// The tools never shrink; user gives way first, then the host name (Shrink is the weight).
			bodyStrong(c, p.title(), hostCol).Shrink(2).MinWidth(0)
			if sub == "" {
				sub = p.host.Address
				if p.host.Username != "" {
					sub = p.host.Username + "@" + sub
				}
			}
			mono(c, sub, inkMuted).Shrink(8).MinWidth(0)
			ui.Spacer(c)
			switch {
			case p.remote: // who drives, and "You follow" (§5.7)
				joinChips(c, st)
			case len(st.Attached) > 1:
				roleChip(c, st.Role)
			}
			ui.Row(c).Gap(space0_5).Shrink(0).Children(func() {
				focused := t.focused == p.id // only the focused pane's tools have caps (keel.css:1032)
				if w.paneCapBtn(c, focused, "⇧D", "split-h", "Split right", false, false) {
					w.act(func() { w.split(t, p.id, layout.Row) })
				}
				if w.paneCapBtn(c, focused, "⇧E", "split-v", "Split down", false, false) {
					w.act(func() { w.split(t, p.id, layout.Col) })
				}
				zoomed := t.zoomed == p.id
				zoomTip, zoomGlyph := "Focus view", "zoom"
				if zoomed {
					zoomTip, zoomGlyph = "Exit focus view", "unzoom"
				}
				if w.paneCapBtn(c, focused, "⇧↵", zoomGlyph, zoomTip, zoomed, len(t.panes) < 2 && !zoomed) { // needs another pane to zoom out of
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
		pane := ui.Box(c).Grow(1).FillWidth().Background(w.termGround())
		// A press into the terminal of a split tab shows the Panes section (term.js mousedown → revealPanes). The press
		// is only watched: the terminal keeps the keyboard. The bar's buttons do not count; the bar itself does.
		pane.ObserveInput(func(ev ui.InputEvent) {
			if ev.Kind == ui.InputPointerDown {
				w.act(func() { w.revealPanes(t) })
			}
		})
		if multi && !focused {
			pane.Opacity(dimAlpha)
		}
		pane.DrawOver(func(pt *ui.Painter, _ ui.Rect) { p.sg.scale = pt.Scale() }) // draws nothing: the notice and the card are placed by the display scale
		pane.Children(func() {
			p.view(c).Bind(&p.handle)
			w.suggestOverlay(c, p)
		})
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
		if n := w.runningForwards(); n > 0 { // #sb-forwards is hidden with none running
			ui.Row(c).Gap(space1_5).Label("status forwards").Children(func() {
				iconSized(c, "forwards", inkMuted, iconSizeSm)
				caption(c, forwardsLabel(n), inkMuted)
			})
		}
		ui.Spacer(c)
		if p := w.focusedPane(); p != nil && p.nb == nil {
			cols, rows := p.size()
			caption(c, fmt.Sprintf("%s · %d×%d · UTF-8", p.title(), cols, rows), inkMuted)
		}
	})
}

// agentButton is the title bar's Agent button with its key cap (internal/ui/agent.templ agentToggle). A joined tab has no
// panel and no button (§5.7).
func (w *workspace) agentButton(c *ui.Context) {
	if w.joinedActive() {
		return
	}
	b := ui.Row(c).Height(sizeControlSm).Padding(0, space2).Gap(space1_5).Radius(radiusSm).Justify(ui.Center).Shrink(0).
		Label("Agent").Cursor(ui.CursorPointer)
	fg := inkMuted
	if w.agent.open || b.Hovered() {
		b.Background(hoverGround)
		fg = ink
	}
	b.Children(func() {
		icon(c, "ai-magic", fg)
		label(c, "Agent", fg)
		kbd(c, agentKeys()) // one cap, as the app's #btn-agent-kbd
	})
	if b.Clicked() {
		w.act(w.toggleAgent)
	}
}
