package native

import (
	"context"
	"testing"
)

func asyncRig() *workspace {
	w := &workspace{}
	w.work = func(fn func()) { fn() }
	w.post = func(fn func()) { fn() }
	return w
}

func TestCallDropsAnswersAfterCloseAndWhenStale(t *testing.T) {
	w := asyncRig()
	got := 0
	do := func(context.Context) (int, error) { return 7, nil }
	done := func(v int, _ error) { got += v }

	call(w, do, done)
	if got != 7 {
		t.Fatalf("call: got %d", got)
	}
	callIf(w, func() bool { return false }, do, done)
	if got != 7 {
		t.Errorf("a stale answer was taken: %d", got)
	}
	w.closed = true
	call(w, do, done)
	callErr(w, func(context.Context) error { return nil }, func(error) { got = -1 })
	if got != 7 {
		t.Errorf("an answer after close was taken: %d", got)
	}
}

func TestACopyIsWrittenWhateverIsOnScreen(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.w.copy("echo hi", "")
	r.w.copy("host:22", "address")
	r.w.copyQuiet("quiet")
	r.tt.Frame()
	if got := r.tt.Clipboard(); got != "quiet" {
		t.Errorf("clipboard %q", got)
	}
	r.tt.Frame()
	if !r.tt.HasText("Copied address") {
		t.Errorf("toast missing: %v", r.tt.Texts())
	}
}

func TestCallOnceKeepsOneReadInFlightAndClearsItWhenDropped(t *testing.T) {
	w := asyncRig()
	var queued []func()
	w.post = func(fn func()) { queued = append(queued, fn) }
	busy, reads := false, 0
	do := func(context.Context) (int, error) { reads++; return 0, nil }
	callOnce(w, &busy, nil, do, func(int, error) {})
	callOnce(w, &busy, nil, do, func(int, error) {})
	if reads != 1 || !busy {
		t.Fatalf("reads %d busy %v: a second read started", reads, busy)
	}
	w.closed = true
	for _, fn := range queued {
		fn()
	}
	if busy {
		t.Error("the in-flight flag stayed set when the answer was dropped")
	}
}
