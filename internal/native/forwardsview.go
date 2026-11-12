package native

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The Port forwarding page, the sidebar's Forwards section and the forward dialog, drawn as ForwardsPage,
// tunnelRow, SidebarForwards and tunnelDialog (internal/ui) with the Logs page's table layout (logsview.go).

const (
	fwdTypeCol    = 56
	fwdThroughCol = 140
	fwdStatusCol  = 200
	fwdOnCol      = 56
)

var fwdChips = [][2]string{{tunnelAll, "All"}, {tunnelRunning, "Running"}, {tunnelStopped, "Stopped"}}

// tunnelMenu is tunnelRowMenu: Start or Stop, Copy local address, Edit, Delete.
func (w *workspace) tunnelMenu(c *ui.Context, t keelclient.Tunnel) func(m *ui.Menu) {
	return func(m *ui.Menu) {
		if tunnelState(t).on {
			if menuItem(m, "Stop").Chosen() {
				w.act(func() { w.setTunnelOn(t, false) })
			}
		} else if menuItem(m, "Start").Chosen() {
			w.act(func() { w.setTunnelOn(t, true) })
		}
		if addr := tunnelLocalAddress(t); addr != "" {
			if menuItem(m, "Copy local address").Chosen() {
				w.act(func() { c.WriteClipboard(addr); w.say("Copied local address") })
			}
		}
		m.Separator()
		if menuItem(m, "Edit").Chosen() {
			w.act(func() { w.editForward(t) })
		}
		m.Separator()
		if menuItem(m, "Delete").Chosen() {
			w.act(func() { w.askDeleteTunnel(t) })
		}
	}
}

// tunnelSwitch is the k-switch of a row: on while the forward runs or connects. A click starts or stops it; the
// state shown is the server's, which the next read confirms.
func (w *workspace) tunnelSwitch(c *ui.Context, t keelclient.Tunnel) {
	on := tunnelState(t).on
	sw := on
	ui.Switch(c, &sw).Label(t.Name + " on").Shrink(0)
	if sw != on {
		w.act(func() { w.setTunnelOn(t, sw) })
	}
}

// everyInterface is the signal badge of a wildcard bind.
func everyInterface(c *ui.Context) {
	ui.Row(c).Height(space4).Padding(0, space1_5).Gap(space1).Radius(radiusXs).Justify(ui.Center).Shrink(0).Background(signalSoft).
		Label("badge Every interface").Children(func() {
		iconSized(c, "alert", signal, iconSizeSm)
		text(c, "Every interface", signal, kbdSize, weightSemibold)
	})
}

func (w *workspace) forwardsView(c *ui.Context, p *forwardsPage) {
	ui.Column(c).Grow(1).FillWidth().FillHeight().Background(w.ground(surface)).Children(func() {
		pageHead(c, "Port forwarding", "Each forward opens its own SSH connection. It keeps running when you close the window.", func() {
			if textBtnIcon(c, "add-01", "New forward", btnPrimary) { // k-btn--primary in the reference
				w.act(w.newForward)
			}
		})
		all, running, stopped := w.tunnelCounts()
		counts := map[string]int{tunnelAll: all, tunnelRunning: running, tunnelStopped: stopped}
		ui.Row(c).FillWidth().Padding(0, space8, space3).Gap(space2).Wrap().Shrink(0).Children(func() {
			searchField(c, &p.filter, "Filter forwards", &p.filterH).Width(sizeSidebar)
			ui.Row(c).Gap(space1_5).Label("Show forwards").Children(func() {
				for _, ch := range fwdChips {
					on := p.show == ch[0] || (p.show == "" && ch[0] == tunnelAll)
					name := ch[1] + " " + strconv.Itoa(counts[ch[0]])
					r := ui.Row(c).Height(sizeControlSm).Padding(0, space3).Gap(space1).Radius(radiusFull).Justify(ui.Center).Shrink(0).Label(name).Cursor(ui.CursorPointer).Checked(on)
					col := inkMuted
					switch {
					case on:
						r.Background(hoverGround).Border(1, lineStrong)
						col = ink
					default:
						r.Border(1, line)
						if r.Hovered() {
							col = ink
						}
					}
					r.Children(func() { label(c, name, col) })
					if r.Clicked() {
						v := ch[0]
						w.act(func() { p.show = v })
					}
				}
			})
		})
		hairline(ui.Row(c).FillWidth().Height(sizePanebar).Padding(0, space6, 0, space8).Gap(space3).Shrink(0), 0, 0, 1, 0).Children(func() {
			ui.Row(c).Grow(1).Shrink(1).MinWidth(0).Children(func() { label(c, "Name", inkFaint) })
			ui.Row(c).Width(fwdTypeCol).Shrink(0).Children(func() { label(c, "Type", inkFaint) })
			ui.Row(c).Grow(2).Shrink(1).MinWidth(0).Children(func() { label(c, "Route", inkFaint) })
			ui.Row(c).Width(fwdThroughCol).Shrink(0).Children(func() { label(c, "Through", inkFaint) })
			ui.Row(c).Width(fwdStatusCol).Shrink(0).Children(func() { label(c, "Status", inkFaint) })
			ui.Row(c).Width(fwdOnCol).Shrink(0).Justify(ui.End).Children(func() { label(c, "On", inkFaint) })
		})
		shown := w.shownTunnels(p)
		switch {
		case len(w.tunnels) == 0 && w.live:
			if pr, _ := emptyPage(c, emptyNone("forwards", "No forwards yet", forwardsNone, "New forward")); pr {
				w.act(w.newForward)
			}
		case len(shown) == 0 && w.live:
			if pr, _ := emptyPage(c, emptyNoMatch("search-01", orElse(p.filter, p.show))); pr {
				w.act(func() { p.filter, p.show = "", tunnelAll })
			}
		default:
			ui.Scroll(c).FillWidth().Grow(1).Shrink(1).Label("forward list").Children(func() {
				for _, t := range shown {
					w.forwardRow(c, t)
				}
			})
		}
	})
}

// sideMatches is term.js sideMatches: every sidebar filter reads a text for the query, any case; blank keeps all.
func sideMatches(text, q string) bool {
	q = strings.ToLower(strings.TrimSpace(q))
	return q == "" || strings.Contains(strings.ToLower(text), q)
}

// forwardRow is tunnelRow: the name, the -L / -R / -D badge, the route (and the Every interface badge), the host it
// goes through, the state (with Retry after a failure), the switch and the row menu.
func (w *workspace) forwardRow(c *ui.Context, t keelclient.Tunnel) {
	st := tunnelState(t)
	r := hairline(ui.Row(c).FillWidth().MinHeight(sizeControlLg).Padding(space1, space6, space1, space8).Gap(space3).AlignItems(ui.Center).Key(t.ID).Label("forward "+t.Name), 0, 0, 1, 0)
	if r.Hovered() {
		r.Background(hoverGround)
	}
	r.PopoverMenu(w.tunnelMenu(c, t))
	r.Children(func() {
		ui.Row(c).Grow(1).Shrink(1).MinWidth(0).Children(func() { bodyStrong(c, t.Name, ink).MinWidth(0) })
		ui.Row(c).Width(fwdTypeCol).Shrink(0).Children(func() { neutralBadge(c, tunnelFlag(t.Kind)) })
		ui.Row(c).Grow(2).Shrink(1).MinWidth(0).Gap(space2).AlignItems(ui.Center).Wrap().Children(func() {
			mono(c, tunnelListen(t)+" → "+tunnelDest(t), ink).MinWidth(0)
			if tunnelEveryInterface(t) {
				everyInterface(c)
			}
		})
		ui.Row(c).Width(fwdThroughCol).Shrink(0).Children(func() { caption(c, w.tunnelHostName(t.HostID), inkMuted).MinWidth(0) })
		ui.Row(c).Width(fwdStatusCol).Shrink(0).Gap(space2).AlignItems(ui.Center).Children(func() {
			stateDot(c, st.dot)
			ui.Text(c, st.label).TextColor(ink).FontSize(captionSize).Grow(1).Shrink(1).MinWidth(0)
			if t.Status == "error" && textBtn(c, "Retry", btnGhost) {
				w.act(func() { w.setTunnelOn(t, true) })
			}
		})
		ui.Row(c).Width(fwdOnCol).Shrink(0).Justify(ui.End).Children(func() { w.tunnelSwitch(c, t) })
	})
}

// ---- the sidebar's Forwards section ----

// shownSideTunnels is the section's rows under its filter (name, host name, destination host).
func (w *workspace) shownSideTunnels() (out []keelclient.Tunnel) {
	for _, t := range w.tunnels {
		if sideMatches(t.Name+" "+w.tunnelHostName(t.HostID)+" "+t.DestHost, w.fwdQuery) {
			out = append(out, t)
		}
	}
	return out
}

// sidebarForwards is SidebarForwards: a row per forward (dot, name, route, switch, menu) and the two links.
func (w *workspace) sidebarForwards(c *ui.Context) {
	ui.Column(c).FillWidth().Gap(space0_5).Children(func() {
		shown := w.shownSideTunnels()
		for _, t := range shown {
			w.sideForwardRow(c, t)
		}
		switch {
		case len(w.tunnels) == 0 && w.live:
			emptySide(c, emptyNone("forwards", "No forwards yet", forwardsNone, "")) // New forward is the button below
		case len(shown) == 0 && w.live:
			if emptySide(c, emptyNoMatch("search-01", w.fwdQuery)) {
				w.act(func() { w.fwdQuery = "" })
			}
		}
		if textBtn(c, "New forward", btnGhost) {
			w.act(w.newForward)
		}
		if textBtn(c, "Manage forwards", btnGhost) {
			w.act(func() { w.openPage("forwards") })
		}
	})
}

func (w *workspace) sideForwardRow(c *ui.Context, t keelclient.Tunnel) {
	st := tunnelState(t)
	// k-side-row: the item that opens the page, then the switch and the menu beside it (they are not inside the item).
	r := ui.Row(c).FillWidth().Height(sizeRow).Padding(0, space2).Gap(space2).Radius(radiusSm).Key(t.ID)
	hover := r.Hovered()
	if hover {
		r.Background(hoverGround)
	}
	r.PopoverMenu(w.tunnelMenu(c, t))
	r.Children(func() {
		item := ui.Row(c).Grow(1).Shrink(1).MinWidth(0).FillHeight().Gap(space2).Cursor(ui.CursorPointer).Focusable().
			Label("sidebar forward " + t.Name).Tooltip(st.label)
		item.Children(func() {
			stateDot(c, st.dot)
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space0_5).Justify(ui.Center).Children(func() {
				body(c, t.Name, ink).MinWidth(0)
				text(c, tunnelListen(t)+" → "+tunnelDest(t), inkMuted, itemSub, weightRegular).Font(monoStack).MinWidth(0)
			})
		})
		if item.Clicked() {
			w.act(func() { w.openPage("forwards") }) // data-open-page="forwards"
		}
		w.tunnelSwitch(c, t)
	})
}

// ---- the dialog ----

var fwdKinds = [][2]string{{"local", "Local (−L)"}, {"remote", "Remote (−R)"}, {"dynamic", "SOCKS5 (−D)"}}

func (w *workspace) forwardDialogView(c *ui.Context) {
	d := w.fwdDlg
	if d == nil || w.confirm != nil || w.fwdKey != nil {
		return
	}
	title := "New forward"
	if d.id != "" {
		title = "Edit forward"
	}
	open := true
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		modalChrome(c, backdrop, panel)
		panel.Width(dialogWidth).Padding(space4).Gap(space3).Label("dialog " + title)
		panel.ObserveInput(func(ev ui.InputEvent) { // ⌘↵ saves from any field
			if ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyEnter && ev.Mods == ui.Cmd {
				w.act(w.saveForward)
			}
		})
		text(c, title, ink, titleSize, weightSemibold)
		labeled(c, "Name", &d.nameH, func() { inputBox(c, "Forward name", &d.name, "db local", &d.nameH, false, false, 0, nil) })
		labeled(c, "Host", nil, func() {
			entries := w.hostPickEntries(d.hostID)
			if len(entries) == 0 {
				entries = []pickEntry{{value: "", label: "Add an SSH host first"}}
			}
			picker(c, "Forward host", d.hostID, entries, func(v string) { w.act(func() { d.hostID = v }) })
		})
		labeled(c, "Kind", nil, func() {
			segControl(c, "Forward kind", d.kind, fwdKinds, func(v string) { w.act(func() { d.kind = v }) })
		})
		ui.Row(c).FillWidth().Gap(space3).Children(func() {
			ui.Column(c).Grow(1).Basis(0).Children(func() {
				labeled(c, "Bind address", &d.bindAddrH, func() { inputBox(c, "Bind address", &d.bindAddr, "", &d.bindAddrH, true, false, 0, nil) })
			})
			ui.Column(c).Grow(1).Basis(0).Children(func() {
				labeled(c, "Bind port", &d.bindPortH, func() { inputBox(c, "Bind port", &d.bindPort, "0", &d.bindPortH, true, false, 5, nil) })
			})
		})
		if d.kind != "dynamic" { // data-tunnel-dest is hidden for SOCKS5
			ui.Row(c).FillWidth().Gap(space3).Children(func() {
				ui.Column(c).Grow(1).Basis(0).Children(func() {
					labeled(c, "Dest host", &d.destHostH, func() { inputBox(c, "Dest host", &d.destHost, "127.0.0.1", &d.destHostH, true, false, 0, nil) })
				})
				ui.Column(c).Grow(1).Basis(0).Children(func() {
					labeled(c, "Dest port", &d.destPortH, func() { inputBox(c, "Dest port", &d.destPort, "5432", &d.destPortH, true, false, 5, nil) })
				})
			})
		}
		help(c, "Bind 0.0.0.0 exposes the port on every interface.")
		if d.err != "" {
			ui.Row(c).FillWidth().Label("forward form error").Children(func() { caption(c, d.err, flare) })
		}
		ui.Row(c).FillWidth().Gap(space2).Justify(ui.End).Children(func() {
			if textBtn(c, "Cancel", btnGhost) {
				open = false
			}
			busyMark(c, d.saving, "Saving")
			if textBtn(c, "Save forward", btnPrimary) {
				w.act(w.saveForward)
			}
		})
	})
	if d.focus && w.fwdDlg == d {
		d.nameH.Focus(c)
		d.focus = false
	}
	if !open {
		w.act(w.closeForwardDialog)
	}
}

// forwardKeyView is the "Unknown host key" dialog of a forward that could not start (promptHostKey with a tunnel id).
func (w *workspace) forwardKeyView(c *ui.Context) {
	o := w.fwdKey
	if o == nil {
		return
	}
	if hostKeyModal(c, o.key, func() { w.act(func() { w.trustForwardKey(o) }) }) {
		w.act(func() {
			if w.fwdKey == o {
				w.fwdKey = nil
			}
		})
	}
}
