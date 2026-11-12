package native

import (
	"strconv"
	"strings"

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
func textBtn(c *ui.Context, name string, kind btnKind) bool { return textBtnAs(c, name, name, kind) }

// textBtnAs is textBtn with a name of its own: several of a kind on one page (a Disconnect on each provider card) need names
// that tell them apart.
func textBtnAs(c *ui.Context, name, shown string, kind btnKind) bool {
	return ctlButton(c, name, shown, kind).Clicked()
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

// connectLog is term.js seedConnectLog followed by what keel serve reported (State.Log): the lines a failed or connecting
// pane lists and Copy logs copies.
func connectLog(h keelclient.Host, st keelclient.State) []keelclient.LogLine {
	var seed []keelclient.LogLine
	if h.AuthType == "" || h.AuthType == "local" {
		seed = []keelclient.LogLine{{Kind: "user", Text: "Starting a local shell"}}
	} else {
		port := h.Port
		if port == 0 {
			port = 22
		}
		addr, p := `"`+h.Address+`"`, `"`+strconv.Itoa(port)+`"`
		seed = []keelclient.LogLine{
			{Kind: "user", Text: "Starting a new connection to: " + addr + " port " + p},
			{Kind: "work", Text: "Starting address resolution of " + addr},
			{Kind: "work", Text: "Address resolution finished"},
			{Kind: "work", Text: "Connecting to " + addr + " port " + p},
		}
	}
	return append(seed, st.Log...)
}

// logText is term.js copyPaneFailLog's text: the lines, one to a line.
func logText(lines []keelclient.LogLine) string {
	var out []string
	for _, l := range lines {
		if l.Text != "" {
			out = append(out, l.Text)
		}
	}
	if len(out) == 0 {
		return "No connection log yet."
	}
	return strings.Join(out, "\n")
}

// connectLogView is .pane-fail-log: over the pane's body while the session is down or connecting, a row per line with its
// glyph (person for what the user asked, gear for a step, a frown for a failure; a failure is in the flare colour).
func (w *workspace) connectLogView(c *ui.Context, p *pane, st keelclient.State) {
	if p.remote || !(st.Status == "error" || st.Status == "exited" || (st.Status == "connecting" && p.host.AuthType != "local")) {
		return
	}
	ui.Column(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Background(w.ground(stage)).Padding(space3).Gap(space2).Label("connection log " + p.id).Children(func() {
		for _, l := range connectLog(p.host, st) {
			glyph, fg, words := "log-gear", inkFaint, inkMuted
			switch l.Kind {
			case "user":
				glyph = "log-user"
			case "fail":
				glyph, fg, words = "log-frown", flare, flare
			}
			ui.Row(c).FillWidth().Gap(space2).AlignItems(ui.Start).Children(func() {
				ui.Column(c).Shrink(0).Padding(1, 0, 0).Children(func() { icon(c, glyph, fg) })
				mono(c, l.Text, words).Grow(1).Shrink(1).MinWidth(0).MaxLines(0)
			})
		}
	})
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
	if p.remote { // a joined tab has its own banner and no host to reconnect or edit
		w.joinBanner(c, t, p, st)
		return
	}
	// The buttons the failure and the connecting banners share, in the app's order (term.js pane-fail-banner): Copy logs,
	// Close and Edit host; Reconnect follows on a failure only.
	common := func() {
		if textBtn(c, "Copy logs", btnGhost) {
			text := logText(connectLog(p.host, st))
			w.act(func() { c.WriteClipboard(text); w.say("Copied connection log") })
		}
		if textBtn(c, "Close", btnGhost) {
			w.act(func() { w.closePane(t, p.id) })
		}
		if p.host.ID != "" && textBtn(c, "Edit host", btnGhost) {
			w.act(func() { w.editPaneHost(p) })
		}
	}
	switch {
	case st.Status == "error" || st.Status == "exited":
		w.bannerRow(c, "banner failed", flareSoft, flare, failHeadline(st), failEndpoint(p.host, st), st.Message, func() {
			common()
			if textBtn(c, "Reconnect", btnPrimary) {
				w.act(func() { w.reconnect(p) })
			}
		})
	case st.Status == "connecting" && p.host.AuthType != "local":
		w.bannerRow(c, "banner connecting", signalSoft, signal, "Connecting…", failEndpoint(p.host, st), "", common)
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

// chip is a k-role chip with its own words: the driving one, or the quiet one with a ring (the owner's).
func chip(c *ui.Context, text string, driving bool) {
	bg, fg := ui.Transparent, inkMuted
	if driving {
		bg, fg = accentSoft, accentInk
	}
	r := roleChipBox(c, "role "+text, bg)
	if !driving {
		r.Border(1, lineStrong)
	}
	r.Children(func() { roleChipFace(c, "steering", text, fg) })
}

// roleChipBox is the k-role pill: 0 8 0 6 padding, a gap of 4 between the glyph and the words (keel.css .k-role).
func roleChipBox(c *ui.Context, name string, bg ui.Color) ui.Element {
	return ui.Row(c).Height(roleChipHeight).Padding(0, space2, 0, space1_5).Gap(space1).Radius(radiusFull).Justify(ui.Center).
		AlignItems(ui.Center).Shrink(0).Background(bg).Label(name)
}

// roleChipFace is the glyph and the words of a role chip: steering for who drives and owns, eye for a follower
// (lib/share.js ROLE_GLYPH), at 0.923rem.
func roleChipFace(c *ui.Context, glyph, words string, fg ui.Color) {
	iconSized(c, glyph, fg, iconSizeSm).Label("role glyph " + glyph)
	ui.Text(c, words).TextColor(fg).FontSize(roleChipSize).FontWeight(weightMedium).MaxLines(1)
}

// chipFollow is the k-role--follower chip.
func chipFollow(c *ui.Context, text string) {
	roleChipBox(c, "role "+text, hazeSoft).Children(func() { roleChipFace(c, "eye", text, haze) })
}

// roleChip is the k-role chip of a shared session's pane bar; it never shrinks.
func roleChip(c *ui.Context, role string) {
	text, glyph, bg, fg := "", "steering", ui.Transparent, inkMuted
	switch role {
	case "driver":
		text, bg, fg = "Driving", accentSoft, accentInk
	case "owner":
		text = "Owner"
	case "follower":
		text, glyph, bg, fg = "Following", "eye", hazeSoft, haze
	default:
		return
	}
	r := roleChipBox(c, "role "+text, bg)
	if role == "owner" {
		r.Border(1, lineStrong)
	}
	r.Children(func() { roleChipFace(c, glyph, text, fg) })
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
	if hostKeyModal(c, k, func() { w.act(func() { w.trustHostKey(p, k) }) }) {
		p.declined = k
		if t != nil {
			w.wantFocus = t.focused
		}
	}
}

// hostKeyModal is #hostkey-modal: the offered key and Abort / Trust and connect (Replace for a changed key). It
// reports that the dialog went away; onTrust runs when Trust and connect was clicked, and only then.
func hostKeyModal(c *ui.Context, k keelclient.HostKey, onTrust func()) (closed bool) {
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
		modalChrome(c, backdrop, panel)
		panel.Width(dialogWidth).Padding(space3).Gap(space2).Label("dialog " + title)
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
				onTrust()
			}
		})
	})
	return !open
}
