package httpapi

import (
	"net"
	"net/http"
	"strings"

	"keel/internal/authtoken"
	"keel/internal/store"
)

// WithAuth sets the token required, in the X-Keel-Token header, on the
// privileged routes (/api/ and /ws/, except /api/health). There is no open
// server: Handler refuses to build one without a token.
func (s *Server) WithAuth(token string) *Server {
	s.authToken = token
	return s
}

// withLoopbackHost rejects requests whose Host is not a loopback name. A
// DNS-rebound page (attacker.example resolved to 127.0.0.1) reaches the
// server with its own hostname in Host, so it is refused before any handler
// runs, including the static files and /api/health.
func withLoopbackHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host // no port; IsLoopbackAddr takes a bracketed IPv6 address
		}
		if !store.IsLoopbackAddr(host) {
			writeError(w, http.StatusForbidden, "host not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// apiPath reports whether p is under the API (/api/ or /ws/). Any other
// path has no route and answers 404, with or without a token, an Origin or
// an unlocked vault: the daemon serves nothing else.
func apiPath(p string) bool {
	return strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/ws/")
}

func needsAuth(r *http.Request) bool {
	return apiPath(r.URL.Path) && r.URL.Path != "/api/health"
}

// WithMCPAuth sets keel mcp's token (authtoken.LoadOrCreateMCP). /api/mcp/
// answers only requests that carry it in authtoken.MCPHeader, and with no
// token set /api/mcp/ answers nothing.
func (s *Server) WithMCPAuth(token string) *Server {
	s.mcpToken = token
	return s
}

// isMCPPath reports whether p is one of keel mcp's routes (mcp.go).
func isMCPPath(p string) bool {
	return strings.HasPrefix(p, "/api/mcp/")
}

// mcpAuthorized checks a request to /api/mcp/: keel mcp's token in its own
// header, and no Origin (keel mcp is a native client; a page always sends
// one on a POST). The UI's token and cookie do not open these routes, and
// keel mcp's token opens no other route, so neither can stand in for the
// other.
func (s *Server) mcpAuthorized(r *http.Request) bool {
	return r.Header.Get("Origin") == "" && authtoken.Equal(r.Header.Get(authtoken.MCPHeader), s.mcpToken)
}

func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isMCPPath(r.URL.Path) {
			if s.mcpToken == "" || r.Header.Get("Origin") != "" {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			// The token comes first, so a caller without it learns nothing,
			// not even whether Claude Code is on or the vault is locked.
			// Then the user's switch: while Claude Code is off nothing
			// under /api/mcp/ reads a pane or writes the log.
			if !s.mcpAuthorized(r) {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			if !s.mcpAllowed(w) {
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if !needsAuth(r) {
			next.ServeHTTP(w, r)
			return
		}
		if authtoken.Equal(r.Header.Get(authtoken.Header), s.authToken) {
			next.ServeHTTP(w, r)
			return
		}
		writeError(w, http.StatusUnauthorized, "unauthorized")
	})
}

// errClaudeCodeOff is what keel mcp passes on to Claude Code while the
// user has not turned Claude Code on in Settings · Agent.
const errClaudeCodeOff = `Claude Code is off in Keel: turn on "Allow Claude Code (keel mcp)" in Settings · Agent`

// mcpAllowed checks the user's "Allow Claude Code (keel mcp)" switch
// (store.AgentSettings.ClaudeCode, off by default) and answers the request
// itself when /api/mcp/ is closed: 403 while off, 423 while the vault is
// locked (the setting cannot be read, so it counts as off).
func (s *Server) mcpAllowed(w http.ResponseWriter) bool {
	set, err := s.store.AgentSettings()
	if err != nil {
		writeErr(w, err)
		return false
	}
	if !set.ClaudeCodeEnabled() {
		writeError(w, http.StatusForbidden, errClaudeCodeOff)
		return false
	}
	return true
}

// mcpHealthProof answers ?mcpNonce= on /api/health with HMAC(mcp token,
// nonce), so keel mcp can check the server holds its token before sending
// it. It answers while Claude Code is off too: the proof shows nothing but
// that this server holds the token, and keel mcp then gets the clear
// "Claude Code is off" refusal from /api/mcp/ instead of "the server did
// not prove it".
func (s *Server) mcpHealthProof(r *http.Request) (string, bool) {
	nonce := r.URL.Query().Get(authtoken.MCPChallenge.Param)
	if s.mcpToken == "" || nonce == "" || len(nonce) > 128 {
		return "", false
	}
	return authtoken.Proof(s.mcpToken, nonce), true
}

// healthProof answers ?nonce= on /api/health with HMAC(token, nonce), so a
// client can check the server holds the token before trusting it with it.
func (s *Server) healthProof(r *http.Request) (string, bool) {
	nonce := r.URL.Query().Get(authtoken.WindowChallenge.Param)
	if nonce == "" || len(nonce) > 128 {
		return "", false
	}
	return authtoken.Proof(s.authToken, nonce), true
}
