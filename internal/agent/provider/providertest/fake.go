// Package providertest has a fake Provider for tests. It sends nothing
// anywhere: it records each request and returns a canned reply.
package providertest

import (
	"context"
	"sync"

	"keel/internal/agent/provider"
)

type Fake struct {
	mu       sync.Mutex
	Reply    provider.Response
	Err      error
	requests []provider.Request
}

func (f *Fake) ID() string { return "fake" }

func (f *Fake) Generate(ctx context.Context, req provider.Request) (provider.Response, error) {
	if err := ctx.Err(); err != nil {
		return provider.Response{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	return f.Reply, f.Err
}

// Requests returns what Generate received, in order.
func (f *Fake) Requests() []provider.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]provider.Request(nil), f.requests...)
}

var _ provider.Provider = (*Fake)(nil)
