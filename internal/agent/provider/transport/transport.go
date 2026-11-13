// Package transport is the HTTP core the provider clients share
// (provider/gemini, provider/anthropic, provider/jev): one JSON POST with a
// size limit, one error mapping, one key scrub and the options. A client
// adds what is its vendor's own: the endpoint, the header the key travels
// in, and the request and response mapping.
//
// It names no endpoint and reads no key from anywhere: a client hands it
// both. Only the three client packages import it
// (internal/agent/invariants_test.go).
package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"keel/internal/agent/provider"
)

// DefaultAnswerTokens is the answer room of a request that sets none, for
// every vendor: a reply is never left unbounded.
const DefaultAnswerTokens = 4096

// maxErrorText caps a vendor's error message in a returned error.
const maxErrorText = 300

// Config is what a vendor's client is made of.
type Config struct {
	// Name is the vendor's id ("gemini"); it starts every error.
	Name string
	// BaseURL is the vendor's API, without a trailing slash.
	BaseURL string
	// Timeout covers one request.
	Timeout time.Duration
	// MaxBody caps the reply read.
	MaxBody int64
	// Auth puts the key in the request's headers, and nowhere else.
	Auth func(h http.Header, key string)
	// ErrorText lets the vendor's own error message into the returned
	// error (one line, capped, the key removed). It is set for the model
	// providers, whose errors the user reads in the agent panel and needs
	// ("API key not valid", "model not found", a quota). It is off for the
	// judge, whose failures are never shown: only the status is kept, so
	// nothing the vendor echoes from a request can reach a log.
	ErrorText bool
}

// Client posts JSON to one vendor's API with one key.
type Client struct {
	cfg  Config
	key  string
	http *http.Client
}

type Option func(*Client)

// WithBaseURL points the client at another endpoint (an httptest server on
// loopback).
func WithBaseURL(u string) Option {
	return func(c *Client) { c.cfg.BaseURL = strings.TrimRight(u, "/") }
}

// New returns a client that authenticates with key. The key travels only
// in the header cfg.Auth sets, never in a URL, a log line or an error.
func New(cfg Config, key string, opts ...Option) (*Client, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, provider.ErrNoKey
	}
	c := &Client{cfg: cfg, key: key, http: &http.Client{Timeout: cfg.Timeout}}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// Post sends body as JSON to path and decodes the reply into out. vendorErr
// is asked, once out is decoded, for the error the reply itself carries
// (its message, and whether there is one); nil when the vendor's replies
// carry none.
//
// Every error starts with the vendor's name and holds the HTTP status when
// there is one; none holds the key.
func (c *Client) Post(ctx context.Context, path string, body, out any, vendorErr func() (string, bool)) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+path, bytes.NewReader(b))
	if err != nil {
		return c.fail("%v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.cfg.Auth(req.Header, c.key)
	resp, err := c.http.Do(req)
	if err != nil {
		return c.fail("%v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, c.cfg.MaxBody))
	if err != nil {
		return c.fail("read: %v", err)
	}
	decoded := json.Unmarshal(raw, out) == nil
	msg, failed := "", false
	if decoded && vendorErr != nil {
		msg, failed = vendorErr()
	}
	switch {
	case resp.StatusCode != http.StatusOK || failed:
		if msg == "" || !c.cfg.ErrorText {
			msg = http.StatusText(resp.StatusCode)
		}
		return c.fail("HTTP %d: %s", resp.StatusCode, oneLine(msg))
	case !decoded:
		return c.fail("HTTP %d: unreadable response", resp.StatusCode)
	}
	return nil
}

// fail makes the client's error: the vendor's name first, the key removed
// in case a transport or server message ever echoes it.
func (c *Client) fail(format string, args ...any) error {
	return errors.New(provider.ScrubKey(c.cfg.Name+": "+fmt.Sprintf(format, args...), c.key))
}

// oneLine keeps a vendor's message on one line and within maxErrorText.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxErrorText {
		s = strings.ToValidUTF8(s[:maxErrorText], "") + "…"
	}
	return s
}

// Model checks a model name before it goes into a URL or a body.
func (c *Client) Model(model string) (string, error) {
	model = strings.TrimSpace(model)
	if model == "" || strings.ContainsAny(model, "/?#% ") {
		return "", fmt.Errorf("%s: invalid model %q", c.cfg.Name, model)
	}
	return model, nil
}

// Turn is one message as a vendor's API takes it.
type Turn struct {
	Role string
	Text string
}

// Turns maps a request's messages to a vendor's role names. dropEmpty
// leaves out a turn with no text, for an API that refuses one; an API that
// takes one keeps it, so the roles still alternate as it expects.
func Turns(req provider.Request, userRole, modelRole string, dropEmpty bool) []Turn {
	var out []Turn
	for _, m := range req.Messages {
		text := m.Text.String()
		if text == "" && dropEmpty {
			continue
		}
		role := userRole
		if m.Role == provider.RoleModel {
			role = modelRole
		}
		out = append(out, Turn{Role: role, Text: text})
	}
	return out
}

// OutputLimit is the output limit to send for an answer of up to answer
// tokens (DefaultAnswerTokens when the request sets none), with room for
// the model's thinking on top, which counts toward the limit, up to the
// vendor's hard cap. A small limit would be spent on thinking and the
// answer would stop mid-sentence; the room is an allowance, not a budget,
// so the reply's stop reason is still checked.
func OutputLimit(answer, thinkingRoom, hardCap int) int {
	if answer <= 0 {
		answer = DefaultAnswerTokens
	}
	return min(answer+thinkingRoom, hardCap)
}

// Stop maps a vendor's finish reason to a provider.Stop* reason through
// its table: "" (the model finished) for a reason the table maps to "",
// provider.StopOther for one it does not know.
func Stop(table map[string]string, reason string) string {
	if stop, ok := table[reason]; ok {
		return stop
	}
	return provider.StopOther
}
