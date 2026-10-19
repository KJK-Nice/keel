// Package providertest has a fake Provider for tests. It sends nothing
// anywhere: it records each request and returns a canned reply.
package providertest

import (
	"context"
	"sync"

	"keel/internal/agent/provider"
)

type Fake struct {
	mu    sync.Mutex
	Reply provider.Response
	// Replies, when set, are returned one per call in order (the last one
	// again once they run out), instead of Reply.
	Replies []provider.Response
	// Before, when set, runs at the start of each call with its number
	// (1 for the first), outside the lock: a test changes the world there
	// while the model "answers".
	Before   func(call int)
	Err      error
	requests []provider.Request
	served   int
}

// Script makes the next calls answer these texts in order (the last one
// again once they run out).
func (f *Fake) Script(texts ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Replies = nil
	for _, t := range texts {
		f.Replies = append(f.Replies, provider.Response{Text: t})
	}
	f.served = 0
}

func (f *Fake) ID() string { return "fake" }

func (f *Fake) Generate(ctx context.Context, req provider.Request) (provider.Response, error) {
	if err := ctx.Err(); err != nil {
		return provider.Response{}, err
	}
	f.mu.Lock()
	f.requests = append(f.requests, req)
	n := len(f.requests)
	before := f.Before
	f.mu.Unlock()
	if before != nil {
		before(n)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Replies) > 0 {
		i := f.served
		f.served++
		if i >= len(f.Replies) {
			i = len(f.Replies) - 1
		}
		return f.Replies[i], f.Err
	}
	return f.Reply, f.Err
}

// Requests returns what Generate received, in order.
func (f *Fake) Requests() []provider.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]provider.Request(nil), f.requests...)
}

var _ provider.Provider = (*Fake)(nil)
