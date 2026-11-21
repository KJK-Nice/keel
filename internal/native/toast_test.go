package native

import (
	"testing"
	"time"
)

func TestMarqueeOffset(t *testing.T) {
	const over = 80 // two seconds across at 40 DIPs a second
	move := 2 * time.Second
	for _, c := range []struct {
		at   time.Duration
		want float32
	}{
		{0, 0},
		{marqueeWait - time.Millisecond, 0},
		{marqueeWait + move/2, -40},
		{marqueeWait + move, -80},
		{2*marqueeWait + move - time.Millisecond, -80},
		{2*marqueeWait + move, 0}, // back to the start, round again
		{3*marqueeWait + move + move/2, -40},
	} {
		if got := marqueeOffset(c.at, over); got < c.want-0.5 || got > c.want+0.5 {
			t.Errorf("at %v: %v, want %v", c.at, got, c.want)
		}
	}
	if marqueeOffset(10*time.Second, 0) != 0 || marqueeOffset(10*time.Second, -5) != 0 {
		t.Error("a title that fits moved")
	}
}
