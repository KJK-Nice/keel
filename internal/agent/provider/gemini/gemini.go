// Package gemini is a minimal Google Gemini API client (generateContent)
// over net/http, behind provider.Provider.
//
// New is the only way to build one. In phase 6a nothing outside this
// package's tests imports it (internal/agent/egress_test.go fails if
// anything does); phase 6b adds the agent service as its one caller.
package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"keel/internal/agent/provider"
)

const (
	defaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"
	maxBody        = 4 << 20
	defaultTimeout = 60 * time.Second
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
// in the x-goog-api-key header, never in a URL, log line or error.
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

func (c *Client) ID() string { return "gemini" }

type part struct {
	Text string `json:"text"`
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
// enum (MINIMAL, LOW, MEDIUM, HIGH).
type thinkingConfig struct {
	ThinkingLevel string `json:"thinkingLevel,omitempty"`
}

// noMinimal lists the models that reject the MINIMAL thinking level
// (https://ai.google.dev/gemini-api/docs/thinking): their lowest is LOW.
// A model not listed here is sent the level as asked.
var noMinimal = map[string]bool{
	"gemini-3.8-flash":       true,
	"gemini-3.1-pro-preview": true,
	"gemini-3-pro-preview":   true,
	"gemini-2.5-pro":         true,
	"gemini-2.5-flash":       true,
	"gemini-2.5-flash-lite":  true,
}

// thinkingLevel maps a provider.Thinking* level to the API's enum for
// model, or "" to leave the model's default.
func thinkingLevel(model, level string) (string, error) {
	switch level {
	case "":
		return "", nil
	case provider.ThinkingMinimal:
		if noMinimal[model] {
			return "LOW", nil
		}
		return "MINIMAL", nil
	case provider.ThinkingLow, provider.ThinkingMedium, provider.ThinkingHigh:
		return strings.ToUpper(level), nil
	}
	return "", fmt.Errorf("gemini: unknown thinking level %q", level)
}

// SupportsMinimal reports whether model takes the MINIMAL thinking level.
func SupportsMinimal(model string) bool { return !noMinimal[model] }

type genRequest struct {
	SystemInstruction *content   `json:"systemInstruction,omitempty"`
	Contents          []content  `json:"contents"`
	GenerationConfig  *genConfig `json:"generationConfig,omitempty"`
}

type genResponse struct {
	Candidates []struct {
		Content      content `json:"content"`
		FinishReason string  `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
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
	model := strings.TrimSpace(req.Model)
	if model == "" || strings.ContainsAny(model, "/?#% ") {
		return provider.Response{}, fmt.Errorf("gemini: invalid model %q", model)
	}
	body := genRequest{}
	if sys := req.System.String(); sys != "" {
		body.SystemInstruction = &content{Parts: []part{{Text: sys}}}
	}
	for _, m := range req.Messages {
		role := "user"
		if m.Role == provider.RoleModel {
			role = "model"
		}
		body.Contents = append(body.Contents, content{Role: role, Parts: []part{{Text: m.Text.String()}}})
	}
	if len(body.Contents) == 0 {
		return provider.Response{}, errors.New("gemini: no messages")
	}
	level, err := thinkingLevel(model, req.Thinking)
	if err != nil {
		return provider.Response{}, err
	}
	if req.MaxOutputTokens > 0 || level != "" {
		body.GenerationConfig = &genConfig{MaxOutputTokens: req.MaxOutputTokens}
		if level != "" {
			body.GenerationConfig.ThinkingConfig = &thinkingConfig{ThinkingLevel: level}
		}
	}
	b, err := json.Marshal(body)
	if err != nil {
		return provider.Response{}, err
	}
	endpoint := c.baseURL + "/models/" + url.PathEscape(model) + ":generateContent"
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return provider.Response{}, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("x-goog-api-key", c.key)
	resp, err := c.http.Do(hreq)
	if err != nil {
		return provider.Response{}, c.scrub(fmt.Errorf("gemini: %w", err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return provider.Response{}, c.scrub(fmt.Errorf("gemini: read: %w", err))
	}
	var out genResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return provider.Response{}, fmt.Errorf("gemini: HTTP %d: unreadable response", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK || out.Error != nil {
		msg := http.StatusText(resp.StatusCode)
		if out.Error != nil && out.Error.Message != "" {
			msg = out.Error.Message
		}
		return provider.Response{}, c.scrub(fmt.Errorf("gemini: HTTP %d: %s", resp.StatusCode, msg))
	}
	if out.PromptFeedback.BlockReason != "" {
		return provider.Response{}, fmt.Errorf("gemini: prompt blocked (%s)", out.PromptFeedback.BlockReason)
	}
	if len(out.Candidates) == 0 {
		return provider.Response{}, errors.New("gemini: no candidates")
	}
	var text strings.Builder
	for _, p := range out.Candidates[0].Content.Parts {
		text.WriteString(p.Text)
	}
	return provider.Response{
		Text:         text.String(),
		InputTokens:  out.UsageMetadata.PromptTokenCount,
		OutputTokens: out.UsageMetadata.CandidatesTokenCount,
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
