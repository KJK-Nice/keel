package native

// The typing gate: whether Keel may type into a pane for the user. It only decides what the window offers; keel serve
// checks again at the write.

// canType reports a pane Keel may type into: a session of this machine's keel serve that is ready and that this client drives.
func (p *pane) canType() bool { return !p.remote && p.conn.Snapshot().CanType() }

// drives reports an agent pane that is open in this window, local, and driven by this client.
func (a *agentPane) drives() bool { return a != nil && a.p != nil && !a.remote && a.driving }
