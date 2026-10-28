package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"keel/internal/authtoken"
)

// Client asks keel serve's /api/mcp/ routes on loopback with keel mcp's
// token. It reads the token when it first needs it (keel serve makes it on
// start), and checks that the server on the address holds the same token
// before it sends it: a program that took the port gets a nonce, never the
// token.
type Client struct {
	addr    string
	dataDir string
	http    *http.Client

	mu       sync.Mutex
	token    string
	verified bool
}

// NewClient returns a client for the keel serve on addr whose data
// directory is dataDir.
func NewClient(addr, dataDir string) *Client {
	return &Client{addr: addr, dataDir: dataDir, http: &http.Client{Timeout: 30 * time.Second}}
}

// errNotRunning says how to get keel serve up.
var errNotRunning = errors.New("keel serve is not running: open the Keel app (or run `keel serve`), then try again")

func (c *Client) base() string { return "http://" + c.addr }

// ready returns the token once it is read and the server proved it holds
// it.
func (c *Client) ready(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token == "" {
		tok, err := authtoken.ReadMCP(c.dataDir)
		if errors.Is(err, os.ErrNotExist) {
			return "", errors.New("no keel-mcp.token yet: open the Keel app once (keel serve makes it on start), then try again")
		}
		if err != nil {
			return "", err
		}
		c.token = tok
	}
	if !c.verified {
		if err := c.verify(ctx, c.token); err != nil {
			return "", err
		}
		c.verified = true
	}
	return c.token, nil
}

// verify asks /api/health for HMAC(token, nonce), sending the nonce only.
func (c *Client) verify(ctx context.Context, token string) error {
	nonce, err := authtoken.NewNonce()
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base()+"/api/health?mcpNonce="+url.QueryEscape(nonce), nil)
	if err != nil {
		return err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return errNotRunning
	}
	defer res.Body.Close()
	var body struct {
		MCPProof string `json:"mcpProof"`
	}
	if res.StatusCode == http.StatusOK {
		_ = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&body)
	}
	if !authtoken.CheckProof(token, nonce, body.MCPProof) {
		return fmt.Errorf("the server on %s did not prove it is this user's keel serve (an older Keel without keel mcp, or another program on that port)", c.addr)
	}
	return nil
}

// Do sends one request to keel serve and decodes the answer into out. An
// error answer comes back as an error with keel serve's message.
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	tok, err := c.ready(ctx)
	if err != nil {
		return err
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base()+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set(authtoken.MCPHeader, tok)
	res, err := c.http.Do(req)
	if err != nil {
		// keel serve may have restarted on another token: check again
		// next time.
		c.mu.Lock()
		c.verified = false
		c.token = ""
		c.mu.Unlock()
		return errNotRunning
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		if res.StatusCode == http.StatusUnauthorized {
			c.mu.Lock()
			c.verified = false
			c.token = ""
			c.mu.Unlock()
		}
		msg := strings.TrimSpace(e.Error)
		if msg == "" {
			msg = http.StatusText(res.StatusCode)
		}
		return fmt.Errorf("keel serve: %s", msg)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func urlPathEscape(s string) string { return url.PathEscape(strings.TrimSpace(s)) }
