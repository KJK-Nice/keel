package native

import (
	"sync"
)

// Sync input: while a tab has it on, the keys and the paste typed into one
// of its terminals go to every other terminal pane of that tab this client drives, as well as to the one typed into. It is
// the user's own input fanned out and nothing else: this file is the one place besides the terminal view's own connection
// that calls Write on a session (TestSyncInputIsTheOnlyFanOutAndNothingTypedReachesIt in guard_test.go).
//
// Threads: the terminal calls Write on its input goroutine, not on the UI thread, and the tab's sync flag and pane list are
// UI-thread state. So the UI thread publishes, for each pane, the connections its keys also go to (publishSync, every frame
// and on every change of sync), behind a mutex of its own, and Write only reads that list. Whether a peer is ready and
// driven by this client is read at Write time from the connection's own Snapshot, which is safe on any goroutine.

// syncPeers is the list of connections one pane's keys also go to: empty unless its tab has sync on.
type syncPeers struct {
	mu sync.Mutex
	to []session
}

func (s *syncPeers) set(to []session) {
	s.mu.Lock()
	s.to = to
	s.mu.Unlock()
}

func (s *syncPeers) get() []session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.to
}

// syncConn is what the terminal view is given as its connection (run.go): reads, closing and the size go to the pane's own
// connection, and so does Write, which also fans the same bytes out to the pane's peers.
type syncConn struct {
	own   session
	peers *syncPeers
}

// newSyncConn wraps p's connection for its terminal view. run.go calls it once, in liveBodyT, and nothing else does.
func newSyncConn(p *pane) *syncConn { return &syncConn{own: p.conn, peers: &p.peers} }

func (s *syncConn) Read(b []byte) (int, error)  { return s.own.Read(b) }
func (s *syncConn) Close() error                { return s.own.Close() }
func (s *syncConn) Resize(cols, rows int) error { return s.own.Resize(cols, rows) }

// Write sends b to the pane's own connection exactly as before and then, with sync on, to its peers. The result is the own
// connection's: a peer that fails is skipped.
func (s *syncConn) Write(b []byte) (int, error) {
	n, err := s.own.Write(b)
	fanOut(s.peers.get(), b)
	return n, err
}

// fanOut writes b to each peer that is ready and driven by this client. A failed write is not retried and stops nobody else.
func fanOut(to []session, b []byte) {
	for _, c := range to {
		if !c.Snapshot().CanType() {
			continue
		}
		_, _ = c.Write(b)
	}
}

// canSync is a tab sync input can be turned on in: a session tab of this machine's keel serve with a terminal pane.
func (t *tab) canSync() bool {
	return t != nil && t.root != nil && !t.remote && t.page == nil && t.pg == nil && t.firstTerm() != nil
}

func (w *workspace) toggleSync(t *tab) {
	if !t.canSync() {
		return
	}
	t.sync = !t.sync
	w.publishSync()
}

// publishSync gives each pane the list of panes its keys also go to (the other terminal panes of its tab, in layout order,
// while the tab has sync on and is not joined). UI thread.
func (w *workspace) publishSync() {
	for _, t := range w.tabs {
		for _, p := range t.panes {
			var to []session
			if t.sync && t.canSync() && p.nb == nil && !p.remote {
				for _, id := range t.root.Leaves() {
					if q := t.panes[id]; q != nil && q != p && q.nb == nil && !q.remote {
						to = append(to, q.conn)
					}
				}
			}
			p.peers.set(to)
		}
	}
}
