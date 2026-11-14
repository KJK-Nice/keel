package native

// The typing gate: whether Keel may type into a pane for the user, and whether a press lacks the ⇧ a command needs. Both
// only decide what the window offers; keel serve checks again at the write.

// canType reports a pane Keel may type into: a session of this machine's keel serve that is ready and that this client drives.
func (p *pane) canType() bool { return !p.remote && p.conn.Snapshot().CanType() }

// drives reports an agent pane that is open in this window, local, and driven by this client.
func (a *agentPane) drives() bool { return a != nil && a.p != nil && !a.remote && a.driving }

// shiftMissing reports a press without ⇧ on a command that needs it (keel serve's NeedsShift rule).
func shiftMissing(needsShift, shift bool) bool { return needsShift && !shift }
