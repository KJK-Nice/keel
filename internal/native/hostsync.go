package native

import (
	"time"

	"keel/internal/native/keelclient"
)

// Keeping the host list current. The webview learns of vault changes from the Datastar stream (/ui/stream); the
// native client has no stream, so it reads the hosts again after every write it makes, and polls GET /api/vault
// for its rev while the window is open. If keel serve later gets a JSON event stream, switch to it; do not grow
// the poll (docs/native.md).

const pollEvery = 3 * time.Second

// setHosts replaces the host list. The filter, the collapsed groups and the open tabs are not touched; a pane
// takes its host's new name and colour, and a pane whose host was deleted keeps its session and its last known
// host (the webview's panes do the same: they look the host up by id and fall back to the session's title).
func (w *workspace) setHosts(hosts []keelclient.Host) {
	w.hosts = hosts
	for _, t := range w.tabs {
		for _, p := range t.panes {
			for _, h := range hosts {
				if h.ID == p.host.ID {
					p.host = h
				}
			}
		}
	}
}

// reloadHosts reads the hosts again.
func (w *workspace) reloadHosts() {
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		hosts, err := w.be.Hosts(c)
		folders, ferr := w.be.Groups(c)
		w.post(func() {
			switch {
			case w.closed:
			case err != nil:
				w.fail("hosts", err)
			default:
				w.setHosts(hosts)
				if ferr == nil {
					w.folders = folders
				}
			}
		})
	})
}

// reloadSnippets reads the snippets again.
func (w *workspace) reloadSnippets() {
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		snippets, err := w.be.Snippets(c)
		w.post(func() {
			switch {
			case w.closed:
			case err != nil:
				w.fail("snippets", err)
			default:
				w.snippets = snippets
			}
		})
	})
}

// pollTick is one tick of the poll: it starts a check, unless the window is closed or one is still running.
func (w *workspace) pollTick() {
	if w.closed || w.polling {
		return
	}
	w.polling = true
	w.work(w.pollOnce)
}

// pollOnce reads the vault state. A changed rev reads the hosts again, once; a vault that is locked again puts
// the Unlock screen over the window (sessions and tabs stay); an unlocked one (by another window) takes it away.
func (w *workspace) pollOnce() {
	c, cancel := ctx()
	defer cancel()
	v, err := w.be.Vault(c)
	w.post(func() {
		w.polling = false
		if w.closed || err != nil { // keel serve not answering is not news: the sessions say so themselves
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
			w.reloadHosts()
			w.reloadSnippets()
			w.reloadNotebooks()
			w.reloadTunnels()
			w.refreshPages()
		}
	})
}

// startPoll runs the poll until the window closes. post runs a tick on the UI thread.
func (w *workspace) startPoll(every time.Duration) {
	w.pollStop = make(chan struct{})
	stop := w.pollStop
	go func() {
		t := time.NewTicker(every)
		f := time.NewTicker(time.Duration(float64(every) * float64(forwardsEvery) / float64(pollEvery)))  // the forwards read at forwardsEvery while pollEvery is the vault's
		rb := time.NewTicker(time.Duration(float64(every) * float64(runEvery) / float64(pollEvery)))      // the run in use is read at runEvery while a typed step waits (runbooks.go)
		gr := time.NewTicker(time.Duration(float64(every) * float64(groupRunEvery) / float64(pollEvery))) // a group run asks keel serve to propose and to read results at groupRunEvery (grouprun.go)
		defer t.Stop()
		defer f.Stop()
		defer rb.Stop()
		defer gr.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				w.post(w.pollTick)
			case <-f.C:
				w.post(w.tunnelTick)
			case <-rb.C:
				w.post(w.runTick)
			case <-gr.C:
				w.post(w.groupRunTick)
			}
		}
	}()
}
