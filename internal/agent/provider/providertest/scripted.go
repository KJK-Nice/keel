package providertest

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"keel/internal/agent/provider"
)

// Scripted is the test provider `keel serve` uses when started with
// KEEL_AGENT_PROVIDER=fake (end-to-end tests only). It sends nothing
// anywhere and has no way to: it builds a canned reply from the request it
// is given, in this process, and keeps only the last few requests in
// memory for Go tests to inspect.
//
// The reply depends on the last user message:
//   - it states how many pane excerpts it was given and how many redaction
//     placeholders ([secret:N], [ip:N], [host:N]) arrived, so a test can
//     see from the UI that redaction ran before the provider was called;
//   - it proposes one command for pane 1 when panes were given: a
//     destructive one when the question says "delete", a state change for
//     "touch", none for "no command", and a read-only echo otherwise;
//   - when the question says "long", it adds a tall answer with **bold**
//     text before the proposal and a second proposal after it, so a test
//     can check that the thread scrolls and the cards keep their height.
//
// An in-pane suggestion request (the message starts with SuggestPrefix)
// gets suggestion blocks instead: a destructive `rm -rf` for words with
// "delete" or "clean up", a state change for "touch", a command that only
// Jev's fake rates stricter for "stricter", and otherwise a read-only echo
// with two read-only alternatives.
//
// The proposal block uses the agent service's format (```keel-proposal);
// internal/agent/service tests check that the two agree.
type Scripted struct {
	mu       sync.Mutex
	requests []provider.Request
}

const keepRequests = 16

func NewScripted() *Scripted { return &Scripted{} }

func (s *Scripted) ID() string { return "fake" }

func (s *Scripted) Generate(ctx context.Context, req provider.Request) (provider.Response, error) {
	if err := ctx.Err(); err != nil {
		return provider.Response{}, err
	}
	s.mu.Lock()
	s.requests = append(s.requests, req)
	if len(s.requests) > keepRequests {
		s.requests = s.requests[len(s.requests)-keepRequests:]
	}
	s.mu.Unlock()

	last := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == provider.RoleUser {
			last = req.Messages[i].Text.String()
			break
		}
	}
	if strings.HasPrefix(last, SuggestPrefix) {
		return suggestReply(strings.ToLower(strings.TrimPrefix(last, SuggestPrefix))), nil
	}
	panes := strings.Count(last, "--- pane ")
	placeholders := strings.Count(last, "[secret:") + strings.Count(last, "[ip:") + strings.Count(last, "[host:")
	question := strings.ToLower(last)
	if i := strings.LastIndex(question, "question:"); i >= 0 {
		question = question[i:]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Test provider: nothing left this machine. I was given %d pane excerpt%s and %d redacted value%s.",
		panes, plural(panes), placeholders, plural(placeholders))
	long := strings.Contains(question, "long")
	if long {
		b.WriteString("\n\n**Add your user to the docker group**")
		for i := 1; i <= 24; i++ {
			fmt.Fprintf(&b, "\n\nStep %d: this line of the long answer exists so the thread has to scroll.", i)
		}
	}
	if panes > 0 && !strings.Contains(question, "no command") {
		cmd, why, risk := "echo keel-agent-ok", "Prints a marker to show the command reached the pane.", "readonly"
		switch {
		case strings.Contains(question, "delete"):
			cmd, why, risk = "rm -rf /tmp/keel-agent-e2e", "Deletes the scratch folder.", "destructive"
		case strings.Contains(question, "touch"):
			cmd, why, risk = "touch /tmp/keel-agent-e2e-touched", "Creates a marker file.", "changes"
		}
		b.WriteString(" Run this in pane 1:\n\n```keel-proposal\n")
		fmt.Fprintf(&b, "{\"pane\": 1, \"command\": %q, \"why\": %q, \"risk\": %q}\n```\n", cmd, why, risk)
		if long {
			b.WriteString("\nThen run this in pane 1:\n\n```keel-proposal\n")
			b.WriteString("{\"pane\": 1, \"command\": \"echo keel-agent-second\", \"why\": \"Prints a second marker.\", \"risk\": \"readonly\"}\n```\n")
		}
	}
	return provider.Response{Text: b.String(), InputTokens: len(last) / 4, OutputTokens: b.Len() / 4}, nil
}

// SuggestPrefix must match service.SuggestPrefix (a service test checks).
const SuggestPrefix = "Words on the prompt line:\n"

func suggestReply(words string) provider.Response {
	type opt struct{ cmd, why, risk string }
	var opts []opt
	switch {
	case strings.Contains(words, "delete") || strings.Contains(words, "clean up"):
		opts = []opt{{"rm -rf /tmp/keel-suggest-e2e", "Deletes the scratch folder and everything in it.", "destructive"}}
	case strings.Contains(words, "touch"):
		opts = []opt{{"touch /tmp/keel-suggest-touched", "Creates an empty marker file.", "changes"}}
	case strings.Contains(words, "stricter"):
		opts = []opt{{"echo keel-jev-stricter", "Prints a marker that the fake judge rates as a state change.", "readonly"}}
	default:
		opts = []opt{
			{"echo keel-suggest-ok", "Prints a marker to show the command reached the prompt line.", "readonly"},
			{"echo keel-suggest-alt-2", "Prints a second marker.", "readonly"},
			{"echo keel-suggest-alt-3", "Prints a third marker.", "readonly"},
		}
	}
	var b strings.Builder
	for _, o := range opts {
		fmt.Fprintf(&b, "```keel-proposal\n{\"command\": %q, \"why\": %q, \"risk\": %q}\n```\n", o.cmd, o.why, o.risk)
	}
	return provider.Response{Text: b.String(), InputTokens: len(words) / 4, OutputTokens: b.Len() / 4}
}

// Requests returns the last requests Generate received, oldest first.
func (s *Scripted) Requests() []provider.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]provider.Request(nil), s.requests...)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

var _ provider.Provider = (*Scripted)(nil)
