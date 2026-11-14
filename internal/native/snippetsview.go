package native

import (
	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The Snippets page, the sidebar's Snippets section and the snippet dialog, with the Keychain page's split layout
// (keychainview.go).

// snippetMenu is the row menu: Type into pane…, Copy, Edit, Delete. "Type into
// pane…" asks Approve before type; nothing in the menu types.
func (w *workspace) snippetMenu(c *ui.Context, s keelclient.Snippet) func(m *ui.Menu) {
	return func(m *ui.Menu) {
		if menuItem(m, "Type into pane…").Chosen() {
			w.act(func() { w.typeSnippet(s.ID) })
		}
		if menuItem(m, "Copy").Chosen() {
			w.act(func() { w.copyQuiet(s.Command) })
		}
		m.Separator()
		if menuItem(m, "Edit").Chosen() {
			w.act(func() { w.editSnippet(s) })
		}
		m.Separator()
		if menuItem(m, "Delete").Chosen() {
			w.act(func() { w.askDeleteSnippet(s) })
		}
	}
}

func (w *workspace) snippetsView(c *ui.Context, p *snippetsPage) {
	cur, hasCur := w.currentSnippet(p)
	if hasCur {
		p.ed.sync(cur)
	}
	if len(w.snippets) == 0 && w.live {
		if pr, _ := w.emptyStage(c, emptyNone("snippet", "No snippets yet", snippetsNone, "New snippet")); pr {
			w.act(w.newSnippet)
		}
		return
	}
	ui.Row(c).Grow(1).FillWidth().FillHeight().Children(func() {
		hairline(ui.Column(c).Width(sizeList).Shrink(0).FillHeight().Background(w.ground(surface)), 0, 1, 0, 0).Children(func() {
			w.splitHead(c, "Snippets", "New snippet", &p.filter, "Filter snippets", &p.filterH, w.newSnippet)
			w.snippetList(c, p, cur.ID)
		})
		ui.Scroll(c).Grow(1).FillHeight().Shrink(1).MinWidth(0).Background(w.ground(stage)).Label("snippet editor").ObserveInput(w.snippetKeys).Children(func() {
			ui.Column(c).FillWidth().Padding(space6, space8).Gap(space4).Children(func() {
				if !hasCur {
					ui.Column(c).FillWidth().Padding(space8, 0).AlignItems(ui.Center).Children(func() {
						help(c, "Select a snippet to edit it, or add one with New snippet.")
					})
					return
				}
				w.snippetEditorView(c, p, cur)
			})
		})
	})
}

func (w *workspace) snippetList(c *ui.Context, p *snippetsPage, current string) {
	shown := w.shownSnippets(p.filter)
	p.choice = -1
	for i, s := range shown {
		if s.ID == current {
			p.choice = i
		}
	}
	p.list.Key = func(i int) any { return shown[i].ID }
	p.list.Label = func(i int) string { return shown[i].Name }
	p.list.Selected = &p.choice
	l := ui.List(c, &p.list, len(shown), func(i int) {
		w.snippetRow(c, p, shown[i], shown[i].ID == current)
	}).FillWidth().Grow(1).Shrink(1).Label("snippet list")
	l.Children(func() {
		if len(shown) == 0 && w.live { // none at all is the whole page's empty state (snippetsView)
			if emptySide(c, emptyNoMatch("search-01", p.filter)) {
				w.act(func() { p.filter = "" })
			}
		}
	})
	if l.Changed() && p.choice >= 0 && p.choice < len(shown) {
		id := shown[p.choice].ID
		w.act(func() { p.selected = id })
	}
}

func (w *workspace) snippetRow(c *ui.Context, p *snippetsPage, s keelclient.Snippet, selected bool) {
	r := hairline(ui.Row(c).FillWidth().Padding(space2, space4).Gap(space3).Cursor(ui.CursorPointer).Key(s.ID).
		Label("snippet "+s.Name).Tooltip(s.Name+" · "+snippetFirstLine(s)).Selected(selected), 0, 0, 1, 0)
	hover := r.Hovered()
	if selected || hover {
		r.Background(hoverGround)
	}
	r.PopoverMenu(w.snippetMenu(c, s))
	r.Children(func() {
		ui.Row(c).Size(sizeControl, sizeControl).Radius(radiusSm).Justify(ui.Center).Shrink(0).Background(inkMuted.Alpha(tileTintAlpha)).Children(func() {
			iconSized(c, "snippet", inkMuted, iconSize-space0_5)
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space0_5).Children(func() {
			bodyStrong(c, s.Name, ink).MinWidth(0)
			text(c, snippetFirstLine(s), inkMuted, itemSub, weightRegular).Font(monoStack).MinWidth(0)
		})
	})
	if r.Clicked() {
		w.act(func() { p.selected = s.ID })
	}
}

// snippetEditorView is the editor: the name, "Type into pane…", Save, the commands, the error and Delete snippet.
func (w *workspace) snippetEditorView(c *ui.Context, p *snippetsPage, cur keelclient.Snippet) {
	e := &p.ed
	ui.Column(c).FillWidth().Gap(space4).Children(func() {
		ui.Row(c).FillWidth().Gap(space2).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				inputBox(c, "Snippet name", &e.name, "", &e.nameH, false, false, 0, nil)
			})
			if textBtn(c, "Type into pane…", btnDefault) {
				w.act(func() { w.typeSnippet(cur.ID) })
			}
			if saveBtn(c, e.saving) {
				w.act(w.saveEditor)
			}
		})
		labeled(c, "Commands", &e.cmdH, func() {
			ui.Column(c).FillWidth().Padding(space1_5, space2).Radius(radiusSm).Background(controlFill(stage)).Border(1, lineStrong).Children(func() {
				ui.TextAreaBase(c, &e.command).Label("Snippet commands").TextColor(ink).FontSize(captionSize).Font(monoStack).
					Lines(6, 6).FillWidth().Bind(&e.cmdH)
			})
			help(c, "One command per line. Each line ends with Enter when typed.")
		})
		// "Preview of what gets typed": each line with its Enter as a chip, then the count.
		lines, summary := snippetPreview(e.command)
		ui.Column(c).FillWidth().Gap(space1).Children(func() {
			label(c, "Preview of what gets typed", inkMuted)
			ui.Column(c).FillWidth().Padding(space3).Radius(radiusSm).Background(stage).Border(1, line).Label("Snippet preview").Children(func() {
				for _, l := range lines {
					ui.RichText(c).FontSize(captionSize).TextColor(ink).Font(monoStack).Children(func() {
						ui.Text(c, l)
						ui.Text(c, " ↵ ").TextColor(accentInk).Background(accentSoft).FontWeight(weightMedium)
					})
				}
			})
			help(c, summary)
		})
		ui.Row(c).FillWidth().Gap(space3).Children(func() {
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space0_5).Children(func() {
				label(c, "Let the agent suggest this snippet", inkMuted)
				help(c, "⇥ can offer it when your words match. One-line commands without {{var}} fields only. When on, its text is sent, redacted, to TypeSafe Jev.")
			})
			ui.Switch(c, &e.agent).Label("Let the agent suggest this snippet switch").Shrink(0)
		})
		if e.err != "" {
			ui.Row(c).FillWidth().Label("snippet form error").Children(func() { caption(c, e.err, flare) })
		}
		hairline(ui.Row(c).FillWidth().Padding(space2, 0, 0).Gap(space2), 1, 0, 0, 0).Children(func() {
			ui.Spacer(c)
			if textBtn(c, "Delete snippet", btnDanger) {
				w.act(func() { w.askDeleteSnippet(cur) })
			}
		})
	})
}

// saveBtn is the primary Save with its ⌘S key cap; it ignores a click while a save is out.
func saveBtn(c *ui.Context, saving bool) bool {
	busyMark(c, saving, "Saving")
	return capButton(c, "Save", "⌘S", saving).Clicked() && !saving
}

// snippetKeys is ⌘S in the editor.
func (w *workspace) snippetKeys(ev ui.InputEvent) {
	if ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyS && ev.Mods == ui.Cmd {
		w.act(w.saveEditor)
	}
}

// ---- the sidebar's Snippets section ----

// sidebarSnippets lists the snippets, each a row that asks Approve before type for the focused pane,
// and Manage snippets.
func (w *workspace) sidebarSnippets(c *ui.Context) {
	ui.Column(c).FillWidth().Gap(space0_5).Children(func() {
		shown := w.shownSnippets(w.snipQuery)
		for _, s := range shown {
			w.sideSnippetRow(c, s)
		}
		switch {
		case len(w.snippets) == 0 && w.live:
			if emptySide(c, emptyNone("snippet", "No snippets yet", snippetsNone, "New snippet")) {
				w.act(w.newSnippet)
			}
		case len(shown) == 0 && w.live:
			if emptySide(c, emptyNoMatch("search-01", w.snipQuery)) {
				w.act(func() { w.snipQuery = "" })
			}
		}
		if textBtn(c, "Manage snippets", btnGhost) {
			w.act(func() { w.openPage("snippets") })
		}
		w.sidebarNotebooks(c) // the Notebooks half of the segment, under the same filter
	})
}

func (w *workspace) sideSnippetRow(c *ui.Context, s keelclient.Snippet) {
	r := ui.Row(c).FillWidth().Height(sizeRow).Padding(0, space2).Gap(space2).Radius(radiusSm).Key(s.ID).
		Label("sidebar snippet " + snippetName(s)).Cursor(ui.CursorPointer).Focusable().Tooltip("Type into the focused pane after you approve")
	hover := r.Hovered()
	if hover {
		r.Background(hoverGround)
	}
	r.PopoverMenu(w.snippetMenu(c, s))
	r.Children(func() {
		ui.Row(c).Size(sizeTileSm, sizeTileSm).Radius(radiusXs).Justify(ui.Center).Shrink(0).Background(inkMuted.Alpha(tileTintAlpha)).Children(func() {
			iconSized(c, "snippet", inkMuted, iconSizeSm)
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space0_5).Children(func() {
			body(c, snippetName(s), ink).MinWidth(0)
			text(c, snippetFirstLine(s), inkMuted, itemSub, weightRegular).Font(monoStack).MinWidth(0)
		})
	})
	if r.Clicked() {
		w.act(func() { w.typeSnippet(s.ID) })
	}
}

// ---- the snippet dialog ----

func (w *workspace) snippetDialogView(c *ui.Context) {
	d := w.snipDlg
	if d == nil {
		return
	}
	title := "New snippet"
	if d.id != "" {
		title = "Edit snippet"
	}
	open := true
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		modalChrome(c, backdrop, panel)
		panel.Width(dialogWidth).Padding(space4).Gap(space3).Label("dialog " + title)
		panel.ObserveInput(func(ev ui.InputEvent) { // ⌘↵ saves from any field
			if ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyEnter && ev.Mods == ui.Cmd {
				w.act(w.saveSnippetDialog)
			}
		})
		text(c, title, ink, titleSize, weightSemibold)
		labeled(c, "Name", &d.nameH, func() { inputBox(c, "Snippet dialog name", &d.name, "disk usage", &d.nameH, false, false, 0, nil) })
		labeled(c, "Command", &d.cmdH, func() {
			ui.Column(c).FillWidth().Padding(space1_5, space2).Radius(radiusSm).Background(controlFill(stage)).Border(1, lineStrong).Children(func() {
				ui.TextAreaBase(c, &d.command).Label("Snippet dialog command").Placeholder("df -h").PlaceholderColor(inkFaint).TextColor(ink).
					FontSize(captionSize).Font(monoStack).Lines(4, 4).FillWidth().Bind(&d.cmdH)
			})
		})
		if d.err != "" {
			ui.Row(c).FillWidth().Label("snippet dialog error").Children(func() { caption(c, d.err, flare) })
		}
		ui.Row(c).FillWidth().Gap(space2).Justify(ui.End).Children(func() {
			if textBtn(c, "Cancel", btnGhost) {
				open = false
			}
			if textBtn(c, "Save snippet", btnPrimary) {
				w.act(w.saveSnippetDialog)
			}
		})
	})
	if d.focus && w.snipDlg == d {
		d.nameH.Focus(c)
		d.focus = false
	}
	if !open {
		w.act(w.closeSnippetDialog)
	}
}
