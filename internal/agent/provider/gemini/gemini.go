// Package gemini is a minimal Google Gemini API client (generateContent)
// behind provider.Provider: the endpoint, the key header and the request
// and response mapping. The HTTP core is provider/transport.
//
// New is the only way to build one, and only the agent service calls it
// (service.Real; internal/agent/invariants_test.go).
package gemini

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"keel/internal/agent/provider"
	"keel/internal/agent/provider/transport"
)

const (
	id = "gemini"
	// maxOutputCap is the hard cap on maxOutputTokens (thinking and
	// answer together), the output limit of the Gemini 3 models.
	maxOutputCap = 65536
)

var config = transport.Config{
	Name:    id,
	BaseURL: "https://generativelanguage.googleapis.com/v1beta",
	// One request: a full answer after a long think takes a while on a
	// thinking model.
	Timeout: 120 * time.Second,
	MaxBody: 4 << 20,
	// The key travels only in the x-goog-api-key header.
	Auth:      func(h http.Header, key string) { h.Set("x-goog-api-key", key) },
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

type part struct {
	Text string `json:"text"`
	// Thought marks a part that is the model's thinking, not its answer
	// (sent only when includeThoughts is asked, which Keel never does).
	Thought bool `json:"thought,omitempty"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

// genConfig is GenerationConfig in the generateContent REST API
// (https://ai.google.dev/api/generate-content#GenerationConfig).
type genConfig struct {
	MaxOutputTokens int             `json:"maxOutputTokens,omitempty"`
	ThinkingConfig  *thinkingConfig `json:"thinkingConfig,omitempty"`
}

// thinkingConfig is ThinkingConfig: thinkingLevel is the ThinkingLevel
// enum (MINIMAL, LOW, MEDIUM, HIGH), the provider.Thinking* level in
// upper case.
type thinkingConfig struct {
	ThinkingLevel string `json:"thinkingLevel,omitempty"`
}

// thinkingRoom is the room maxOutputTokens leaves for thinking at a level,
// on top of the answer (transport.OutputLimit). Gemini 3 thinks at HIGH by
// default. Levels are allowances, not budgets, so the room is generous.
var thinkingRoom = map[string]int{
	provider.ThinkingMinimal: 1024,
	provider.ThinkingLow:     4096,
	provider.ThinkingMedium:  8192,
	provider.ThinkingHigh:    24576,
	"":                       24576, // the model's default
}

// outputLimit is maxOutputTokens for an answer of up to answer tokens.
func outputLimit(answer int, level string) int {
	return transport.OutputLimit(answer, thinkingRoom[level], maxOutputCap)
}

// stops maps a FinishReason to a provider.Stop* reason: "" when the model
// finished (STOP, or no reason given).
var stops = map[string]string{
	"":                   "",
	"STOP":               "",
	"MAX_TOKENS":         provider.StopLength,
	"SAFETY":             provider.StopSafety,
	"PROHIBITED_CONTENT": provider.StopSafety,
	"SPII":               provider.StopSafety,
	"BLOCKLIST":          provider.StopSafety,
	"IMAGE_SAFETY":       provider.StopSafety,
	"RECITATION":         provider.StopRecitation,
}

type genRequest struct {
	SystemInstruction *content  `json:"systemInstruction,omitempty"`
	Contents          []content `json:"contents"`
	GenerationConfig  genConfig `json:"generationConfig"`
}

type genResponse struct {
	Candidates []struct {
		Content      content `json:"content"`
		FinishReason string  `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		ThoughtsTokenCount   int `json:"thoughtsTokenCount"`
	} `json:"usageMetadata"`
	PromptFeedback struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// Generate sends one generateContent request built only from req.
func (c *Client) Generate(ctx context.Context, req provider.Request) (provider.Response, error) {
	model, err := c.t.Model(req.Model)
	if err != nil {
		return provider.Response{}, err
	}
	body := genRequest{}
	if sys := req.System.String(); sys != "" {
		body.SystemInstruction = &content{Parts: []part{{Text: sys}}}
	}
	// An empty turn is kept: the API takes one, and the roles alternate.
	for _, t := range transport.Turns(req, "user", "model", false) {
		body.Contents = append(body.Contents, content{Role: t.Role, Parts: []part{{Text: t.Text}}})
	}
	if len(body.Contents) == 0 {
		return provider.Response{}, errors.New("gemini: no messages")
	}
	level, err := provider.Thinking(id, model, req.Thinking)
	if err != nil {
		return provider.Response{}, err
	}
	body.GenerationConfig.MaxOutputTokens = outputLimit(req.MaxOutputTokens, level)
	if level != "" {
		body.GenerationConfig.ThinkingConfig = &thinkingConfig{ThinkingLevel: strings.ToUpper(level)}
	}
	var out genResponse
	err = c.t.Post(ctx, "/models/"+url.PathEscape(model)+":generateContent", body, &out, func() (string, bool) {
		if out.Error == nil {
			return "", false
		}
		return out.Error.Message, true
	})
	if err != nil {
		return provider.Response{}, err
	}
	if out.PromptFeedback.BlockReason != "" {
		return provider.Response{}, fmt.Errorf("gemini: prompt blocked (%s)", out.PromptFeedback.BlockReason)
	}
	if len(out.Candidates) == 0 {
		return provider.Response{}, errors.New("gemini: no candidates")
	}
	// One candidate (candidateCount defaults to 1); its answer is every
	// text part in order, thought parts left out.
	cand := out.Candidates[0]
	var text strings.Builder
	for _, p := range cand.Content.Parts {
		if p.Thought {
			continue
		}
		text.WriteString(p.Text)
	}
	return provider.Response{
		Text:          text.String(),
		InputTokens:   out.UsageMetadata.PromptTokenCount,
		OutputTokens:  out.UsageMetadata.CandidatesTokenCount,
		ThoughtTokens: out.UsageMetadata.ThoughtsTokenCount,
		Stop:          transport.Stop(stops, cand.FinishReason),
	}, nil
}

var _ provider.Provider = (*Client)(nil)
