package share

import (
	"io"
	"sync"
	"testing"
)

type fakeBridge struct {
	inR  *io.PipeReader
	inW  *io.PipeWriter
	outR *io.PipeReader
	outW *io.PipeWriter
	once sync.Once
}

func newFakeBridge() *fakeBridge {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	return &fakeBridge{inR: inR, inW: inW, outR: outR, outW: outW}
}

func (f *fakeBridge) Read(p []byte) (int, error)  { return f.outR.Read(p) }
func (f *fakeBridge) Write(p []byte) (int, error) { return f.inW.Write(p) }
func (f *fakeBridge) Resize(int, int) error       { return nil }
func (f *fakeBridge) Close() error {
	f.once.Do(func() {
		_ = f.inW.Close()
		_ = f.outW.Close()
		_ = f.inR.Close()
		_ = f.outR.Close()
	})
	return nil
}

func TestFromEnvFallsBackToDefault(t *testing.T) {
	t.Setenv("KEEL_SIGNAL", "")
	old := DefaultSignal
	DefaultSignal = "https://signal.example"
	t.Cleanup(func() { DefaultSignal = old })
	h := FromEnv(nil)
	if h.URL() != "https://signal.example" {
		t.Fatalf("got %q", h.URL())
	}
}

func TestFromEnvOverridesDefault(t *testing.T) {
	t.Setenv("KEEL_SIGNAL", "https://override.example")
	old := DefaultSignal
	DefaultSignal = "https://signal.example"
	t.Cleanup(func() { DefaultSignal = old })
	h := FromEnv(nil)
	if h.URL() != "https://override.example" {
		t.Fatalf("got %q", h.URL())
	}
}
