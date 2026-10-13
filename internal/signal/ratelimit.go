package signal

import (
	"sync"
	"time"
)

// rateLimiter is a per-key token bucket: burst tokens, refilled one per
// interval.
type rateLimiter struct {
	mu       sync.Mutex
	burst    float64
	interval time.Duration
	buckets  map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter(burst int, interval time.Duration) *rateLimiter {
	return &rateLimiter{burst: float64(burst), interval: interval, buckets: map[string]*bucket{}}
}

func (l *rateLimiter) allow(key string, now time.Time) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	if b == nil {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens += float64(now.Sub(b.last)) / float64(l.interval)
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// sweep drops buckets that have refilled completely, bounding memory.
func (l *rateLimiter) sweep(now time.Time) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	full := time.Duration(l.burst) * l.interval
	for k, b := range l.buckets {
		if now.Sub(b.last) > full {
			delete(l.buckets, k)
		}
	}
}
