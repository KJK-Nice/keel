// Package keelclient is the only code in keel-native that talks to keel serve:
// the JSON API and the /ws/session socket. It never creates the token and never
// puts it in an error.
package keelclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"keel/internal/authtoken"
	"keel/internal/paths"
)

// Client talks to one keel serve.
type Client struct {
	addr, token string
	http        *http.Client
}

// New reads the address (paths.Addr) and the token (authtoken.Read). It does
// not start keel serve and does not create the token.
func New() (*Client, error) {
	tok, err := authtoken.Read(paths.DataDir())
	if err != nil {
		return nil, fmt.Errorf("read the keel serve token (is keel serve running?): %w", err)
	}
	return NewAt(paths.Addr(), tok), nil
}

// NewAt is New with explicit values, for tests.
func NewAt(addr, token string) *Client {
	return &Client{addr: addr, token: token, http: &http.Client{Timeout: 15 * time.Second}}
}

type Vault struct{ Locked, NeedsSetup, FirstRun bool }

type Host struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Group    string `json:"group"`
	Address  string `json:"address"`
	Username string `json:"username"`
	AuthType string `json:"authType"`
	Color    string `json:"color"`
	Port     int    `json:"port"`
}

type Session struct {
	ID       string `json:"id"`
	HostID   string `json:"hostId"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	Color    string `json:"color"`
	Status   string `json:"status"`
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
}

func (c *Client) do(ctx context.Context, method, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, "http://"+c.addr+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set(authtoken.Header, c.token) // no Origin: a native client
	resp, err := c.http.Do(req)
	if err != nil {
		// url.Error carries the URL, which has no secret in it.
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("%s %s: %s", method, path, resp.Status)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Vault is GET /api/vault.
func (c *Client) Vault(ctx context.Context) (v Vault, err error) {
	var raw struct {
		Locked     bool `json:"locked"`
		NeedsSetup bool `json:"needsSetup"`
		FirstRun   bool `json:"firstRun"`
	}
	err = c.do(ctx, http.MethodGet, "/api/vault", &raw)
	return Vault(raw), err
}

// Hosts is GET /api/hosts (secrets are already redacted by the server).
func (c *Client) Hosts(ctx context.Context) (h []Host, err error) {
	err = c.do(ctx, http.MethodGet, "/api/hosts", &h)
	return h, err
}

// Sessions is GET /api/sessions.
func (c *Client) Sessions(ctx context.Context) (s []Session, err error) {
	err = c.do(ctx, http.MethodGet, "/api/sessions", &s)
	return s, err
}

// Kill is DELETE /api/sessions/{id}: it ends that PTY.
func (c *Client) Kill(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/sessions/"+url.PathEscape(id), nil)
}

// Spawn starts a session on a host and attaches to it.
func (c *Client) Spawn(hostID string, cols, rows int) (*Conn, error) {
	return c.dial(url.Values{"hostId": {hostID}}, cols, rows)
}

// Attach attaches to a running session.
func (c *Client) Attach(sessionID string, cols, rows int) (*Conn, error) {
	return c.dial(url.Values{"id": {sessionID}}, cols, rows)
}

// name is who this client says it is, in the order cmd/keel parseName uses.
func name() string {
	for _, k := range []string{"KEEL_NAME", "USER"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return "native"
}

func (c *Client) dial(q url.Values, cols, rows int) (*Conn, error) {
	q.Set("cols", strconv.Itoa(cols))
	q.Set("rows", strconv.Itoa(rows))
	q.Set("name", name())
	q.Set("kind", "web")
	return dialWS("ws://"+c.addr+"/ws/session?"+q.Encode(), c.token)
}
