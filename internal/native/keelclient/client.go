// Package keelclient is the only code in keel-native that talks to keel serve:
// the JSON API and the /ws/session socket. It never creates the token and never
// puts it in an error.
package keelclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	slow        *http.Client // no timeout of its own: Ask (agent.go)
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
	return &Client{addr: addr, token: token, http: &http.Client{Timeout: 15 * time.Second}, slow: &http.Client{}}
}

// Vault is GET /api/vault. Rev changes with every vault or tunnel change (the webview's stream carries it too).
type Vault struct {
	Locked     bool   `json:"locked"`
	NeedsSetup bool   `json:"needsSetup"`
	FirstRun   bool   `json:"firstRun"`
	KeyringOff bool   `json:"keyringOff"`
	Mode       string `json:"mode"` // "keychain" or a passphrase vault
	Path       string `json:"path"`
	Rev        string `json:"rev"`
	Sessions   int    `json:"sessions"`
	SSHConfig  string `json:"sshConfig"` // the ssh config's display path for First run ("~/.ssh/config"), or ""
}

// Host is a saved host as GET /api/hosts lists it: every field the Edit host dialog shows, and no secret (the
// server redacts them, so a saved password is never in it).
type Host struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Group           string   `json:"group"`
	GroupID         string   `json:"groupId"`
	Address         string   `json:"address"`
	Username        string   `json:"username"`
	AuthType        string   `json:"authType"`
	IdentityID      string   `json:"identityId"`
	Color           string   `json:"color"`
	Port            int      `json:"port"`
	Tags            []string `json:"tags"`
	JumpHost        string   `json:"jumpHost"`
	StartupCommand  string   `json:"startupCommand"`
	KeepAliveSec    int      `json:"keepAliveSec"` // -1 is off, 0 the default
	NoAutoReconnect bool     `json:"noAutoReconnect"`
	AgentReadable   *bool    `json:"agentReadable"`
	Production      *bool    `json:"production"`
	AgentNotes      *bool    `json:"agentNotes"`
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

// do sends one request; body, when not nil, is sent as JSON. An error names the method, path and
// status only, never a body (a body may carry a one-time token).
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	return c.doWith(ctx, c.http, method, path, body, out)
}

// doWith is do over hc: an ask waits for a model, longer than the 15 s of the other calls, and is bounded by its
// context instead.
func (c *Client) doWith(ctx context.Context, hc *http.Client, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://"+c.addr+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set(authtoken.Header, c.token) // a native client sends no origin header
	resp, err := hc.Do(req)
	if err != nil {
		// url.Error carries the URL, which has no secret in it.
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
		return statusError(method, path, resp)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusAccepted {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// StatusError is a refused request. Error names the method, path and status only; Msg is the server's own
// one-line reason ({"error": "..."}), for the user: validation text such as "name is required", never an echo
// of the request.
type StatusError struct {
	Method, Path, Status, Msg string
	Code                      int
	// ErrorKind and Key are what a refused host import adds ({"errorKind": "wrongPassphrase", "key": path}): the
	// kind of the refusal and the path of the key it is about. Neither is a secret.
	ErrorKind, Key string
	// Reason is the machine word of a refusal that has one ({"reason": "history"} on a notebook save or delete).
	Reason string
	// Offer is the host key a refused start of a forward offers (409 unknown-host or host-key-mismatch): what
	// TrustHostKey sends back. Its Token is a one-time capability; HostKey.String leaves it out.
	Offer HostKey
}

func (e *StatusError) Error() string { return fmt.Sprintf("%s %s: %s", e.Method, e.Path, e.Status) }

func statusError(method, path string, resp *http.Response) error {
	e := &StatusError{Method: method, Path: path, Status: resp.Status, Code: resp.StatusCode}
	var body struct {
		Error     string `json:"error"`
		ErrorKind string `json:"errorKind"`
		Key       string `json:"key"`
		Reason    string `json:"reason"`
		Hostname  string `json:"hostname"`
		Finger    string `json:"fingerprint"`
		Expected  string `json:"expected"`
		KeyType   string `json:"keyType"`
		Token     string `json:"trustToken"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body) == nil && len(body.Error) <= 300 {
		e.Msg, e.ErrorKind, e.Key, e.Reason = body.Error, body.ErrorKind, body.Key, body.Reason
		if k := body.ErrorKind; k == "unknown-host" || k == "host-key-mismatch" {
			e.Offer = HostKey{k, body.Hostname, body.Finger, body.Expected, body.KeyType, body.Token}
		}
	}
	return e
}

// Vault is GET /api/vault.
func (c *Client) Vault(ctx context.Context) (v Vault, err error) {
	err = c.do(ctx, http.MethodGet, "/api/vault", nil, &v)
	return v, err
}

// Hosts is GET /api/hosts (secrets are already redacted by the server).
func (c *Client) Hosts(ctx context.Context) (h []Host, err error) {
	err = c.do(ctx, http.MethodGet, "/api/hosts", nil, &h)
	return h, err
}

// NewHost is the body of POST /api/hosts. Password is a secret the user typed: String leaves it out, so that
// printing a NewHost never shows it, and nothing in this package logs a request body.
type NewHost struct {
	Name     string `json:"name"`
	Group    string `json:"group"`
	Address  string `json:"address"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	AuthType string `json:"authType"`
	Password string `json:"password"`
	Color    string `json:"color"`
}

func (h NewHost) String() string {
	return fmt.Sprintf("{%s %s %s:%d %s}", h.Name, h.Group, h.Address, h.Port, h.AuthType)
}

// CreateHost is POST /api/hosts: it saves a host and returns it (secrets redacted, with its id).
func (c *Client) CreateHost(ctx context.Context, h NewHost) (out Host, err error) {
	err = c.do(ctx, http.MethodPost, "/api/hosts", h, &out)
	return out, err
}

// Sessions is GET /api/sessions.
func (c *Client) Sessions(ctx context.Context) (s []Session, err error) {
	err = c.do(ctx, http.MethodGet, "/api/sessions", nil, &s)
	return s, err
}

// Kill is DELETE /api/sessions/{id}: it ends that PTY.
func (c *Client) Kill(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/sessions/"+url.PathEscape(id), nil, nil)
}

// Reconnect is POST /api/sessions/{id}/reconnect: the server starts the session's connection again.
func (c *Client) Reconnect(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/api/sessions/"+url.PathEscape(id)+"/reconnect", nil, nil)
}

// TrustHostKey is POST /api/known-hosts: it trusts the key a connect was just refused for, sending back
// exactly what the server offered (k). replace is true only for a changed key. Call it only from the
// user's click in the host key dialog.
func (c *Client) TrustHostKey(ctx context.Context, k HostKey, replace bool) error {
	return c.do(ctx, http.MethodPost, "/api/known-hosts", map[string]any{
		"token": k.Token, "hostname": k.Hostname, "fingerprint": k.Fingerprint, "replace": replace,
	}, nil)
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
