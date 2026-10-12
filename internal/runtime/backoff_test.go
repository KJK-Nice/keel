package runtime

import (
	"testing"
	"time"
)

func TestReconnectDelay(t *testing.T) {
	cases := []struct {
		unit time.Duration
		n    int
		want time.Duration
	}{
		{0, 0, time.Second},
		{time.Second, 0, time.Second},
		{time.Second, 1, 2 * time.Second},
		{time.Second, 3, 8 * time.Second},
		{time.Second, 4, maxReconnectDelay},
		{10 * time.Millisecond, 2, 40 * time.Millisecond},
		{time.Minute, 0, maxReconnectDelay},
	}
	for _, c := range cases {
		if got := reconnectDelay(c.unit, c.n); got != c.want {
			t.Errorf("reconnectDelay(%v, %d) = %v, want %v", c.unit, c.n, got, c.want)
		}
	}
}

// A host that stays down keeps the attempt counter growing. The delay must
// stay at the cap instead of overflowing into a zero or negative wait.
func TestReconnectDelayNeverOverflows(t *testing.T) {
	// 1s<<34 is the first shift that overflowed; 1s<<55 and beyond gave 0.
	for _, n := range []int{34, 40, 55, 63, 64} {
		if got := reconnectDelay(time.Second, n); got != maxReconnectDelay {
			t.Fatalf("reconnectDelay(1s, %d) = %v, want %v", n, got, maxReconnectDelay)
		}
	}
	for _, unit := range []time.Duration{time.Nanosecond, time.Millisecond, time.Second} {
		for n := 64; n <= 200; n++ {
			if got := reconnectDelay(unit, n); got != maxReconnectDelay {
				t.Fatalf("reconnectDelay(%v, %d) = %v, want %v", unit, n, got, maxReconnectDelay)
			}
		}
	}
}
