package runtime

import (
	"sync"
)

type ring struct {
	mu    sync.Mutex
	buf   []byte
	max   int
	total uint64 // bytes ever written; a mark is a value of total
}

func newRing(max int) *ring {
	return &ring{max: max}
}

func (r *ring) Write(p []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.total += uint64(len(p))
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

// Mark returns the current write position.
func (r *ring) Mark() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.total
}

// BytesSince returns what was written after mark that the ring still holds.
func (r *ring) BytesSince(mark uint64) []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	if mark >= r.total {
		return nil
	}
	n := r.total - mark
	if n >= uint64(len(r.buf)) {
		return append([]byte(nil), r.buf...)
	}
	return append([]byte(nil), r.buf[uint64(len(r.buf))-n:]...)
}

// Range returns what was written between the stream offsets from and to
// that the ring still holds. truncated is true when from had already left
// the ring (what is returned then starts at the oldest byte held).
func (r *ring) Range(from, to uint64) (b []byte, truncated bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if to > r.total {
		to = r.total
	}
	start := r.total - uint64(len(r.buf))
	if from < start {
		from, truncated = start, true
	}
	if from >= to {
		return nil, truncated
	}
	return append([]byte(nil), r.buf[from-start:to-start]...), truncated
}
