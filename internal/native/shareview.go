package native

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/wire"
)

// The Share controls and popover (§5.6), and the Allow / Deny and control-request prompts.

const (
	sharePopPx = 400 // width, min(30.769rem, …)
	avatarPx   = 26  // 2rem
)

// shareControls is the right of the title bar: the "N watching" badge and the Share button of a session tab.
func (w *workspace) shareControls(c *ui.Context) {
	p := w.shareTarget()
	if p == nil {
		return
	}
	st := p.conn.Snapshot()
	if n := watchingCount(st.Attached); n > 0 { // opens the popover with the people list
		b := ui.Row(c).Height(space4).Padding(0, space1_5).Gap(space1).Radius(radiusXs).Justify(ui.Center).Shrink(0).
			Background(hazeSoft).Label(fmt.Sprintf("%d watching", n)).Cursor(ui.CursorPointer)
		b.Children(func() {
			iconSized(c, "eye", haze, iconSizeSm)
			text(c, fmt.Sprintf("%d watching", n), haze, kbdSize, weightSemibold)
		})
		if b.Clicked() {
			w.act(w.toggleShare)
		}
	}
	b := barButton(c, "Share", "share-08", "", w.share != nil && w.share.open)
	if b.Clicked() {
		w.act(w.toggleShare)
	}
	if w.share != nil && w.share.open {
		w.sharePopover(c, b)
	}
}

// sharePopover is the popover below the Share button. Escape and a click outside close it.
func (w *workspace) sharePopover(c *ui.Context, anchor ui.Element) {
	s := w.share
	open := true
	ui.PopoverBase(c, anchor, &open, func(panel ui.Element) {
		panel.Width(rem(sharePopPx)).Background(raised).Border(1, line).Radius(radiusMd).Shadow(0, 8, 24, 0, shadowPop).Label("dialog Share session")
		w.shareHead(c, s)
		ui.Column(c).FillWidth().Padding(space3, space4).Gap(space3).Children(func() { w.shareBody(c, s) })
		st := s.p.conn.Snapshot()
		rows := sharePeople(st, time.Now())
		seat := seatOf(st)
		kind, who := popoverAction(rows, seat)
		if len(rows) > 0 {
			hairline(ui.Column(c).FillWidth().Padding(space1, space2, space2), 1, 0, 0, 0).Children(func() {
				ui.Row(c).Padding(space1_5, space2, 0).Children(func() { menuLabel(c, fmt.Sprintf("In this session · %d", len(rows))) })
				for _, r := range rows {
					w.personRow(c, s.p, r, seat, kind == "pass")
				}
			})
		}
		hairline(ui.Row(c).FillWidth().Padding(space3, space4).Gap(space2).Justify(ui.Start).Label("share foot"), 1, 0, 0, 0).Children(func() {
			if s.status != "error" && textBtn(c, "Stop sharing", btnDanger) {
				w.act(w.stopShare)
			}
			ui.Spacer(c)
			switch kind {
			case "pass":
				if textBtnIcon(c, "steering", "Pass control to "+who.name, btnDefault) {
					w.act(func() { w.passControl(s.p, who.id) })
				}
			case "take":
				if textBtnIcon(c, "steering", "Take control", btnDefault) {
					w.act(func() { w.sendControl(s.p.conn.TakeControl, "take control") })
				}
			case "ask":
				if textBtnIcon(c, "steering", "Ask for control", btnDefault) {
					w.act(func() { w.sendControl(s.p.conn.RequestControl, "ask for control") })
				}
			}
		})
	})
	if !open { // Escape or a click outside, unless the press was on a prompt: it is a click on Allow, not a dismissal
		w.act(func() {
			if !w.promptPressed {
				w.closeShare()
			}
		})
	}
}

func (w *workspace) shareHead(c *ui.Context, s *sharePop) {
	hairline(ui.Column(c).FillWidth().Padding(space3, space4).Gap(space1), 0, 0, 1, 0).Children(func() {
		ui.Row(c).FillWidth().Gap(space2).Children(func() {
			text(c, "Share session", ink, titleSize, weightSemibold)
			mono(c, s.p.host.Name, inkMuted).Shrink(1).MinWidth(0)
			ui.Spacer(c)
			badgeText, bg, fg := "Not shared", flareSoft, flare
			switch s.status {
			case "ready":
				badgeText, bg, fg = "Live link", tideSoft, tide
			case "pending":
				badgeText, bg, fg = "Creating link", hoverGround, inkMuted
			}
			ui.Row(c).Height(space4).Padding(0, space1_5).Gap(space1_5).Radius(radiusXs).Justify(ui.Center).Shrink(0).Background(bg).Label("share " + badgeText).Children(func() {
				if s.status != "error" {
					dot(c, fg)
				}
				text(c, badgeText, fg, kbdSize, weightSemibold)
			})
		})
		help(c, "People with the link see this pane's terminal only, from when you started sharing. Never your hosts, keys, vault or other panes.")
	})
}

func (w *workspace) shareBody(c *ui.Context, s *sharePop) {
	if s.status == "error" {
		msg := s.err
		if msg == "" {
			msg = "Sharing failed."
		}
		ui.Text(c, msg).TextColor(flare).FontSize(captionSize)
		return
	}
	ui.Column(c).FillWidth().Gap(space1_5).Children(func() {
		label(c, "Join link", ink)
		ui.Row(c).FillWidth().Height(sizeControl).Padding(0, space0_5, 0, space2).Gap(space1_5).Radius(radiusSm).Background(stage).Border(1, lineStrong).Label("Join link field").Children(func() {
			if s.link.url == "" {
				ui.Spinner(c).Label("Creating link").Shrink(0)
				mono(c, "Creating link…", inkFaint).Grow(1).Shrink(1).MinWidth(0)
			} else {
				mono(c, s.link.url, ink).Grow(1).Shrink(1).MinWidth(0)
			}
			name := "Copy"
			if s.copied {
				name = "Copied"
			}
			if s.link.url == "" {
				ui.Row(c).Height(sizeControlSm).Padding(0, space2).Opacity(disabledAlpha).Disabled(true).Label(name).Children(func() { label(c, name, inkMuted) })
			} else if textBtn(c, name, btnGhost) {
				url := s.link.url
				w.act(func() { w.copyQuiet(url); s.copied = true })
			}
		})
		help(c, "Works until you stop sharing or close this pane, 24 hours at most. You are asked before anyone joins; a request you do not answer is turned away after 2 minutes. Peer-to-peer over WebRTC; your loopback API stays private.")
	})
}

// personRow is a row of "In this session": an avatar, the name and how they came, the role, and Pass control when
// several people could be handed it (one person is the foot's button).
func (w *workspace) personRow(c *ui.Context, p *pane, r person, s seat, grantOne bool) {
	ui.Row(c).FillWidth().Padding(space1_5, space2).Gap(space2).Radius(radiusSm).Label("person " + r.name).Children(func() {
		bg, fg := hazeSoft, haze
		if r.you {
			bg, fg = accentSoft, accentInk
		}
		ui.Row(c).Size(rem(avatarPx), rem(avatarPx)).Radius(radiusFull).Justify(ui.Center).Shrink(0).Background(bg).Children(func() {
			text(c, r.initials, fg, rem(11), weightSemibold)
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			bodyStrong(c, r.name, ink).MinWidth(0)
			if r.sub != "" {
				mono(c, r.sub, inkMuted).MinWidth(0)
			}
		})
		roleChip(c, r.role)
		if (s.driving || s.owner) && !r.you && r.role != wire.RoleDriver && !grantOne {
			if textBtnIcon(c, "steering", "Pass control to "+r.name, btnGhost) {
				w.act(func() { w.passControl(p, r.id) })
			}
		}
	})
}

// sharePrompts is the region "Share requests", top right: an Allow / Deny prompt for each person who asks to
// watch, and a Hand over / Keep driving prompt for each request for control.
func (w *workspace) sharePrompts(c *ui.Context) {
	if len(w.prompts) == 0 {
		return
	}
	ui.Overlay(c, func() {
		ui.Column(c).Absolute().Top(sizeTitlebar + 2*sizePanebar + space2).Right(space3).Width(toastWidth).Gap(space2).Label("Share requests").Children(func() {
			for _, s := range w.prompts {
				w.sharePrompt(c, s)
			}
		})
	})
}

func (w *workspace) sharePrompt(c *ui.Context, s *sharePrompt) {
	title := s.p.host.Name
	if title == "" {
		title = "this session"
	}
	row := ui.Row(c).FillWidth().Padding(space3).Gap(space2).AlignItems(ui.Start).Radius(radiusMd).Background(raised).Border(1, line).
		Shadow(0, 8, 24, 0, shadowPop).Label("alertdialog " + s.name).Key(s.key())
	if row.PressedInside() {
		w.promptPressed = true
	}
	row.Children(func() {
		ui.Row(c).Shrink(0).Children(func() {
			if s.control {
				icon(c, "steering", haze)
			} else {
				icon(c, "eye", haze)
			}
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space1_5).Children(func() {
			ui.Row(c).FillWidth().Gap(space1).Children(func() {
				text(c, s.name, ink, uiFontSize, weightSemibold).Shrink(1).MinWidth(0)
				if s.control {
					text(c, "asks for control", ink, uiFontSize, weightSemibold).Shrink(0)
				} else {
					text(c, "wants to watch "+title+".", ink, uiFontSize, weightSemibold).Shrink(0)
				}
			})
			note := joinNote
			if s.control {
				note = controlDesc(s.p, seatOf(s.p.conn.Snapshot()).owner)
			}
			ui.Text(c, note).TextColor(inkMuted).FontSize(captionSize)
			ui.Row(c).Gap(space2).Children(func() {
				if s.control {
					if textBtn(c, "Hand over", btnDefault) {
						w.act(func() { w.handOver(s) })
					}
					if textBtn(c, "Keep driving", btnGhost) {
						w.act(func() { w.dropPrompt(s.p, s.key()) })
					}
					return
				}
				if textBtn(c, "Allow", btnDefault) {
					w.act(func() { w.answerJoin(s, true) })
				}
				if textBtn(c, "Deny", btnGhost) {
					w.act(func() { w.answerJoin(s, false) })
				}
			})
		})
	})
}

// shareFrame runs once a frame, before the view is built: it reads what the sockets announced, closes a popover whose
// pane is gone, and puts a link that has just arrived on the clipboard.
func (w *workspace) shareFrame(c *ui.Context) {
	w.promptPressed = false
	w.drainEvents()
	w.syncShare()
	if s := w.share; s != nil && s.wantCopy {
		s.wantCopy = false
		w.copyQuiet(s.link.url)
		s.copied = true
	}
}
