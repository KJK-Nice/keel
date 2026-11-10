package native

import (
	"strconv"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// What a pane's session is doing, as the user sees it (docs/ui-redesign.md §5.1): a failure or
// connecting banner, a follower banner, a role chip, and the host key dialog. The words are the
// webview's: term.js failHeadline/failEndpoint, hostKeyDialog in internal/ui/dialogs.templ,
// lib/share.js ROLE_LABEL.

type btnKind int

const (
	btnDefault btnKind = iota
	btnGhost
	btnPrimary
	btnDanger
)

// textBtn is a small k-btn; it reports a click.
func textBtn(c *ui.Context, name string, kind btnKind) bool {
	b := ui.Row(c).Height(sizeControlSm).Padding(0, space2).Radius(radiusSm).Justify(ui.Center).Shrink(0).
		Label(name).Cursor(ui.CursorPointer)
	bg, fg, hov := raised, ink, b.Hovered()
	switch kind {
	case btnGhost:
		bg, fg = ui.Transparent, inkMuted
		if hov {
			bg, fg = hoverGround, ink
		}
	case btnPrimary:
		bg, fg = brass, onBrass
		if hov {
			bg = brassStrong
		}
	case btnDanger:
		bg, fg = flareSoft, flare
		if hov {
			bg, fg = flare, onFlare
		}
	default:
		if hov {
			bg = hoverGround
		}
	}
	b.Background(bg)
	if kind == btnDefault {
		b.Border(1, lineStrong)
	}
	b.Children(func() { label(c, name, fg) })
	return b.Clicked()
}

// failHeadline is term.js failHeadline for a session that is down.
func failHeadline(st keelclient.State) string {
	switch {
	case st.Status == "exited":
		return "Session ended"
	case st.WasReady:
		return "Connection lost"
	}
	return "Connection failed"
}

// failEndpoint is term.js failEndpoint.
func failEndpoint(h keelclient.Host, st keelclient.State) string {
	if h.AuthType == "" || h.AuthType == "local" {
		if st.Subtitle != "" {
			return st.Subtitle
		}
		return "Local shell"
	}
	port := h.Port
	if port == 0 {
		port = 22
	}
	return "SSH " + h.Address + ":" + strconv.Itoa(port)
}

// bannerRow is a k-pane-banner: a soft ground with its text colour, a headline with the endpoint (and a
// message under it) on the left, the buttons on the right. The soft ground sits on stage, as in the webview.
func (w *workspace) bannerRow(c *ui.Context, name string, soft, fg ui.Color, head, endpoint, msg string, buttons func()) {
	ui.Box(c).FillWidth().Shrink(0).Background(w.ground(stage)).Children(func() {
		hairline(ui.Row(c).FillWidth().Padding(space1_5, space3).Gap(space2).Background(soft).Label(name), 0, 0, 1, 0).Children(func() {
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				ui.Row(c).Gap(space2).Children(func() {
					label(c, head, fg).Shrink(0)
					if endpoint != "" {
						mono(c, endpoint, fg).Shrink(1).MinWidth(0)
					}
				})
				if msg != "" {
					caption(c, msg, fg).MinWidth(0)
				}
			})
			ui.Row(c).Gap(space1_5).Shrink(0).Children(buttons)
		})
	})
}

// paneBanners draws the banners of a pane, between its bar and its body.
func (w *workspace) paneBanners(c *ui.Context, t *tab, p *pane, st keelclient.State) {
	switch {
	case st.Status == "error" || st.Status == "exited":
		w.bannerRow(c, "banner failed", flareSoft, flare, failHeadline(st), failEndpoint(p.host, st), st.Message, func() {
			if textBtn(c, "Close", btnGhost) {
				w.act(func() { w.closePane(t, p.id) })
			}
			if p.host.ID != "" && textBtn(c, "Edit host", btnGhost) {
				w.act(func() { w.editPaneHost(p) })
			}
			if textBtn(c, "Reconnect", btnPrimary) {
				w.act(func() { w.reconnect(p) })
			}
		})
	case st.Status == "connecting" && p.host.AuthType != "local":
		w.bannerRow(c, "banner connecting", signalSoft, signal, "Connecting…", failEndpoint(p.host, st), "", func() {})
	case st.Role == "follower" || st.Role == "owner": // anyone who is not driving
		who := "Nobody is driving"
		for _, a := range st.Attached {
			if st.DriverID != 0 && a.ID == st.DriverID && a.Name != "" {
				who = a.Name + " is driving"
			}
		}
		owner := st.OwnerID != 0 && st.OwnerID == st.YouID // only the owner may take control (term.js syncFollowBanner)
		w.bannerRow(c, "banner following", hazeSoft, haze, "Following · "+who+" · read-only", "", "", func() {
			switch {
			case owner && textBtn(c, "Take control", btnDefault):
				w.act(func() { w.sendControl(p.conn.TakeControl, "take control") })
			case !owner && textBtn(c, "Ask for control", btnDefault):
				w.act(func() { w.sendControl(p.conn.RequestControl, "ask for control") })
			}
		})
	}
}

// roleChip is the k-role chip of a shared session's pane bar; it never shrinks.
func roleChip(c *ui.Context, role string) {
	text, bg, fg := "", ui.Transparent, inkMuted
	switch role {
	case "driver":
		text, bg, fg = "Driving", brassSoft, brassInk
	case "owner":
		text = "Owner"
	case "follower":
		text, bg, fg = "Following", hazeSoft, haze
	default:
		return
	}
	r := ui.Row(c).Height(roleChipHeight).Padding(0, space2).Radius(radiusFull).Justify(ui.Center).Shrink(0).
		Background(bg).Label("role " + text)
	if role == "owner" {
		r.Border(1, lineStrong)
	}
	r.Children(func() { ui.Text(c, text).TextColor(fg).FontSize(roleChipSize).FontWeight(weightMedium).MaxLines(1) })
}

// sendControl sends one control frame (take or request control) off the UI thread; the server's answer
// is the role the socket reports next.
func (w *workspace) sendControl(send func() error, what string) {
	w.work(func() {
		if err := send(); err != nil {
			w.post(func() { w.fail(what, err) })
		}
	})
}

// hostKeyDialog is the one place a host key is trusted: a modal for the focused pane's offer. Trust sends
// back exactly what the server offered; Cancel and Escape leave the pane with its failure banner.
func (w *workspace) hostKeyDialog(c *ui.Context) {
	t := w.tab()
	p := w.focusedPane()
	if p == nil {
		return
	}
	k := p.conn.Snapshot().HostKey
	if k.Kind == "" || k == p.declined {
		return
	}
	open := true
	mismatch := k.Kind == "host-key-mismatch"
	title, hint := "Unknown host key", "Trust this key once to connect. Keel will reject a different key later."
	trust := btnPrimary
	if mismatch {
		title = "Host key changed"
		hint = "This machine presented a different key than the one saved in Keel. Only replace it if you reinstalled the server."
		trust = btnDanger
	}
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(scrim)
		panel.Width(dialogWidth).Padding(space3).Gap(space2).Background(raised).Border(1, line).Radius(radiusMd).Label("dialog " + title)
		bodyStrong(c, title, ink)
		ui.Text(c, hint).TextColor(inkMuted).FontSize(captionSize)
		body(c, k.Hostname, ink).Font(monoStack)
		if k.KeyType != "" {
			mono(c, k.KeyType, inkMuted)
		}
		mono(c, k.Fingerprint, inkMuted)
		if mismatch && k.Expected != "" {
			mono(c, "saved: "+k.Expected, inkMuted)
		}
		ui.Row(c).FillWidth().Gap(space2).Justify(ui.End).Children(func() {
			if textBtn(c, "Abort", btnGhost) {
				open = false
			}
			if textBtn(c, "Trust and connect", trust) {
				open = false
				w.act(func() { w.trustHostKey(p, k) })
			}
		})
	})
	if !open {
		p.declined = k
		if t != nil {
			w.wantFocus = t.focused
		}
	}
}
