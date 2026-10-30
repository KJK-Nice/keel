package desktop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"keel/internal/authtoken"
)

// runtimeStatus is what the menu bar item shows of keel serve.
type runtimeStatus struct {
	running  bool
	locked   bool
	sessions int
	// claudeCode is Settings · Agent → Allow Claude Code (keel mcp); known
	// is false while it cannot be read (locked, down, or a server that did
	// not prove it holds the token).
	claudeCode bool
	known      bool
}

// serveClient reads keel serve for the menu bar item. /api/health needs no
// token; the Claude Code switch does, and the token is sent only after the
// server proves it holds it (HMAC of a fresh nonce, as app.VerifyServe), so
// another program that took the port after keel serve stopped never sees
// it.
type serveClient struct {
	base  string // http://127.0.0.1:port
	token string
	http  *http.Client
}

// newServeClient takes the page URL the app opens (desktopURL's input),
// which carries keel serve's address and the UI token.
func newServeClient(pageURL string) *serveClient {
	u, err := url.Parse(pageURL)
	if err != nil || u.Host == "" {
		return nil
	}
	token := u.Query().Get("keel_token")
	if token == "" {
		return nil
	}
	return &serveClient{
		base:  u.Scheme + "://" + u.Host,
		token: token,
		http: &http.Client{
			Timeout: 2 * time.Second,
			// Never follow a redirect with the token on it.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

type healthBody struct {
	Sessions int    `json:"sessions"`
	Locked   bool   `json:"locked"`
	Proof    string `json:"proof"`
}

// health reads /api/health with a fresh nonce; proven is whether the server
// answered it with this token's proof.
func (c *serveClient) health() (h healthBody, proven bool, err error) {
	nonce, err := authtoken.NewNonce()
	if err != nil {
		return h, false, err
	}
	res, err := c.http.Get(c.base + "/api/health?nonce=" + url.QueryEscape(nonce))
	if err != nil {
		return h, false, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return h, false, fmt.Errorf("health: HTTP %d", res.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&h); err != nil {
		return h, false, err
	}
	return h, authtoken.CheckProof(c.token, nonce, h.Proof), nil
}

func (c *serveClient) status() runtimeStatus {
	h, proven, err := c.health()
	if err != nil {
		return runtimeStatus{}
	}
	s := runtimeStatus{running: true, locked: h.Locked, sessions: h.Sessions}
	if h.Locked || !proven {
		return s
	}
	var body struct {
		Settings struct {
			ClaudeCode bool `json:"claudeCode"`
		} `json:"settings"`
	}
	if err := c.do(http.MethodGet, "/api/agent/settings", nil, &body); err != nil {
		return s
	}
	s.claudeCode, s.known = body.Settings.ClaudeCode, true
	return s
}

// setClaudeCode turns Allow Claude Code (keel mcp) on or off, as the
// Settings · Agent switch does.
func (c *serveClient) setClaudeCode(on bool) error {
	if _, proven, err := c.health(); err != nil {
		return err
	} else if !proven {
		return fmt.Errorf("keel serve did not prove it holds the token")
	}
	b, _ := json.Marshal(map[string]bool{"claudeCode": on})
	return c.do(http.MethodPut, "/api/agent/settings", b, nil)
}

func (c *serveClient) do(method, path string, body []byte, out any) error {
	req, err := http.NewRequest(method, c.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set(authtoken.Header, c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s %s: HTTP %d", method, path, res.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
}

// statusLabel is the menu's first line, worded like the status bar
// ("keel serve · 2 sessions").
func statusLabel(s runtimeStatus) string {
	switch {
	case !s.running:
		return "keel serve is not running"
	case s.locked:
		return "keel serve · vault locked"
	case s.sessions == 1:
		return "keel serve · 1 session"
	default:
		return fmt.Sprintf("keel serve · %d sessions", s.sessions)
	}
}
