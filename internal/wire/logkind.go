package wire

// The kind words of keel.log entries, as the log's API and the window's
// filter chips name them. Agent entries also use dotted kinds ("agent.read").
const (
	LogHost     = "host"
	LogVault    = "vault"
	LogGroup    = "group"
	LogIdentity = "identity"
	LogSnippet  = "snippet"
	LogNotebook = "notebook"
	LogTunnel   = "tunnel"
	LogSession  = "session"
	LogShare    = "share"
	LogAgent    = "agent"
)
