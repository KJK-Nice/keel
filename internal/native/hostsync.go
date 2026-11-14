package native

import (
	"context"
	"time"

	"keel/internal/native/keelclient"
)

// Keeping the host list current. The native client has no event stream, so it reads the hosts
// again after every write it makes, and polls GET /api/vault for its rev while the window is open. If keel serve later gets a JSON event stream, switch to it; do not grow
// the poll (docs/native.md).

const pollEvery = 3 * time.Second

// setHosts replaces the host list. The filter, the collapsed groups and the open tabs are not touched; a pane
// takes its host's new name and colour, and a pane whose host was deleted keeps its session and its last known
// host.
func (w *workspace) setHosts(hosts []keelclient.Host) {
	w.hosts = hosts
	for _, t := range w.tabs {
		for _, p := range t.panes {
			if h, ok := hostByID(hosts, p.host.ID); ok {
				p.host = h
			}
		}
	}
}

// hostByID is the host with that id, as the list holds it.
func hostByID(hosts []keelclient.Host, id string) (keelclient.Host, bool) {
	for _, h := range hosts {
		if h.ID == id {
			return h, true
		}
	}
	return keelclient.Host{}, false
}

// reloadHosts reads the hosts again.
func (w *workspace) reloadHosts() {
	type read struct {
		hosts   []keelclient.Host
		folders []keelclient.Group
		ferr    error
	}
	call(w, func(c context.Context) (r read, err error) {
		r.hosts, err = w.be.Hosts(c)
		r.folders, r.ferr = w.be.Groups(c)
		return r, err
	}, func(r read, err error) {
		if err != nil {
			w.fail("hosts", err)
			return
		}
		w.setHosts(r.hosts)
		if r.ferr == nil {
			w.folders = r.folders
		}
	})
}

// reloadSnippets reads the snippets again.
func (w *workspace) reloadSnippets() {
	call(w, w.be.Snippets, func(snippets []keelclient.Snippet, err error) {
		if err != nil {
			w.fail("snippets", err)
			return
		}
		w.snippets = snippets
	})
}

// pollTick is one tick of the poll: it starts a check, unless the window is closed or one is still running.
func (w *workspace) pollTick() {
	if !w.closed {
		w.pollOnce()
	}
}

// pollOnce reads the vault state. A changed rev reads the hosts again, once; a vault that is locked again puts
// the Unlock screen over the window (sessions and tabs stay); an unlocked one (by another window) takes it away.
func (w *workspace) pollOnce() {
	callOnce(w, &w.polling, nil, w.be.Vault, func(v keelclient.Vault, err error) {
		if err != nil { // keel serve not answering is not news: the sessions say so themselves
			return
		}
		switch {
		case v.Locked && !v.NeedsSetup:
			if w.gate == nil {
				w.showUnlock(v)
			} else {
				w.gate.vault = v
			}
		case v.Locked || v.NeedsSetup || v.FirstRun: // First run shows its own screens; nothing else to do
		case w.gate != nil: // unlocked elsewhere
			w.unlocked(w.gate)
			w.rev = v.Rev
		case v.Rev != w.rev && w.live:
			w.rev = v.Rev
			for _, refresh := range w.refreshers() {
				refresh()
			}
		}
	})
}

// refreshers are the reads that run again when the vault's rev changes.
func (w *workspace) refreshers() []func() {
	return []func(){
		w.reloadHosts,
		w.refreshSuggestSettings, // the agent settings may have changed in another window
		w.reloadSnippets,
		w.reloadNotebooks,
		w.reloadTunnels,
		w.refreshPages,
	}
}

// tickUnit is the poll's clock: every ticker's period is a whole number of units.
const tickUnit = 500 * time.Millisecond

// A ticker runs tick on the UI thread every `every`, rounded to tickUnit. The tick decides whether there is
// anything to do (a page shown, a read not out).
type ticker struct {
	every time.Duration
	tick  func()
}

// tickers are the reads that repeat while the window is open.
func (w *workspace) tickers() []ticker {
	return []ticker{
		{pollEvery, w.pollTick},         // the vault's rev
		{forwardsEvery, w.tunnelTick},   // forwards, while the page or the sidebar section shows
		{logsEvery, w.logsTick},         // the Logs page, while it is the selected tab
		{runEvery, w.runTick},           // the run in use, while a typed step waits
		{groupRunEvery, w.groupRunTick}, // a group run proposes and reads results
	}
}

// startPoll runs the tickers until the window closes. every is the vault's period; the others keep their ratio to
// it (tests pass a short one).
func (w *workspace) startPoll(every time.Duration) {
	w.pollStop = make(chan struct{})
	stop := w.pollStop
	ts := w.tickers()
	go func() {
		clock := time.NewTicker(every / time.Duration(pollEvery/tickUnit))
		defer clock.Stop()
		for n := 1; ; n++ {
			select {
			case <-stop:
				return
			case <-clock.C:
				for _, t := range ts {
					if n%int(t.every/tickUnit) == 0 {
						w.post(t.tick)
					}
				}
			}
		}
	}()
}
