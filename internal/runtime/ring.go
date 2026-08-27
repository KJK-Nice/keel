package runtime

import (
	"sync"
)

type ring struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func newRing(max int) *ring {
	return &ring{max: max}
}

func (r *ring) Write(p []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(p) >= r.max {
		r.buf = append([]byte(nil), p[len(p)-r.max:]...)
		return
	}
	need := len(r.buf) + len(p) - r.max
	if need > 0 {
		r.buf = append(r.buf[need:], p...)
		return
	}
	r.buf = append(r.buf, p...)
}

func (r *ring) Bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]byte(nil), r.buf...)
}
