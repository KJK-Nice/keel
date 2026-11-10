package native

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
	"keel/internal/native/sshparse"
)

// The New tab page, laid out as the webview's (.k-start-* in keel.css): "Where to?", the input, the offer for a
// pasted command, the recent cards, the Start actions and the recently closed rows. The words are term.js
// paintStartPage and lib/newtab.js. Start has Quick connect and New host: Join session waits for phase N6
// (docs/native.md).

const (
	startGap      = space6 + space1 // calc(space-6 + space-1)
	startGrid     = 3
	startMaxWidth = sizeSidebar * 3
	startInputH   = sizeControlLg * 3 / 2
	startActionH  = sizeControlLg * 5 / 4
)

// startPageView is the stage for a New tab.
func (w *workspace) startPageView(c *ui.Context, t *tab) {
	p := t.page
	cards := w.cards(p)
	p.sync(cards)
	offer, offerErr := p.offer()
	q := strings.TrimSpace(p.query)
	page := ui.Scroll(c).Grow(1).FillHeight().Background(w.ground(stage)).Label("start page")
	// ⌥1 to ⌥6 open a card, also while the input has the keyboard, where a Mac's ⌥1 types "¡": the page sees
	// the key as it is pressed (the webview matches the key's code for the same reason).
	page.ObserveInput(func(ev ui.InputEvent) {
		if ev.Kind == ui.InputKeyDown && ev.Mods == ui.Alt {
			if i := slices.Index(altDigits, ev.Key); i >= 0 {
				w.act(func() { w.openCard(t, i) })
			}
		}
	})
	page.Children(func() {
		ui.Row(c).FillWidth().Justify(ui.Center).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(startMaxWidth).Padding(space8*2, space8, space8).Gap(startGap).Children(func() {
				w.startHead(c, t)
				switch {
				case offer != nil:
					w.offerView(c, offer)
				case offerErr != "":
					ui.Text(c, offerErr).TextColor(flare).FontSize(uiFontSize)
				}
				switch {
				case len(cards) > 0:
					head := "Recent"
					if q != "" {
						head = "Hosts"
					}
					w.section(c, head, "⌥1–⌥6 to open", func() {
						ui.Grid(c).Columns(startGrid).Gap(space2).FillWidth().Children(func() {
							for _, h := range cards {
								w.card(c, t, h)
							}
						})
					})
				case q != "" && offer == nil && offerErr == "":
					body(c, "No saved host matches “"+q+"”.", inkMuted)
				}
				w.section(c, "Start", "", func() {
					ui.Grid(c).Columns(startGrid).Gap(space2).FillWidth().Children(func() {
						w.startAction(c, "action", "Quick connect", "⌘⇧O", func() { w.openQuick("", nil) })
						w.startAction(c, "add-01", "New host", "⌘N", w.newHost)
					})
				})
				if closed := w.closedList(); len(closed) > 0 {
					w.section(c, "Recently closed", "reopens as a new session", func() {
						ui.Column(c).FillWidth().Children(func() {
							now := time.Now()
							for i, e := range closed[:min(len(closed), closedShown)] {
								w.closedRowView(c, t, i, e, now)
							}
						})
					})
				}
			})
		})
	})
	if p.focus && !w.modalOpen() {
		p.input.Focus(c)
		p.focus = false
	}
}

func (w *workspace) startHead(c *ui.Context, t *tab) {
	p := t.page
	ui.Column(c).FillWidth().Gap(space2).Children(func() {
		ui.Text(c, "Where to?").TextColor(ink).FontSize(displaySize).FontWeight(weightSemibold).MaxLines(1)
		ui.Row(c).FillWidth().Height(startInputH).Padding(0, space2, 0, space3).Gap(space3).Radius(radiusMd).
			Background(surface).Border(1, focus).Shadow(0, 0, 0, 1, focus).Children(func() {
			icon(c, "computer-terminal-01", inkFaint)
			in := ui.TextInputBase(c, &p.query).Label("Host to connect to").
				Placeholder("saved host, user@host:port, or paste an ssh command").PlaceholderColor(inkFaint).
				TextColor(ink).FontSize(titleSize).Font(monoStack).Grow(1).MinWidth(0).MaxLength(maxInput).Bind(&p.input)
			in.OnTextKey(func(k ui.TextKey) bool {
				if k.Mods != 0 {
					return false
				}
				switch k.Key {
				case ui.KeyEnter:
					p.query = k.Text()
					w.act(func() { w.submitStart(t) })
				case ui.KeyDown:
					p.query = k.Text()
					w.act(func() { w.movePick(t, 1) })
				case ui.KeyUp:
					p.query = k.Text()
					w.act(func() { w.movePick(t, -1) })
				default:
					return false
				}
				return true
			})
			kbd(c, "↵")
		})
		ui.RichText(c).FontSize(captionSize).TextColor(inkMuted).Children(func() {
			ui.Text(c, "Paste ")
			ui.Text(c, "ssh -J bastion deploy@10.0.4.21 -p 2222").Font(monoStack)
			ui.Text(c, " and Keel reads it, shows what it kept and left out, then offers to save it as a host. Nothing runs.")
		})
	})
}

// section is a k-start-section: a head (name left, a note right) and its body.
func (w *workspace) section(c *ui.Context, head, note string, bodyFn func()) {
	ui.Column(c).FillWidth().Gap(space2).Children(func() {
		ui.Row(c).FillWidth().Gap(space2).Children(func() {
			text(c, head, inkFaint, captionSize, weightSemibold)
			if note != "" {
				ui.Spacer(c)
				text(c, note, inkFaint, captionSize, weightSemibold)
			}
		})
		bodyFn()
	})
}

// card is one recent host (k-start-card): its tile, name and address, the live count and the ⌥ number.
func (w *workspace) card(c *ui.Context, t *tab, h keelclient.Host) {
	p := t.page
	r := ui.Row(c).FillWidth().Padding(space3).Gap(space3).Radius(radiusMd).Background(surface).Label("start card " + h.Name).Cursor(ui.CursorPointer)
	border := line
	switch {
	case p.pick == h.ID:
		border = focus
	case r.Hovered():
		r.Background(hoverGround)
		border = lineStrong
	}
	r.Border(1, border)
	r.Children(func() {
		ic := "server"
		if h.AuthType == "local" {
			ic = "computer-terminal-01"
		}
		ui.Row(c).Size(sizeControl, sizeControl).Radius(radiusSm).Justify(ui.Center).Shrink(0).
			Background(hostTint(h.Color).Alpha(tileTintAlpha)).Children(func() { icon(c, ic, hostTint(h.Color)) })
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space0_5).Children(func() {
			bodyStrong(c, h.Name, ink).MinWidth(0)
			ui.Row(c).Gap(space1_5).MinWidth(0).Children(func() {
				if p.connecting == h.ID {
					cardSub(c, "Connecting…")
					return
				}
				if n := w.panesOn(h.ID); n > 0 {
					stateDot(c, "live")
					cardSub(c, fmt.Sprintf("%d live · ", n))
				}
				cardSub(c, hostSubtitle(h))
			})
		})
		for i, o := range w.cards(p) {
			if o.ID == h.ID && i < recentCards {
				ui.Row(c).Absolute().Top(space1_5).Right(space1_5).Padding(1, space1).Radius(radiusXs).Background(brass).Children(func() {
					text(c, fmt.Sprintf("⌥%d", i+1), onBrass, keyhintSize, weightMedium).Font(monoStack)
				})
			}
		}
	})
	if r.Clicked() {
		w.act(func() { w.openFrom(t, h) })
	}
}

func cardSub(c *ui.Context, s string) { text(c, s, inkMuted, kbdSize, weightRegular).Font(monoStack) }

// startAction is a k-start-action button: icon, label, key cap.
func (w *workspace) startAction(c *ui.Context, glyph, name, keys string, run func()) {
	r := ui.Row(c).FillWidth().Height(startActionH).Padding(0, space3).Gap(space3).Radius(radiusMd).Border(1, line).Label(name).Cursor(ui.CursorPointer)
	col := inkMuted
	if r.Hovered() {
		r.Background(hoverGround)
		col = ink
	}
	r.Children(func() {
		icon(c, glyph, inkMuted)
		text(c, name, col, uiFontSize, weightMedium)
		ui.Spacer(c)
		kbd(c, keys)
	})
	if r.Clicked() {
		w.act(run)
	}
}

// closedRowView is a recently closed row (k-start-closed): the first shows ⌘⇧T.
func (w *workspace) closedRowView(c *ui.Context, t *tab, i int, e closedEntry, now time.Time) {
	label := w.closedRow(e)
	r := ui.Row(c).FillWidth().Height(sizeControlLg).Padding(0, space3).Gap(space3).Radius(radiusSm).Label("reopen " + label).Cursor(ui.CursorPointer)
	col := inkMuted
	if r.Hovered() {
		r.Background(hoverGround)
		col = ink
	}
	r.Children(func() {
		icon(c, "reopen", inkMuted)
		body(c, label, col).MinWidth(0)
		ui.Spacer(c)
		body(c, ageLabel(e.at, now), inkFaint)
		if i == 0 {
			kbd(c, "⌘⇧T")
		}
	})
	if r.Clicked() {
		w.act(func() { w.reopenClosedAt(t, i) })
	}
}

// offerView is the panel for a read command (offerHTML): what was read, what was left out, and Connect once.
// Save as host opens the host dialog filled from the command. Nothing has run.
func (w *workspace) offerView(c *ui.Context, r *sshparse.Result) {
	ui.Column(c).FillWidth().Padding(space3).Gap(space2).Background(surface).Border(1, line).Radius(radiusMd).Label("pasted command").Children(func() {
		ui.RichText(c).FontSize(uiFontSize).TextColor(ink).Children(func() {
			ui.Text(c, "Read as ")
			ui.Text(c, targetText(r)).Font(monoStack).TextColor(brassInk)
			ui.Text(c, ". Nothing has run.")
		})
		w.notesView(c, r)
		ui.Row(c).Gap(space2).Children(func() {
			w.saveAsHostButton(c, r)
			if r.User == "" {
				caption(c, "Add a user (user@host) to connect once.", inkMuted)
			} else if textBtn(c, "Connect once", btnDefault) {
				w.act(func() { w.openQuick(targetText(r), r) })
			}
		})
	})
}

// saveAsHostButton is the offer's primary button: it opens New host filled from the command (offerSaveHost).
func (w *workspace) saveAsHostButton(c *ui.Context, r *sshparse.Result) {
	b := ui.Row(c).Height(sizeControlSm).Padding(0, space2).Gap(space1_5).Radius(radiusSm).Justify(ui.Center).Shrink(0).Background(brass).Label("Save as host").Cursor(ui.CursorPointer)
	if b.Hovered() {
		b.Background(brassStrong)
	}
	b.Children(func() {
		label(c, "Save as host", onBrass)
		text(c, "↵", onBrass, kbdSize, weightMedium).Font(monoStack)
	})
	if b.Clicked() {
		w.act(func() { w.saveAsHost(r) })
	}
}

// notesView is notesHTML: the options that were left out (and why) and the key-file note.
func (w *workspace) notesView(c *ui.Context, r *sshparse.Result) {
	if len(r.Dropped) > 0 {
		text(c, fmt.Sprintf("Left out (%d), not applied", len(r.Dropped)), signal, captionSize, weightSemibold)
		ui.Column(c).FillWidth().Gap(space1).Label("left out").Children(func() {
			for _, d := range r.Dropped {
				ui.RichText(c).FontSize(uiFontSize).TextColor(inkMuted).Children(func() {
					ui.Text(c, d.Option).Font(monoStack)
					ui.Text(c, " "+d.Reason)
				})
			}
		})
	}
	for _, n := range r.Notes {
		ui.Text(c, n).TextColor(inkMuted).FontSize(uiFontSize)
	}
}
