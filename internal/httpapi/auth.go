package httpapi

import (
	"net"
	"net/http"
	"strings"

	"keel/internal/authtoken"
)

// tokenParam carries the token on the desktop app's first page load. The
// server trades it for a cookie and redirects to the same URL without it.
const tokenParam = "keel_token"

// WithAuth requires token on the privileged routes (/, /api/, /ws/, /ui/,
// except /api/health). An empty token leaves them open, as in tests.
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
			host = strings.TrimSuffix(strings.TrimPrefix(r.Host, "["), "]")
		}
		if !isLoopbackHost(host) {
			writeError(w, http.StatusForbidden, "host not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func needsAuth(r *http.Request) bool {
	p := r.URL.Path
	if p == "/api/health" {
		return false
	}
	return p == "/" || strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/ws/") || strings.HasPrefix(p, "/ui/")
}

// authCookieName is per port: cookies are scoped by host, not port, so two
// Keel instances on 127.0.0.1 (different KEEL_ADDR) would otherwise
// overwrite each other's cookie in a shared webview cookie store.
func authCookieName(r *http.Request) string {
	_, port, err := net.SplitHostPort(r.Host)
	if err != nil || port == "" {
		port = "80"
	}
	return "keel_auth_" + port
}

func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.authToken == "" || !needsAuth(r) {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodGet {
			if t := r.URL.Query().Get(tokenParam); t != "" {
				if !authtoken.Equal(t, s.authToken) {
					writeError(w, http.StatusUnauthorized, "unauthorized")
					return
				}
				http.SetCookie(w, &http.Cookie{
					Name:     authCookieName(r),
					Value:    s.authToken,
					Path:     "/",
					HttpOnly: true,
					SameSite: http.SameSiteStrictMode,
				})
				u := *r.URL
				q := u.Query()
				q.Del(tokenParam)
				u.RawQuery = q.Encode()
				http.Redirect(w, r, u.RequestURI(), http.StatusSeeOther)
				return
			}
		}
		if authtoken.Equal(r.Header.Get(authtoken.Header), s.authToken) {
			next.ServeHTTP(w, r)
			return
		}
		if c, err := r.Cookie(authCookieName(r)); err == nil && authtoken.Equal(c.Value, s.authToken) {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/" {
			http.Error(w, "Keel: open this window from the Keel app.", http.StatusUnauthorized)
			return
		}
		writeError(w, http.StatusUnauthorized, "unauthorized")
	})
}

// healthProof answers ?nonce= on /api/health with HMAC(token, nonce), so a
// client can check the server holds the token before trusting it with it.
func (s *Server) healthProof(r *http.Request) (string, bool) {
	nonce := r.URL.Query().Get("nonce")
	if s.authToken == "" || nonce == "" || len(nonce) > 128 {
		return "", false
	}
	return authtoken.Proof(s.authToken, nonce), true
}
