// Package jev is a minimal TypeSafe Jev client (POST /v1/systemone) over
// net/http, behind provider.Judge. Jev is a judgment model: it answers
// typed questions (Noul, Choice) about a state with probabilities and never
// generates text.
//
// The HTTP contract follows https://docs.typesafe.ai/api.md (checked
// 2026-10-15, model jev-1.13.0): a JSON body {state, model, questions},
// "Authorization: Bearer <key>", and answers keyed by the question ids.
//
// New is the only way to build one, and only the agent service calls it
// (service.RealJudge; internal/agent/invariants_test.go). The key travels
// only in the Authorization header, never in a URL, a log line or an
// error. There are no retries: detection has a latency budget, and a
// failed call falls back to #-lines in the caller.
package jev

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
	defaultBaseURL = "https://api.typesafe.ai"
	endpointPath   = "/v1/systemone"
	// Model is pinned, not jev-latest: the thresholds in
	// internal/agent/service/suggestq.go were tuned against it, and an
	// alias moves on a new release.
	Model          = "jev-1.13.0"
	maxBody        = 1 << 20
	defaultTimeout = 10 * time.Second
)

type Client struct {
	key     string
	baseURL string
	model   string
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

// New returns a client that authenticates with key.
func New(key string, opts ...Option) (*Client, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, provider.ErrNoKey
	}
	c := &Client{key: key, baseURL: defaultBaseURL, model: Model, http: &http.Client{Timeout: defaultTimeout}}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

func (c *Client) ID() string { return "jev" }

type noulCriteria struct {
	True  string `json:"true,omitempty"`
	False string `json:"false,omitempty"`
}

type question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	// *noulCriteria for a Noul (omitted when empty), map for a Choice.
	Criteria any `json:"criteria,omitempty"`
}

type request struct {
	State     map[string]any      `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]question `json:"questions"`
}

type answer struct {
	Type          string             `json:"type"`
	Noul          *float64           `json:"noul"`
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
}

type response struct {
	Model   string            `json:"model"`
	Answers map[string]answer `json:"answers"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Judge sends one request built only from req: the Safe state fields and
// the questions. Every question must come back answered with its type.
func (c *Client) Judge(ctx context.Context, req provider.JudgeRequest) (provider.JudgeResponse, error) {
	if len(req.Questions) == 0 {
		return provider.JudgeResponse{}, errors.New("jev: no questions")
	}
	body := request{State: map[string]any{}, Model: c.model, Questions: map[string]question{}}
	for k, v := range req.Fields {
		body.State[k] = v.String()
	}
	for k, list := range req.Lists {
		out := make([]string, len(list))
		for i, v := range list {
			out[i] = v.String()
		}
		body.State[k] = out
	}
	for k, m := range req.Maps {
		out := make(map[string]string, len(m))
		for id, v := range m {
			out[id] = v.String()
		}
		body.State[k] = out
	}
	for id, q := range req.Questions {
		switch q.Type {
		case provider.QuestionNoul:
			wq := question{Type: "noul", Instructions: q.Instructions}
			if q.True != "" || q.False != "" {
				wq.Criteria = &noulCriteria{True: q.True, False: q.False}
			}
			body.Questions[id] = wq
		case provider.QuestionChoice:
			if len(q.Options) < 2 {
				return provider.JudgeResponse{}, fmt.Errorf("jev: choice %q needs two or more options", id)
			}
			crit := make(map[string]*string, len(q.Options))
			for _, o := range q.Options {
				if o.Description == "" {
					crit[o.Key] = nil
					continue
				}
				d := o.Description
				crit[o.Key] = &d
			}
			body.Questions[id] = question{Type: "choice", Instructions: q.Instructions, Criteria: crit}
		default:
			return provider.JudgeResponse{}, fmt.Errorf("jev: question %q has unknown type %q", id, q.Type)
		}
	}
	b, err := json.Marshal(body)
	if err != nil {
		return provider.JudgeResponse{}, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+endpointPath, bytes.NewReader(b))
	if err != nil {
		return provider.JudgeResponse{}, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Authorization", "Bearer "+c.key)
	resp, err := c.http.Do(hreq)
	if err != nil {
		return provider.JudgeResponse{}, c.scrub(fmt.Errorf("jev: %w", err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return provider.JudgeResponse{}, c.scrub(fmt.Errorf("jev: read: %w", err))
	}
	if resp.StatusCode != http.StatusOK {
		// The body names the bad field on a 422; keep the status only, so
		// nothing from the request is echoed into a log.
		return provider.JudgeResponse{}, fmt.Errorf("jev: HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	var out response
	if err := json.Unmarshal(raw, &out); err != nil {
		return provider.JudgeResponse{}, errors.New("jev: unreadable response")
	}
	res := provider.JudgeResponse{Model: out.Model, Nouls: map[string]float64{}, Choices: map[string]provider.ChoiceAnswer{}, InputTokens: out.Usage.InputTokens}
	for id, q := range req.Questions {
		a, ok := out.Answers[id]
		if !ok {
			return provider.JudgeResponse{}, fmt.Errorf("jev: no answer for %q", id)
		}
		switch q.Type {
		case provider.QuestionNoul:
			if a.Type != "noul" || a.Noul == nil || *a.Noul < 0 || *a.Noul > 1 {
				return provider.JudgeResponse{}, fmt.Errorf("jev: bad noul answer for %q", id)
			}
			res.Nouls[id] = *a.Noul
		case provider.QuestionChoice:
			if a.Type != "choice" || !hasOption(q.Options, a.Choice) {
				return provider.JudgeResponse{}, fmt.Errorf("jev: bad choice answer for %q", id)
			}
			probs := map[string]float64{}
			for _, o := range q.Options {
				probs[o.Key] = a.Probabilities[o.Key]
			}
			res.Choices[id] = provider.ChoiceAnswer{Choice: a.Choice, Probabilities: probs}
		}
	}
	return res, nil
}

func hasOption(opts []provider.Option, key string) bool {
	for _, o := range opts {
		if o.Key == key {
			return true
		}
	}
	return false
}

// scrub removes the key from an error, in case a transport message ever
// echoes it.
func (c *Client) scrub(err error) error {
	if err == nil || !strings.Contains(err.Error(), c.key) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), c.key, "[key]"))
}

var _ provider.Judge = (*Client)(nil)
