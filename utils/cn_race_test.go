package utils

import (
	"sync"
	"testing"
)

// TestCNConcurrentFirstUse is the regression test for #62. tailwind-merge-go
// builds its merger lazily on the first call without synchronization, so
// concurrent first calls of CN (parallel templ renders) raced under -race.
// The race needs CN's first use in the process to be concurrent, so no other
// test in this package may call CN before this one runs (Go runs tests in
// source order, file by file in name order).
func TestCNConcurrentFirstUse(t *testing.T) {
	inputs := [][]any{
		{"p-2", "p-4"},
		{"bg-primary hover:bg-muted", "bg-card"},
		{"px-(--card-spacing)", "px-2"},
		{"p-2 underline!", map[string]bool{"p-4": true}},
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	got := make([][]string, 16)
	for g := range got {
		got[g] = make([]string, len(inputs))
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i, in := range inputs {
				got[g][i] = CN(in...)
			}
		}()
	}
	close(start)
	wg.Wait()
	for i, in := range inputs {
		want := CN(in...)
		for g := range got {
			if got[g][i] != want {
				t.Errorf("goroutine %d, input %d: got %q, want %q", g, i, got[g][i], want)
			}
		}
	}
}

// BenchmarkCNParallel measures CN under concurrent renders.
func BenchmarkCNParallel(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			CN("flex items-center gap-2 rounded-md px-2 py-1.5", "px-(--card-spacing)", map[string]bool{"bg-card": true})
		}
	})
}
