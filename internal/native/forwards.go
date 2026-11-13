package native

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// Port forwarding (docs/ui-redesign.md §5.10): what the app has, and so what this has. The words and rules are
// ForwardsPage, TunnelList and tunnelRow (internal/ui/pages.templ, lists.templ), tunnelDialog (dialogs.templ), the
// row menu (rowmenus.templ), the sidebar's Forwards section (sidebarlists.templ), the status bar's forwards count
// (page.templ, term.js renderStatusBar) and the handlers in cmd/keel/web/js/term.js. The helpers below are
// internal/ui/data.go's. Each forward opens its own SSH connection from keel serve and keeps running when the window
// closes.
//
// Not in the app and so not here: traffic counters, a per-row Edit shortcut on a failed forward (a failed row has
// Retry; Edit is in the row menu), a "Missing host" repair other than the dialog's own entry. The webview learns of
// a forward's state from its Datastar stream; this client reads GET /api/tunnels after every action, when the vault
// rev changes, and every 2 s while the page or the sidebar's Forwards section shows (forwardsEvery).
//
// A forward's bind address defaults to 127.0.0.1 and the dialog says "Bind 0.0.0.0 exposes the port on every
// interface." A wildcard bind carries the "Every interface" badge. Nothing here binds anything: keel serve does.

const (
	forwardsEvery = 2 * time.Second // how often the page and the sidebar section read GET /api/tunnels while shown
	tunnelAll     = "all"
	tunnelRunning = "running"
	tunnelStopped = "stopped"
)

// forwardsPage is the state of the Port forwarding tab.
type forwardsPage struct {
	filter  string
	show    string // all | running | stopped (the chips)
	filterH ui.Handle
}

func (p *forwardsPage) load(w *workspace)                { w.reloadTunnels() }
func (p *forwardsPage) view(w *workspace, c *ui.Context) { w.forwardsView(c, p) }
func (p *forwardsPage) newItem(w *workspace)             { w.newForward() }

// tunnelStateInfo is internal/ui/data.go tunnelStatus: how a forward's state reads, a dot and a word.
type tunnelStateInfo struct {
	label  string
	dot    string // the palette's dot: live | connecting | dead | idle
	filter string // running | stopped
	on     bool
}

// tunnelState is data.go tunnelState.
func tunnelState(t keelclient.Tunnel) tunnelStateInfo {
	switch t.Status {
	case "ready":
		return tunnelStateInfo{"Running", "live", tunnelRunning, true}
	case "connecting":
		return tunnelStateInfo{"Connecting", "connecting", tunnelRunning, true}
	case "error":
		label := "Failed"
		if t.Error != "" {
			label = "Failed · " + t.Error
		}
		return tunnelStateInfo{label, "dead", tunnelStopped, false}
	}
	return tunnelStateInfo{"Stopped", "idle", tunnelStopped, false}
}

// tunnelFlag is data.go tunnelFlag: the ssh flag a kind maps to.
func tunnelFlag(kind string) string {
	switch kind {
	case "remote":
		return "-R"
	case "dynamic":
		return "-D"
	}
	return "-L"
}

// tunnelDest is data.go tunnelDest.
func tunnelDest(t keelclient.Tunnel) string {
	if t.Kind == "dynamic" {
		return "SOCKS5"
	}
	return fmt.Sprintf("%s:%d", t.DestHost, t.DestPort)
}

// tunnelListen is data.go tunnelListen.
func tunnelListen(t keelclient.Tunnel) string {
	if t.ListenAddr != "" {
		return t.ListenAddr
	}
	if t.BindPort == 0 {
		return t.BindAddr + ":auto"
	}
	return fmt.Sprintf("%s:%d", t.BindAddr, t.BindPort)
}

// tunnelEveryInterface is data.go tunnelEveryInterface: a wildcard bind, not loopback only. These are display
// strings for the badge; nothing here binds.
func tunnelEveryInterface(t keelclient.Tunnel) bool {
	switch strings.TrimSpace(t.BindAddr) {
	case "0.0.0.0", "::", "[::]", "*":
		return true
	}
	return false
}

// tunnelLocalAddress is menus.go tunnelLocalAddress: what "Copy local address" copies, "" when there is none.
func tunnelLocalAddress(t keelclient.Tunnel) string {
	if t.Kind == "remote" {
		if t.DestHost == "" || t.DestPort == 0 {
			return ""
		}
		return fmt.Sprintf("%s:%d", t.DestHost, t.DestPort)
	}
	if t.ListenAddr != "" {
		return t.ListenAddr
	}
	if t.BindPort == 0 {
		return ""
	}
	addr := t.BindAddr
	if addr == "" {
		addr = "127.0.0.1"
	}
	return fmt.Sprintf("%s:%d", addr, t.BindPort)
}

// tunnelHostName is data.go hostNameByID: the host's name, else its id.
func (w *workspace) tunnelHostName(id string) string {
	if h, ok := hostByID(w.hosts, id); ok {
		return h.Name
	}
	return id
}

// tunnelFilterText is what the page's filter reads: the name, the host's name, the listen address and the target.
func (w *workspace) tunnelFilterText(t keelclient.Tunnel) string {
	return t.Name + " " + w.tunnelHostName(t.HostID) + " " + tunnelListen(t) + " " + tunnelDest(t)
}

// shownTunnels is the page's rows: the filter text and the chip.
func (w *workspace) shownTunnels(p *forwardsPage) (out []keelclient.Tunnel) {
	q := strings.ToLower(strings.TrimSpace(p.filter))
	for _, t := range w.tunnels {
		if q != "" && !strings.Contains(strings.ToLower(w.tunnelFilterText(t)), q) {
			continue
		}
		if p.show != tunnelAll && p.show != "" && tunnelState(t).filter != p.show {
			continue
		}
		out = append(out, t)
	}
	return out
}

// tunnelCounts is the chips' counts (syncTunnelPage counts every row).
func (w *workspace) tunnelCounts() (all, running, stopped int) {
	for _, t := range w.tunnels {
		all++
		if tunnelState(t).filter == tunnelRunning {
			running++
		} else {
			stopped++
		}
	}
	return
}

// runningForwards is the status bar's count: forwards that are ready (term.js renderStatusBar).
func (w *workspace) runningForwards() (n int) {
	for _, t := range w.tunnels {
		if t.Status == "ready" {
			n++
		}
	}
	return n
}

// forwardsLabel is the status bar's "1 forward" / "2 forwards".
func forwardsLabel(n int) string {
	if n == 1 {
		return "1 forward"
	}
	return fmt.Sprintf("%d forwards", n)
}

// forwardsShown reports that the page or the sidebar's Forwards section is on screen: the 2 s read runs then.
func (w *workspace) forwardsShown() bool {
	if w.closed || w.gate != nil || w.first != nil {
		return false
	}
	if w.sidebar && w.sideSeg == "forwards" {
		return true
	}
	return pageAs[*forwardsPage](w.tab()) != nil
}

// ---- reading ----

// reloadTunnels reads the forwards again.
func (w *workspace) reloadTunnels() { w.readTunnels(new(bool), false) }

// tunnelTick is one tick of the 2 s read: it reads only while the forwards show and no read is out.
func (w *workspace) tunnelTick() {
	if w.forwardsShown() {
		w.readTunnels(&w.tunReading, true)
	}
}

// readTunnels is the one read of the forwards. busy is the in-flight flag a tick shares; quiet leaves a failed
// read unreported (it is not news: the next tick tries again).
func (w *workspace) readTunnels(busy *bool, quiet bool) {
	callOnce(w, busy, nil, w.be.Tunnels, func(ts []keelclient.Tunnel, err error) {
		switch {
		case err == nil:
			w.tunnels = ts
		case !quiet:
			w.fail("forwards", err)
		}
	})
}

// ---- start, stop, delete ----

// fwdKeyOffer is the host-key prompt of a forward that could not start: the same "Unknown host key" dialog as a
// session's. Only its Trust and connect button sends the key back.
type fwdKeyOffer struct {
	tunnelID string
	key      keelclient.HostKey
}

// setTunnelOn is the switch, Retry and the menu's Start / Stop.
func (w *workspace) setTunnelOn(t keelclient.Tunnel, on bool) {
	callErr(w, func(c context.Context) (err error) {
		if on {
			_, err = w.be.StartTunnel(c, t.ID)
		} else {
			_, err = w.be.StopTunnel(c, t.ID)
		}
		return err
	}, func(err error) {
		if err != nil {
			var se *keelclient.StatusError
			if on && errors.As(err, &se) && se.Offer.Kind != "" { // promptHostKey(err, {tunnelId})
				w.fwdKey = &fwdKeyOffer{tunnelID: t.ID, key: se.Offer}
			} else {
				w.warn(errText("forward", err))
			}
		}
		w.reloadTunnels()
	})
}

// trustForwardKey is Trust and connect: it trusts the key the server offered, starts the forward again and says so.
func (w *workspace) trustForwardKey(o *fwdKeyOffer) {
	w.fwdKey = nil
	callErr(w, func(c context.Context) error {
		err := w.be.TrustHostKey(c, o.key, o.key.Kind == keelclient.HostKeyMismatch)
		if err == nil {
			_, err = w.be.StartTunnel(c, o.tunnelID)
		}
		return err
	}, func(err error) {
		if err != nil {
			w.warn(errText("trust host key", err))
		} else {
			w.say("Host key saved")
		}
		w.reloadTunnels()
	})
}

// askDeleteTunnel is Delete: it asks first (term.js: askConfirm `Delete forward “name”?`).
func (w *workspace) askDeleteTunnel(t keelclient.Tunnel) {
	w.askConfirm(&hostConfirm{title: "Confirm", message: "Delete forward “" + t.Name + "”?", action: "Remove", run: func() { w.deleteTunnel(t) }})
}

func (w *workspace) deleteTunnel(t keelclient.Tunnel) {
	callErr(w, func(c context.Context) error { return w.be.DeleteTunnel(c, t.ID) }, func(err error) {
		if err != nil {
			w.warn(errText("delete forward", err))
		}
		w.reloadTunnels()
	})
}

// ---- the dialog ----

// forwardDialog is New forward / Edit forward (tunnelDialog in dialogs.templ). The values are strings, as the form's.
type forwardDialog struct {
	id                                                string // "" for New forward
	name, hostID, kind                                string
	bindAddr, bindPort, destHost, destPort            string
	err                                               string
	saving, focus                                     bool
	nameH, bindAddrH, bindPortH, destHostH, destPortH ui.Handle
}

// newForward is fillTunnelForm(null): local, 127.0.0.1, port 0, a target of 127.0.0.1:5432.
func (w *workspace) newForward() {
	w.fwdDlg = &forwardDialog{kind: "local", bindAddr: "127.0.0.1", bindPort: "0", destHost: "127.0.0.1", destPort: "5432", focus: true}
}

// editForward is fillTunnelForm(t).
func (w *workspace) editForward(t keelclient.Tunnel) {
	d := &forwardDialog{id: t.ID, name: t.Name, hostID: t.HostID, kind: t.Kind, bindAddr: t.BindAddr,
		bindPort: strconv.Itoa(t.BindPort), destHost: t.DestHost, destPort: strconv.Itoa(t.DestPort), focus: true}
	if t.BindAddr == "" {
		d.bindAddr = "127.0.0.1"
	}
	if t.DestHost == "" {
		d.destHost = "127.0.0.1"
	}
	if t.DestPort == 0 {
		d.destPort = "5432"
	}
	if d.kind == "" {
		d.kind = "local"
	}
	w.fwdDlg = d
}

func (w *workspace) closeForwardDialog() { w.fwdDlg = nil }

// sshHosts is the host picker's list: every host but the local shell (term.js sshHosts).
func (w *workspace) sshHosts() (out []keelclient.Host) {
	for _, h := range w.hosts {
		if !h.IsLocal() {
			out = append(out, h)
		}
	}
	return out
}

// hostPickEntries is fillTunnelHosts: each SSH host with its address, and "Missing host — pick another" for a saved
// host that is gone.
func (w *workspace) hostPickEntries(selected string) (out []pickEntry) {
	found := selected == ""
	for _, h := range w.sshHosts() {
		out = append(out, pickEntry{value: h.ID, label: h.Name, desc: hostSubtitle(h)})
		if h.ID == selected {
			found = true
		}
	}
	if !found {
		out = append([]pickEntry{{value: selected, label: "Missing host — pick another"}}, out...)
	}
	return out
}

// request is what /ui/forms/tunnel makes of the form: a blank bind address is 127.0.0.1, a port that is not a number
// is 0. The kind's own rules (a target for -L and -R, a loopback bind for -D) are the server's.
func (d *forwardDialog) request() keelclient.TunnelInput {
	bind := strings.TrimSpace(d.bindAddr)
	if bind == "" {
		bind = "127.0.0.1"
	}
	n := func(s string) int { v, _ := strconv.Atoi(strings.TrimSpace(s)); return v }
	return keelclient.TunnelInput{ID: d.id, Name: strings.TrimSpace(d.name), HostID: d.hostID, Kind: d.kind, BindAddr: bind,
		BindPort: n(d.bindPort), DestHost: strings.TrimSpace(d.destHost), DestPort: n(d.destPort)}
}

// saveForward is Save forward.
func (w *workspace) saveForward() {
	d := w.fwdDlg
	if d == nil || d.saving {
		return
	}
	if d.hostID == "" {
		d.err = "Pick an SSH host first" // forms.go
		return
	}
	in := d.request()
	d.saving, d.err = true, ""
	callErr(w, func(c context.Context) error { _, err := w.be.SaveTunnel(c, in); return err }, func(err error) {
		d.saving = false
		if err != nil {
			d.err = hostError(err)
			return
		}
		if w.fwdDlg == d {
			w.closeForwardDialog()
		}
		w.say("Forward saved")
		w.reloadTunnels()
	})
}
