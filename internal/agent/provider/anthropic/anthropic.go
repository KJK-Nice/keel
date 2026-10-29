// Package anthropic is a minimal Claude Messages API client (POST
// /v1/messages) over net/http, behind provider.Provider.
//
// New is the only way to build one, and only the agent service calls it
// (service.Real, from providerFor on a user's Ask or ⇥), as for the Gemini
// client (internal/agent/invariants_test.go).
package anthropic

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

const (
	defaultBaseURL = "https://api.anthropic.com"
	// apiVersion is the anthropic-version header every request carries.
	apiVersion = "2023-06-01"
	maxBody    = 4 << 20
	// defaultTimeout covers one request: a full answer after a long
	// think takes a while.
	defaultTimeout = 120 * time.Second
	// maxOutputCap is the hard cap on max_tokens (thinking and answer
	// together), the synchronous output limit of the current models.
	maxOutputCap = 128000
	// defaultAnswer is the answer room when the request sets none:
	// max_tokens is required by the API.
	defaultAnswer = 4096
)

type Client struct {
	key     string
	baseURL string
	http    *http.Client
}

type Option func(*Client)

// WithBaseURL points the client at another endpoint (tests use an
// httptest server on loopback).
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithHTTPClient sets the HTTP client.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// New returns a client that authenticates with key. The key travels only
// in the x-api-key header, never in a URL, log line or error.
func New(key string, opts ...Option) (*Client, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, provider.ErrNoKey
	}
	c := &Client{key: key, baseURL: defaultBaseURL, http: &http.Client{Timeout: defaultTimeout}}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

func (c *Client) ID() string { return "anthropic" }

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// outputConfig is output_config: effort is how much the model works on
// the reply, thinking included (low, medium, high; xhigh and max are
// never sent).
type outputConfig struct {
	Effort string `json:"effort,omitempty"`
}

type msgRequest struct {
	Model        string        `json:"model"`
	MaxTokens    int           `json:"max_tokens"`
	System       string        `json:"system,omitempty"`
	Messages     []message     `json:"messages"`
	OutputConfig *outputConfig `json:"output_config,omitempty"`
}

type msgResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// effort maps a provider.Thinking* level to output_config.effort, or ""
// for the model's default. The current models think adaptively and take
// effort as the control for how much; "minimal" has no effort of its own,
// so it is the lowest one, low.
func effort(level string) (string, error) {
	switch level {
	case "":
		return "", nil
	case provider.ThinkingMinimal, provider.ThinkingLow:
		return "low", nil
	case provider.ThinkingMedium:
		return "medium", nil
	case provider.ThinkingHigh:
		return "high", nil
	}
	return "", fmt.Errorf("anthropic: unknown thinking level %q", level)
}

// thinkingRoom is the room max_tokens leaves for thinking at an effort
// ("" for the model's default), on top of the answer: thinking tokens
// count toward max_tokens, so a small limit is spent on thinking and the
// answer stops mid-sentence (stop_reason max_tokens). Effort is a signal,
// not a budget, so the room is generous and the stop is still checked.
func thinkingRoom(effort string) int {
	switch effort {
	case "low":
		return 4096
	case "medium":
		return 8192
	}
	return 16384 // high, and the default
}

// outputLimit is max_tokens for an answer of up to answer tokens.
func outputLimit(answer int, effort string) int {
	if answer <= 0 {
		answer = defaultAnswer
	}
	return min(answer+thinkingRoom(effort), maxOutputCap)
}

// stopReason maps stop_reason to a provider.Stop* reason: "" when the
// model finished (end_turn, a stop sequence, or no reason given).
func stopReason(r string) string {
	switch r {
	case "", "end_turn", "stop_sequence":
		return ""
	case "max_tokens", "model_context_window_exceeded":
		return provider.StopLength
	case "refusal":
		return provider.StopSafety
	}
	return provider.StopOther
}

// Generate sends one Messages request built only from req.
func (c *Client) Generate(ctx context.Context, req provider.Request) (provider.Response, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" || strings.ContainsAny(model, "/?#% ") {
		return provider.Response{}, fmt.Errorf("anthropic: invalid model %q", model)
	}
	body := msgRequest{Model: model, System: req.System.String()}
	for _, m := range req.Messages {
		text := m.Text.String()
		if text == "" {
			// The API refuses an empty turn; an empty one says nothing.
			continue
		}
		role := "user"
		if m.Role == provider.RoleModel {
			role = "assistant"
		}
		body.Messages = append(body.Messages, message{Role: role, Content: text})
	}
	if len(body.Messages) == 0 {
		return provider.Response{}, errors.New("anthropic: no messages")
	}
	eff, err := effort(req.Thinking)
	if err != nil {
		return provider.Response{}, err
	}
	if eff != "" {
		body.OutputConfig = &outputConfig{Effort: eff}
	}
	body.MaxTokens = outputLimit(req.MaxOutputTokens, eff)
	b, err := json.Marshal(body)
	if err != nil {
		return provider.Response{}, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(b))
	if err != nil {
		return provider.Response{}, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("anthropic-version", apiVersion)
	hreq.Header.Set("x-api-key", c.key)
	resp, err := c.http.Do(hreq)
	if err != nil {
		return provider.Response{}, c.scrub(fmt.Errorf("anthropic: %w", err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return provider.Response{}, c.scrub(fmt.Errorf("anthropic: read: %w", err))
	}
	var out msgResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return provider.Response{}, fmt.Errorf("anthropic: HTTP %d: unreadable response", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK || out.Error != nil {
		msg := http.StatusText(resp.StatusCode)
		if out.Error != nil && out.Error.Message != "" {
			msg = out.Error.Message
		}
		return provider.Response{}, c.scrub(fmt.Errorf("anthropic: HTTP %d: %s", resp.StatusCode, msg))
	}
	// The answer is every text block in order; thinking blocks (empty
	// unless asked for, which Keel never does) are left out.
	var text strings.Builder
	for _, blk := range out.Content {
		if blk.Type == "text" {
			text.WriteString(blk.Text)
		}
	}
	return provider.Response{
		Text:        text.String(),
		InputTokens: out.Usage.InputTokens,
		// output_tokens counts the thinking too: the API gives no split,
		// so ThoughtTokens stays 0.
		OutputTokens: out.Usage.OutputTokens,
		Stop:         stopReason(out.StopReason),
	}, nil
}

// scrub removes the key from an error, in case a transport or server
// message ever echoes it.
func (c *Client) scrub(err error) error {
	if err == nil || !strings.Contains(err.Error(), c.key) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), c.key, "[key]"))
}

var _ provider.Provider = (*Client)(nil)
