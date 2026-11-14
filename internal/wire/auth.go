package wire

// The AuthType values of a host. AuthLocal is the local shell host: a shell on this machine, no SSH. An identity's
// Kind is AuthPassword or AuthKey.
const (
	AuthLocal    = "local"
	AuthPassword = "password"
	AuthKey      = "key"
	AuthIdentity = "identity"
)
