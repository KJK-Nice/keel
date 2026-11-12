package native

import (
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"keel/internal/native/keelclient"
	"keel/internal/native/layout"
)

// A notebook pane (term.js makeNotebookShell, paintNotebook, syncNotebookMeta, syncNotebookReview): the pane bar, the
// review banner of an agent edit, the title, the meta line, a rule and the document. The document is shown rendered; Edit
// (or a double click on it) shows the Markdown as plain text in a text area, and Done, Escape or ⌘↵ shows it rendered
// again. The text is stored as typed: it never passes through a rich text model (docs/native-notebook-spike.md).

const nbFenceBar = sizeControlSm // the bar over a code block

// tileNotebook is the tile of a notebook pane, in the place tileBody has for a terminal.
func (w *workspace) tileNotebook(c *ui.Context, t *tab, p *pane, multi bool, fit func(ui.Element) ui.Element) {
	n := p.nb
	w.nbFrame(c, p)
	focused := t.focused == p.id
	tl := fit(ui.Column(c)).Label("pane " + p.id).Key(p.id)
	if multi {
		tl.Radius(radiusMd).Clip()
		w.tileRing(c, tl, focused)
	}
	tl.Children(func() {
		barGround, titleCol := w.ground(surface), inkMuted
		if focused {
			titleCol = ink
		}
		if !multi && !focused {
			barGround = w.ground(chrome)
		}
		bar := ui.Row(c).FillWidth().Height(sizePanebar).Shrink(0).Padding(0, space1, 0, space3).Gap(space2).Background(barGround).Label("pane bar " + p.id)
		bar.PopoverMenu(w.paneMenu(t, p))
		if bar.Clicked() {
			w.act(func() { p.handle.Focus(c); t.focused = p.id; w.revealPanes(t) })
		}
		bar.Children(func() {
			dot(c, inkFaint) // k-dot--idle
			bodyStrong(c, strings.TrimSpace(n.title), titleCol).Shrink(2).MinWidth(0)
			mono(c, "Notebook", inkMuted).Shrink(8).MinWidth(0)
			ui.Spacer(c)
			ui.Row(c).Gap(space0_5).Shrink(0).Children(func() {
				// The first tool (term.js makeNotebookShell). A private notebook does not exist for the agent (§6 rule 12), so
				// it is not offered; the app offers it and refuses.
				if rec, saved := w.notebook(n.id); !(saved && rec.Private) && iconBtn(c, "ai-magic", "Ask the agent about this notebook", sizeControlSm, false) {
					w.act(func() { w.askAboutNotebookPane(p) })
				}
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
				if w.paneCapBtn(c, focused, "⇧↵", zoomGlyph, zoomTip, zoomed, len(t.panes) < 2 && !zoomed) {
					w.act(func() { w.toggleZoom(t, p.id) })
				}
				iconMenuBtn(c, "more", "More pane actions", sizeControlSm, w.paneMenu(t, p))
				if iconBtn(c, "cancel-01", "Close pane", sizeControlSm, false) {
					w.act(func() { w.closePane(t, p.id) })
				}
			})
		})
		pane := ui.Column(c).Grow(1).FillWidth().Background(w.ground(surface)).Focusable().Label("notebook " + strings.TrimSpace(n.title)).Bind(&p.handle)
		pane.ObserveInput(func(ev ui.InputEvent) {
			if ev.Kind == ui.InputPointerDown {
				w.act(func() { w.revealPanes(t) })
			}
		})
		if multi && !focused {
			pane.Opacity(dimAlpha)
		}
		pane.Children(func() { w.notebookBody(c, t, p) })
	})
}

// nbFrame follows the pane's edits for the autosave and saves when it is due.
func (w *workspace) nbFrame(c *ui.Context, p *pane) {
	n := p.nb
	now := c.Now()
	if n.title != n.titleSeen || n.md != n.mdSeen {
		n.titleSeen, n.mdSeen = n.title, n.md
		n.auto.edit(now)
	}
	if n.auto.due(now) {
		w.act(func() { w.nbPump(p) })
	} else if d := n.auto.wake(now); d > 0 {
		c.After(d)
	}
	if rec, ok := w.notebook(n.id); ok && n.id != "" && rec.Rev > n.rev && !n.dirty() {
		w.act(w.followNotebooks)
	}
}

// notebookBody is the reading surface: the banner, the title, the meta line, a rule and the document.
func (w *workspace) notebookBody(c *ui.Context, t *tab, p *pane) {
	n := p.nb
	rec, saved := w.notebook(n.id)
	ui.Column(c).Grow(1).FillWidth().MinHeight(0).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).FillWidth().MaxWidth(sizeReading+2*space8).MinHeight(0).Padding(space6+space1, space8, 0).Children(func() {
			if saved && rec.AgentUnreviewed {
				w.notebookReview(c, rec)
			}
			// The title is the notebook's name: an editable field (term.js: aria-label "Rename notebook").
			ui.TextInputBase(c, &n.title).Label("Rename notebook").TextColor(ink).FontSize(displaySize).FontWeight(weightSemibold).FillWidth().Bind(&n.titleH)
			w.notebookMeta(c, t, p, rec, saved)
			ui.Box(c).FillWidth().Height(1).Background(line).Shrink(0).Margin(space4, 0, space1)
			if n.editing {
				w.notebookEditor(c, p)
			} else {
				w.notebookRead(c, t, p)
			}
		})
	})
}

// notebookReview is the banner on a notebook the agent edited that the user has not reviewed (syncNotebookReview): View
// changes, Undo and Mark reviewed. Only Mark reviewed clears the mark.
func (w *workspace) notebookReview(c *ui.Context, rec keelclient.Notebook) {
	head := "Edited by the agent"
	if rec.Source == byAgent && rec.LastBy == byAgent {
		head = "Written by the agent"
	}
	if when := agentWhen(rec); when != "" {
		head += " · " + when
	}
	// Wraps as the app's does (.notebook-review .k-rb-review: flex-wrap, the words' basis is six controls wide), so a narrow
	// pane puts the three buttons under the words instead of squeezing them to a letter a line.
	ui.Row(c).FillWidth().Padding(space2, space3).Gap(space2).Wrap().Radius(radiusMd).Background(hoverGround).AlignItems(ui.Center).Margin(0, 0, space4).
		Label("Agent edit to review").Children(func() {
		iconSized(c, "ai-magic", haze, iconSize)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(sizeControlLg * 6).Children(func() {
			text(c, head, ink, captionSize, weightSemibold).MinWidth(0)
			ui.Text(c, agentEditReview).TextColor(inkMuted).FontSize(captionSize).MaxLines(0)
		})
		id := rec.ID
		// View changes opens History on the version before the agent's edit; Undo restores it (term.js undoAgentEdit).
		if textBtn(c, "View changes", btnGhost) {
			w.act(func() { w.openNotebookHistoryAt(id, "before-agent-edit") })
		}
		if textBtnAs(c, "Undo agent edit", "Undo", btnGhost) {
			w.act(func() { w.undoAgentEdit(id) })
		}
		if textBtn(c, "Mark reviewed", btnPrimary) {
			w.act(func() { w.markNotebookReviewed(id) })
		}
	})
}

func agentWhen(rec keelclient.Notebook) string {
	if rec.AgentEditedAt == nil {
		return ""
	}
	return whenText(*rec.AgentEditedAt, time.Now())
}

// notebookMeta is the line under the title: Notebook · Updated Mon D · the terminal fences type into (or "No terminal in
// this tab") · Private · History · the Edited by agent badge; and Edit or Done.
func (w *workspace) notebookMeta(c *ui.Context, t *tab, p *pane, rec keelclient.Notebook, saved bool) {
	n := p.nb
	sep := func() { text(c, "·", inkFaint, captionSize, weightRegular) }
	ui.Row(c).FillWidth().Gap(space2).Wrap().AlignItems(ui.Center).Margin(space2, 0, 0).Label("notebook meta").Children(func() {
		caption(c, "Notebook", inkMuted)
		if saved && !rec.UpdatedAt.IsZero() {
			sep()
			caption(c, "Updated "+clockMD(rec.UpdatedAt), inkMuted)
		}
		sep()
		if term := t.firstTerm(); term != nil {
			ui.Row(c).Height(space4+space1).Padding(0, space1_5).Gap(space1).Radius(radiusXs).AlignItems(ui.Center).Background(hoverGround).
				Tooltip("Shell blocks type into this pane").Label("target " + term.title()).Children(func() {
				dot(c, hostTint(term.host.Color)).Size(space1_5, space1_5)
				text(c, term.title(), ink, kbdSize+space0_5, weightRegular)
			})
		} else {
			caption(c, "No terminal in this tab", inkMuted)
		}
		if saved {
			sep()
			w.metaButton(c, "lock", privateLabel(rec), rec.Private, "Private: the agent cannot see, read or change it. Click to let the agent read it.", "The agent may read and change this notebook. Click to make it private.", func() {
				id := rec.ID
				w.act(func() { w.toggleNotebookPrivate(id) })
			})
			sep()
			w.metaButton(c, "agent-history", "History", false, "", "", func() {
				id := rec.ID
				w.act(func() { w.openNotebookHistory(id) })
			})
			if rec.AgentUnreviewed {
				tag(c, "Edited by agent", signalSoft, signal)
			}
		}
		ui.Spacer(c)
		if n.editing {
			if textBtn(c, "Done", btnDefault) {
				w.act(func() { w.nbDone(p) })
			}
		} else if textBtn(c, "Edit", btnDefault) {
			w.act(func() { w.nbEdit(p) })
		}
	})
}

// metaButton is .notebook-meta-btn: a quiet text button with an icon. on shows it pressed (Private).
func (w *workspace) metaButton(c *ui.Context, glyph, name string, on bool, tipOn, tipOff string, click func()) {
	b := ui.Row(c).Height(sizeControlSm).Padding(0, space1).Gap(space1).Radius(radiusXs).AlignItems(ui.Center).Cursor(ui.CursorPointer).Label(name).Shrink(0)
	col := inkMuted
	switch {
	case on:
		b.Background(accentSoft)
		col = accentInk
	case b.Hovered():
		b.Background(hoverGround)
		col = ink
	}
	tip := tipOff
	if on {
		tip = tipOn
	}
	if tip != "" {
		b.Tooltip(tip)
	}
	b.Children(func() {
		iconSized(c, glyph, col, iconSizeSm)
		text(c, name, col, captionSize, weightRegular)
	})
	if b.Clicked() {
		click()
	}
}

// nbEdit shows the Markdown as text (Edit, a double click, or a new or empty notebook).
func (w *workspace) nbEdit(p *pane) {
	p.nb.editing, p.nb.focusEdit = true, true
}

// nbDone shows the document rendered again, after saving what was typed.
func (w *workspace) nbDone(p *pane) {
	n := p.nb
	if !n.editing {
		return
	}
	n.editing = false
	w.nbFlush(p, nil)
	w.wantFocus = p.id
}

// notebookEditor is the Markdown source as plain text: a text area in the mono font filling the pane.
func (w *workspace) notebookEditor(c *ui.Context, p *pane) {
	n := p.nb
	ui.Column(c).Grow(1).FillWidth().MinHeight(0).Margin(space3, 0, space4).Padding(space1_5, space2).Radius(radiusSm).Background(stage).Border(1, lineStrong).Children(func() {
		ta := ui.TextAreaBase(c, &n.md).Label("Markdown document").Placeholder("Markdown document").PlaceholderColor(inkFaint).TextColor(ink).
			FontSize(captionSize).Font(monoStack).Grow(1).FillWidth().Bind(&n.editH)
		ta.OnTextKey(func(k ui.TextKey) bool {
			switch {
			case k.Key == ui.KeyEscape && k.Mods == 0, k.Key == ui.KeyEnter && k.Mods == ui.Cmd:
				w.act(func() { w.nbDone(p) })
				return true
			}
			return false
		})
	})
	if n.focusEdit {
		n.editH.Focus(c)
		n.focusEdit = false
	}
}

// notebookRead is the document rendered: MyGo's Markdown view with a bar over each code block. A double click on it is Edit.
func (w *workspace) notebookRead(c *ui.Context, t *tab, p *pane) {
	n := p.nb
	fs := fences(n.md)
	n.opts.Theme = w.notebookTheme()
	n.opts.CodeBlock = func(c *ui.Context, b *markdown.CodeBlockView) { w.notebookCodeBlock(c, t, p, fs, b) }
	n.opts.OnLink = func(url string) {
		if ui.SafeToOpen(url) {
			c.Services().OpenURL(url)
		}
	}
	doc := ui.Scroll(c).Grow(1).FillWidth().MinHeight(0).Label("notebook document")
	doc.Children(func() {
		box := ui.Column(c).FillWidth().Padding(0, 0, space6).Label("notebook text")
		box.Children(func() { markdown.View(c, n.md, n.opts) })
		if box.DoubleClicked() {
			w.act(func() { w.nbEdit(p) })
		}
	})
}

// notebookTheme is the Markdown look from the tokens in use.
func (w *workspace) notebookTheme() *markdown.Theme {
	return &markdown.Theme{
		FontSize: uiFontSize * 1.077, CodeFont: monoStack, Text: ink, Muted: inkMuted, Link: haze, Strong: ink,
		Code: ink, CodeBackground: hoverGround, QuoteBar: line, QuoteText: inkMuted, Rule: line,
		CodeBlockBackground: stage, CodeBlockBorder: line, CodeBlockRadius: radiusMd, TableBorder: line, TableHeader: hoverGround,
		Checkbox: accentFill,
	}
}

// notebookCodeBlock is one fence: a bar (the language, Copy code, and for a shell fence Type into <pane>) above the code.
// The fence comes from the stored Markdown parsed by fences, by the block's index, never from what the view reports.
func (w *workspace) notebookCodeBlock(c *ui.Context, t *tab, p *pane, fs []fence, b *markdown.CodeBlockView) {
	f, ok := fenceAt(fs, b.Index)
	if !ok { // the view and fences disagree: Copy what the view holds, and offer no Type
		f = fence{Index: b.Index, Lang: strings.ToLower(b.Language), Code: strings.TrimSuffix(b.Code(), "\n")}
	}
	idx := b.Index
	lang := b.Language
	ui.Column(c).FillWidth().Background(stage).Border(1, line).Radius(radiusMd).Clip().Label("fence " + lang).Children(func() {
		ui.Row(c).FillWidth().Height(nbFenceBar).Padding(0, space1, 0, space3).Gap(space1).AlignItems(ui.Center).Children(func() {
			text(c, lang, inkMuted, captionSize, weightRegular).Font(monoStack)
			ui.Spacer(c)
			copyName := "Copy code"
			if textBtnAs(c, fenceName(copyName, idx), copyName, btnGhost) {
				code := f.Code
				w.act(func() { c.WriteClipboard(code); w.say("Copied") })
			}
			if f.Shell {
				// The app's bar always has it: "Type into <pane>", or "Type into pane" with no terminal beside the notebook
				// (notebook-editor.mjs), and pressing it then says "Split a terminal beside this notebook first".
				name, tip := "Type into pane", "Split a terminal beside this notebook first."
				if term := t.firstTerm(); term != nil {
					name, tip = "Type into "+term.title(), "Shows the command for "+term.title()+" first. Nothing is typed until you confirm."
				}
				ui.Row(c).Shrink(0).Tooltip(tip).Children(func() {
					if textBtnAs(c, fenceName(name, idx), name, btnGhost) {
						w.act(func() { w.typeNotebookFence(p, f) })
					}
				})
			}
		})
		b.Body(c)
	})
}

// fenceName tells one fence's button from another's for assistive technology and the tester: "Copy code" for the first
// block, "Copy code 2" for the one after it, by the block's index.
func fenceName(name string, index int) string { return name + " " + strconv.Itoa(index) }
