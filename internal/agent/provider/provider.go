// Package provider is the seam between Keel's agent and a model provider.
//
// A Provider takes only redacted text (redact.Safe), so terminal output
// cannot reach one without going through internal/agent/redact first. Real
// clients live in their own sub-packages (provider/gemini) and are built
// only by the agent service in phase 6b, on a user action; in phase 6a no
// code outside tests builds or calls one (see egress_test.go).
package provider

import (
	"context"
	"errors"

	"keel/internal/agent/redact"
)

// Role of a message in a conversation.
type Role string

const (
	RoleUser  Role = "user"
	RoleModel Role = "model"
)

type Message struct {
	Role Role
	Text redact.Safe
}

// Request is everything a provider receives. Every text field is Safe.
type Request struct {
	Model           string
	System          redact.Safe
	Messages        []Message
	MaxOutputTokens int
}

type Response struct {
	Text         string
	InputTokens  int
	OutputTokens int
}

// Provider generates a reply. Implementations must send nothing but the
// request's fields, and must never log or return the API key.
type Provider interface {
	ID() string
	Generate(ctx context.Context, req Request) (Response, error)
}

var (
	ErrNoKey   = errors.New("provider: no API key")
	ErrUnknown = errors.New("provider: unknown provider")
)

// Kind tells the settings page what a provider is for.
type Kind string

const (
	KindModel    Kind = "model"    // answers and command proposals (agent panel)
	KindDetector Kind = "detector" // plain-language detection and risk labels (6c)
)

// Descriptor describes a provider to the settings and first-run screens.
// It holds no secret.
type Descriptor struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Kind         Kind     `json:"kind"`
	KeyLabel     string   `json:"keyLabel"`
	KeyHelpURL   string   `json:"keyHelpUrl,omitempty"`
	DefaultModel string   `json:"defaultModel,omitempty"`
	Models       []string `json:"models,omitempty"`
	// Warning is shown next to the key field before the user connects.
	Warning  string `json:"warning,omitempty"`
	TermsURL string `json:"termsUrl,omitempty"`
}

// GeminiWarning is worded with care: it states what Google's terms allow,
// not what Google does.
const GeminiWarning = "On the free tier of the Gemini API, Google may use the content you submit, " +
	"and the responses, to improve its products, and human reviewers may read it. " +
	"Keel redacts secrets before sending, but the rest of what you share with the agent is sent as is. " +
	"If that matters to you, use a key from a paid (billing-enabled) project, where the terms differ, " +
	"and read Google's Gemini API terms."

var descriptors = []Descriptor{
	{
		ID:           "gemini",
		Name:         "Google Gemini",
		Kind:         KindModel,
		KeyLabel:     "Gemini API key",
		KeyHelpURL:   "https://aistudio.google.com/apikey",
		DefaultModel: "gemini-3.8-flash",
		// The 2.5 family is closed to keys that never used it, so new
		// users get a 404 from it.
		Models:   []string{"gemini-3.8-flash", "gemini-3.6-flash", "gemini-3.5-flash-lite"},
		Warning:  GeminiWarning,
		TermsURL: "https://ai.google.dev/gemini-api/terms",
	},
	{
		ID:       "jev",
		Name:     "TypeSafe Jev",
		Kind:     KindDetector,
		KeyLabel: "Jev API key",
	},
}

// Descriptors lists the providers Keel knows, model providers first.
func Descriptors() []Descriptor {
	out := make([]Descriptor, len(descriptors))
	for i, d := range descriptors {
		d.Models = append([]string(nil), d.Models...)
		out[i] = d
	}
	return out
}

// Lookup returns the descriptor for id.
func Lookup(id string) (Descriptor, bool) {
	for _, d := range Descriptors() {
		if d.ID == id {
			return d, true
		}
	}
	return Descriptor{}, false
}
