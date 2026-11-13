package native

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Plan 032 section 10: Settings · Appearance · Window shadow.

func TestWindowShadowPrefDefaultsOnAndIsRead(t *testing.T) {
	dir := t.TempDir()
	if loadPrefs(dir).NoShadow {
		t.Error("an absent windowShadow must be on")
	}
	for body, wantOff := range map[string]bool{`{"windowShadow":false}`: true, `{"windowShadow":true}`: false, `{"windowShadow":"no"}`: false, `{}`: false} {
		if err := os.WriteFile(filepath.Join(dir, "keel-native.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := loadPrefs(dir).NoShadow; got != wantOff {
			t.Errorf("%s: NoShadow %v, want %v", body, got, wantOff)
		}
	}
}

func TestWindowShadowRowAndSwitch(t *testing.T) {
	r := settingsRig(t, settingsBackend())
	if !hasShadowSwitch() {
		if r.has("Window shadow switch") {
			t.Fatal("the Window shadow row shows where the window cannot have the switch")
		}
		return
	}
	r.texts(t, "Window shadow", "The shadow around the window. With a translucent window the shadow shows faintly through it; turn it off for a clean background.")
	var calls []bool
	r.w.shadowSetHook = func(on bool) { calls = append(calls, on) }
	r.click(t, "Window shadow switch")
	if !r.w.win.NoShadow || string(r.prefsFile(t)["windowShadow"]) != "false" || len(calls) != 1 || calls[0] {
		t.Errorf("off: NoShadow %v file %v calls %v", r.w.win.NoShadow, r.prefsFile(t), calls)
	}
	r.click(t, "Window shadow switch")
	if r.w.win.NoShadow || string(r.prefsFile(t)["windowShadow"]) != "true" || len(calls) != 2 || !calls[1] {
		t.Errorf("on again: NoShadow %v file %v calls %v", r.w.win.NoShadow, r.prefsFile(t), calls)
	}
}

func TestWindowShadowRowIsFoundBySearch(t *testing.T) {
	if !hasShadowSwitch() {
		t.Skip("the row is macOS only")
	}
	r := settingsRig(t, settingsBackend())
	r.w.pageTab("settings").pg.settings.search = "shadow"
	r.tt.Frame()
	r.texts(t, "Window shadow")
	if r.has("Window opacity") {
		t.Error("the search for shadow still lists Window opacity")
	}
}

// With the shadow off nothing is armed; with it on, frames of a change arm it once, and idle frames (the caret, the poll) never.
func TestShadowHookArmsOnlyOnAChangeAndNeverWhenOff(t *testing.T) {
	old := shadowDelay
	shadowDelay = 5 * time.Millisecond
	defer func() { shadowDelay = old }()

	r := newRig(t, settingsBackend())
	n := 0
	r.w.shadowHook = func() { n++ }
	// The timer posts from its own goroutine; post is the UI thread's, so the test queues what is posted and runs it
	// itself in settle, as the UI thread would (running it on the timer's goroutine raced with the frames).
	var mu sync.Mutex
	var queue []func()
	r.w.post = func(f func()) { mu.Lock(); queue = append(queue, f); mu.Unlock() }
	settle := func() {
		time.Sleep(30 * time.Millisecond)
		mu.Lock()
		run := queue
		queue = nil
		mu.Unlock()
		for _, f := range run {
			f()
		}
	}
	t.Cleanup(func() {
		if r.w.shadowTimer != nil {
			r.w.shadowTimer.Stop()
		}
	})

	r.w.win.NoShadow = true
	for i := 0; i < 5; i++ {
		r.w.shadowGen++
		r.tt.Frame()
	}
	settle()
	if r.w.shadowTimer != nil || n != 0 {
		t.Fatalf("armed with the shadow off: timer %v, %d calls", r.w.shadowTimer, n)
	}

	r.w.win.NoShadow = false
	r.tt.Frame() // the first frame of a shape: due
	settle()
	r.tt.Frame()
	settle()
	if n != 1 {
		t.Fatalf("one shape, %d calls", n)
	}
	for i := 0; i < 20; i++ { // idle frames
		r.tt.Frame()
	}
	settle()
	if n != 1 {
		t.Fatalf("idle frames cut the shadow again: %d calls", n)
	}
	r.w.shadowGen++ // a user action ran
	r.tt.Frame()
	settle()
	if n != 2 {
		t.Fatalf("a change did not cut the shadow: %d calls", n)
	}
}
