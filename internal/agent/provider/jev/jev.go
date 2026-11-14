// Package jev is a minimal TypeSafe Jev client (POST /v1/systemone) behind
// provider.Judge: the endpoint, the key header and the request and
// response mapping. The HTTP core is provider/transport. Jev is a judgment
// model: it answers typed questions (Noul, Choice) about a state with
// probabilities and never generates text.
//
// The HTTP contract follows https://docs.typesafe.ai/api.md (checked
// 2026-10-15, model jev-1.13.0): a JSON body {state, model, questions},
// "Authorization: Bearer <key>", and answers keyed by the question ids.
//
// New is the only way to build one, and only the agent service calls it
// (service.RealJudge; internal/agent/invariants_test.go). There are no
// retries: detection has a latency budget, and a failed call falls back to
// #-lines in the caller.
package jev

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"keel/internal/agent/provider"
	"keel/internal/agent/provider/transport"
)

const id = "jev"

var config = transport.Config{
	Name:    id,
	BaseURL: "https://api.typesafe.ai",
	Timeout: 10 * time.Second,
	MaxBody: 1 << 20,
	// The key travels only in the Authorization header.
	Auth: func(h http.Header, key string) { h.Set("Authorization", "Bearer "+key) },
	// ErrorText stays off: the body names the bad field on a 422, and a
	// Jev failure is never shown to the user, so only the status is kept.
}

type Client struct{ t *transport.Client }

type Option = transport.Option

// WithBaseURL points the client at another endpoint (tests use an
// httptest server on loopback).
func WithBaseURL(u string) Option { return transport.WithBaseURL(u) }

func New(key string, opts ...Option) (*Client, error) {
	t, err := transport.New(config, key, opts...)
	if err != nil {
		return nil, err
	}
	return &Client{t: t}, nil
}

func (c *Client) ID() string { return id }

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
	body := request{State: map[string]any{}, Model: provider.JevModel, Questions: map[string]question{}}
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
	var out response
	if err := c.t.Post(ctx, "/v1/systemone", body, &out, nil); err != nil {
		return provider.JudgeResponse{}, err
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

var _ provider.Judge = (*Client)(nil)
