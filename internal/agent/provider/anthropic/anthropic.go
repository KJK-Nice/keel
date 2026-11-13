// Package anthropic is a minimal Claude Messages API client (POST
// /v1/messages) behind provider.Provider: the endpoint, the key header and
// the request and response mapping. The HTTP core is provider/transport.
//
// New is the only way to build one, and only the agent service calls it
// (service.Real, from providerFor on a user's Ask or ⇥), as for the Gemini
// client (internal/agent/invariants_test.go).
package anthropic

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"keel/internal/agent/provider"
	"keel/internal/agent/provider/transport"
)

const (
	id = "anthropic"
	// apiVersion is the anthropic-version header every request carries.
	apiVersion = "2023-06-01"
	// maxOutputCap is the hard cap on max_tokens (thinking and answer
	// together), the synchronous output limit of the current models.
	maxOutputCap = 128000
)

var config = transport.Config{
	Name:    id,
	BaseURL: "https://api.anthropic.com",
	// One request: a full answer after a long think takes a while.
	Timeout: 120 * time.Second,
	MaxBody: 4 << 20,
	// The key travels only in the x-api-key header.
	Auth: func(h http.Header, key string) {
		h.Set("anthropic-version", apiVersion)
		h.Set("x-api-key", key)
	},
	ErrorText: true,
}

type Client struct{ t *transport.Client }

type Option = transport.Option

// WithBaseURL points the client at another endpoint (tests use an
// httptest server on loopback).
func WithBaseURL(u string) Option { return transport.WithBaseURL(u) }

// New returns a client that authenticates with key.
func New(key string, opts ...Option) (*Client, error) {
	t, err := transport.New(config, key, opts...)
	if err != nil {
		return nil, err
	}
	return &Client{t: t}, nil
}

func (c *Client) ID() string { return id }

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// outputConfig is output_config: effort is how much the model works on
// the reply, thinking included (low, medium, high; xhigh and max are
// never sent). It is the provider.Thinking* level by the same name.
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

// thinkingRoom is the room max_tokens leaves for thinking at an effort, on
// top of the answer (transport.OutputLimit). Effort is a signal, not a
// budget, so the room is generous.
var thinkingRoom = map[string]int{
	provider.ThinkingLow:    4096,
	provider.ThinkingMedium: 8192,
	provider.ThinkingHigh:   16384,
	"":                      16384, // the model's default
}

// outputLimit is max_tokens for an answer of up to answer tokens.
func outputLimit(answer int, effort string) int {
	return transport.OutputLimit(answer, thinkingRoom[effort], maxOutputCap)
}

// stops maps stop_reason to a provider.Stop* reason: "" when the model
// finished (end_turn, a stop sequence, or no reason given).
var stops = map[string]string{
	"":                              "",
	"end_turn":                      "",
	"stop_sequence":                 "",
	"max_tokens":                    provider.StopLength,
	"model_context_window_exceeded": provider.StopLength,
	"refusal":                       provider.StopSafety,
}

// Generate sends one Messages request built only from req.
func (c *Client) Generate(ctx context.Context, req provider.Request) (provider.Response, error) {
	model, err := c.t.Model(req.Model)
	if err != nil {
		return provider.Response{}, err
	}
	body := msgRequest{Model: model, System: req.System.String()}
	// The API refuses an empty turn; an empty one says nothing.
	for _, t := range transport.Turns(req, "user", "assistant", true) {
		body.Messages = append(body.Messages, message{Role: t.Role, Content: t.Text})
	}
	if len(body.Messages) == 0 {
		return provider.Response{}, errors.New("anthropic: no messages")
	}
	effort, err := provider.Thinking(id, model, req.Thinking)
	if err != nil {
		return provider.Response{}, err
	}
	if effort != "" {
		body.OutputConfig = &outputConfig{Effort: effort}
	}
	// max_tokens is required by the API.
	body.MaxTokens = outputLimit(req.MaxOutputTokens, effort)
	var out msgResponse
	err = c.t.Post(ctx, "/v1/messages", body, &out, func() (string, bool) {
		if out.Error == nil {
			return "", false
		}
		return out.Error.Message, true
	})
	if err != nil {
		return provider.Response{}, err
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
		Stop:         transport.Stop(stops, out.StopReason),
	}, nil
}

var _ provider.Provider = (*Client)(nil)
