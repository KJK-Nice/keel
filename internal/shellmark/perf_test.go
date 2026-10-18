package shellmark

import (
	"bytes"
	"testing"
	"time"
)

// Text full of 0xC2 (°, ·, NBSP, ±, « ») must parse in linear time: the
// runtime feeds the parser under the session lock, so a scan that is
// quadratic in the chunk would stall every client of the pane. Feeding 4×
// the bytes in one chunk must take about 4× the time (quadratic would be
// 16×), whatever the machine or -race costs per byte.
func TestFeedIsLinearOnC2HeavyText(t *testing.T) {
	best := func(b []byte) time.Duration {
		var min time.Duration
		for range 5 {
			var p Parser
			start := time.Now()
			p.Feed(b, 0, func(Mark) {})
			if d := time.Since(start); min == 0 || d < min {
				min = d
			}
		}
		return min
	}
	for name, unit := range map[string]string{"degrees": "°", "mixed": "° \x1b[0m·"} {
		small := bytes.Repeat([]byte(unit), (512<<10)/len(unit))
		large := bytes.Repeat([]byte(unit), (2<<20)/len(unit))
		s, l := best(small), best(large)
		if l > 8*s+5*time.Millisecond {
			t.Errorf("%s: 512 KiB %v, 2 MiB %v: not linear", name, s, l)
		}
	}
}

func BenchmarkFeed(b *testing.B) {
	for name, in := range map[string][]byte{
		"ascii":   bytes.Repeat([]byte("x"), 1<<20),
		"degrees": bytes.Repeat([]byte("°"), 1<<19),
		"marks":   bytes.Repeat([]byte(osc("133;A")+"$ "+osc("133;C")+"out\r\n"+osc("133;D;0")), 1<<14),
	} {
		b.Run(name, func(b *testing.B) {
			b.SetBytes(int64(len(in)))
			for b.Loop() {
				var p Parser
				p.Feed(in, 0, func(Mark) {})
			}
		})
	}
}
