package store

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// benchStore builds a store sized like a real user's: 60 hosts and 20
// notebooks of 20 cells (~2 KB each), so saves rewrite a realistic keel.json.
func benchStore(b *testing.B) (*Store, Notebook) {
	b.Helper()
	b.Setenv("KEEL_VAULT_PASSPHRASE", "bench-vault-pass")
	b.Setenv("KEEL_VAULT_FAST", "1")
	st, err := New(filepath.Join(b.TempDir(), "keel.json"))
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 60; i++ {
		if _, err := st.UpsertHost(Host{
			Name:     fmt.Sprintf("host-%02d", i),
			Group:    fmt.Sprintf("group-%d", i%6),
			Address:  fmt.Sprintf("10.0.%d.%d", i/250, i%250+1),
			Port:     22,
			Username: "ubuntu",
			AuthType: "password",
			Password: "secret",
			Color:    "#6ea8ff",
		}); err != nil {
			b.Fatal(err)
		}
	}
	body := strings.Repeat("kubectl get pods -A | grep -v Running\n", 55)
	var last Notebook
	for n := 0; n < 20; n++ {
		nb := Notebook{Name: fmt.Sprintf("runbook %d", n)}
		for c := 0; c < 20; c++ {
			nb.Cells = append(nb.Cells, NotebookCell{ID: fmt.Sprintf("c%d", c), Body: body})
		}
		saved, err := st.UpsertNotebook(nb)
		if err != nil {
			b.Fatal(err)
		}
		last = saved
	}
	return st, last
}

// One notebook autosave: full keel.json marshal + temp write + fsync + rename.
func BenchmarkNotebookAutosave(b *testing.B) {
	st, nb := benchStore(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nb.Cells[0].Body = fmt.Sprintf("echo %d\n", i)
		if _, err := st.UpsertNotebook(nb); err != nil {
			b.Fatal(err)
		}
	}
}

// Reads that every /ui request does (Locked + ListHosts), alone.
func BenchmarkReadIdle(b *testing.B) {
	st, _ := benchStore(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = st.Locked()
		if _, err := st.ListHosts(); err != nil {
			b.Fatal(err)
		}
	}
}

// Same reads while a notebook autosave runs continuously in the background:
// shows how long a UI request waits behind the global store lock.
func BenchmarkReadDuringSaves(b *testing.B) {
	st, nb := benchStore(b)
	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		local := nb
		local.Cells = append([]NotebookCell(nil), nb.Cells...)
		for i := 0; !stop.Load(); i++ {
			local.Cells[0].Body = fmt.Sprintf("echo %d\n", i)
			_, _ = st.UpsertNotebook(local)
		}
	}()
	time.Sleep(20 * time.Millisecond)
	var worst time.Duration
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t0 := time.Now()
		_ = st.Locked()
		if _, err := st.ListHosts(); err != nil {
			b.Fatal(err)
		}
		if d := time.Since(t0); d > worst {
			worst = d
		}
	}
	b.StopTimer()
	stop.Store(true)
	wg.Wait()
	b.ReportMetric(float64(worst.Microseconds())/1000, "worst-ms")
}
