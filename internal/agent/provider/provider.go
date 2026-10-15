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
	// Thinking is how much the model reasons before it answers: one of
	// the Thinking* levels, or "" for the model's default. Each client
	// maps it to its own API and to what the model accepts.
	Thinking string
}

// Thinking levels, lowest first. Not every model takes every level; the
// client lowers a level a model does not take to the nearest it does.
const (
	ThinkingMinimal = "minimal"
	ThinkingLow     = "low"
	ThinkingMedium  = "medium"
	ThinkingHigh    = "high"
)

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
	// SuggestModel and SuggestThinking are used for in-pane suggestions
	// (§5.5), where the user waits after ⇥: a small model that thinks
	// little. The agent panel keeps the model from the settings, with the
	// model's default thinking. Empty: the settings model, default thinking.
	SuggestModel    string `json:"suggestModel,omitempty"`
	SuggestThinking string `json:"suggestThinking,omitempty"`
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
		Models: []string{"gemini-3.8-flash", "gemini-3.6-flash", "gemini-3.5-flash-lite"},
		// gemini-3.5-flash-lite takes "minimal"; gemini-3.8-flash does not
		// (low, medium, high only), so the two are never mixed up.
		SuggestModel:    "gemini-3.5-flash-lite",
		SuggestThinking: ThinkingMinimal,
		Warning:         GeminiWarning,
		TermsURL:        "https://ai.google.dev/gemini-api/terms",
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

// Judge answers typed questions about a state (phase 6c, TypeSafe Jev). It
// never generates text: each answer is a probability (Noul) or a pick from
// options the caller wrote (Choice). Like a Provider it takes only
// redacted text: every state value is Safe, and the questions are
// constants written in Keel's code.
type Judge interface {
	ID() string
	Judge(ctx context.Context, req JudgeRequest) (JudgeResponse, error)
}

// QuestionType is the answer shape of a Question.
type QuestionType string

const (
	QuestionNoul   QuestionType = "noul"   // yes/no; the answer is P(yes)
	QuestionChoice QuestionType = "choice" // one of Options
)

// Option is one answer a Choice may pick.
type Option struct {
	Key         string
	Description string
}

// Question is one typed question. Instructions point at state fields by
// path in backticks (`prompt_line`, `commands[0]`), never by pasting the
// text in.
type Question struct {
	Type         QuestionType
	Instructions string
	// Noul only: what a yes and a no mean (optional).
	True, False string
	// Choice only, in order.
	Options []Option
}

// JudgeRequest is one request: every question about one state. Untrusted
// terminal text lives in its own named field (Fields or Lists), apart from
// the questions.
type JudgeRequest struct {
	Fields    map[string]redact.Safe
	Lists     map[string][]redact.Safe
	Questions map[string]Question
}

// ChoiceAnswer is a Choice's pick and the probability of every option.
type ChoiceAnswer struct {
	Choice        string
	Probabilities map[string]float64
}

// JudgeResponse holds the answers under the question ids of the request.
type JudgeResponse struct {
	Model       string
	Nouls       map[string]float64
	Choices     map[string]ChoiceAnswer
	InputTokens int
}
