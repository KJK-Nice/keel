package runtime

import "testing"

// Scrollback ring under steady terminal output. Chunk sizes mimic small SSH
// reads (interactive echo) and large PTY reads (cat / build logs).
func benchRingWrite(b *testing.B, chunk int) {
	r := newRing(256 << 10)
	p := make([]byte, chunk)
	for i := range p {
		p[i] = 'a' + byte(i%26)
	}
	// Fill so every measured write is in the "ring is full" state.
	for i := 0; i < (512<<10)/chunk+1; i++ {
		r.Write(p)
	}
	b.SetBytes(int64(chunk))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Write(p)
	}
}

func BenchmarkRingWrite64B(b *testing.B)  { benchRingWrite(b, 64) }
func BenchmarkRingWrite1KB(b *testing.B)  { benchRingWrite(b, 1<<10) }
func BenchmarkRingWrite32KB(b *testing.B) { benchRingWrite(b, 32<<10) }
