package wire

// A tunnel's Kind.
const (
	TunnelLocal   = "local"
	TunnelRemote  = "remote"
	TunnelDynamic = "dynamic"
)

// A tunnel's live Status.
const (
	TunnelStopped    = "stopped"
	TunnelConnecting = "connecting"
	TunnelReady      = "ready"
	TunnelError      = "error"
)
