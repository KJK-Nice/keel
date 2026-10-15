package desktop

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// Readings taken from a real full-screen Keel window on a 1920x1080
// display (points, bottom-left origin): the toolbar window is 54pt tall,
// its titlebar view ends 16pt above its bottom, the menu bar is 24pt.
func fsProbe(barTop float64) chromeProbe {
	return chromeProbe{
		Fullscreen:     true,
		BarSeparate:    true,
		ScreenTop:      1080,
		MenuBarHeight:  24,
		WindowTop:      1080,
		BarTop:         barTop,
		BarBottom:      barTop - 54,
		TitlebarBottom: barTop - 38,
	}
}

func TestChromeStateOf(t *testing.T) {
	cases := []struct {
		name string
		p    chromeProbe
		want chromeState
	}{
		{
			name: "windowed",
			p:    chromeProbe{WindowTop: 947, ScreenTop: 1080, MenuBarHeight: 24},
			want: chromeState{},
		},
		{
			name: "full screen, menu bar hidden: bar parked above the screen",
			p:    fsProbe(1134),
			want: chromeState{Fullscreen: true},
		},
		{
			name: "full screen, revealed: titlebar ends 62pt down",
			p:    fsProbe(1056),
			want: chromeState{Fullscreen: true, Revealed: true, Top: 62},
		},
		{
			name: "mid-slide down: already the end position",
			p:    fsProbe(1118),
			want: chromeState{Fullscreen: true, Revealed: true, Top: 62},
		},
		{
			name: "mid-slide, bar barely in",
			p:    fsProbe(1080 + 54 - 1),
			want: chromeState{Fullscreen: true, Revealed: true, Top: 62},
		},
		{
			name: "window starts under the menu bar: only the titlebar overlaps",
			p: chromeProbe{
				Fullscreen: true, BarSeparate: true, ScreenTop: 1080, MenuBarHeight: 24,
				WindowTop: 1056, BarTop: 1056, BarBottom: 1002, TitlebarBottom: 1018,
			},
			want: chromeState{Fullscreen: true, Revealed: true, Top: 38},
		},
		{
			name: "notch: taller menu bar",
			p: chromeProbe{
				Fullscreen: true, BarSeparate: true, ScreenTop: 982, MenuBarHeight: 37,
				WindowTop: 982, BarTop: 945, BarBottom: 891, TitlebarBottom: 907,
			},
			want: chromeState{Fullscreen: true, Revealed: true, Top: 75},
		},
		{
			name: "full screen but the lights are still in the window",
			p:    chromeProbe{Fullscreen: true, WindowTop: 1080, ScreenTop: 1080},
			want: chromeState{Fullscreen: true},
		},
		{
			name: "bad reading is capped",
			p: chromeProbe{
				Fullscreen: true, BarSeparate: true, ScreenTop: 1080, MenuBarHeight: 24,
				WindowTop: 1080, BarTop: 1056, BarBottom: 700, TitlebarBottom: 700,
			},
			want: chromeState{Fullscreen: true, Revealed: true, Top: maxOverlayTop},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := chromeStateOf(tc.p); got != tc.want {
				t.Fatalf("chromeStateOf = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestChromeJSSetsPageState(t *testing.T) {
	js := chromeJS(chromeState{Fullscreen: true, Revealed: true, Top: 62})
	for _, want := range []string{
		`toggleAttribute("data-fullscreen",true)`,
		`toggleAttribute("data-titlebar-revealed",true)`,
		`setProperty("--titlebar-overlay","62px")`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("chromeJS missing %q in %s", want, js)
		}
	}
	js = chromeJS(chromeState{})
	if !strings.Contains(js, `"data-fullscreen",false`) || !strings.Contains(js, `"data-titlebar-revealed",false`) {
		t.Fatalf("windowed chromeJS must clear both attributes: %s", js)
	}
}

type fakeChrome struct {
	mu     sync.Mutex
	probes []chromeProbe // served in order; the last one repeats
	pushed []chromeState
	calls  int
}

func (f *fakeChrome) probe() (chromeProbe, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	p := f.probes[0]
	if len(f.probes) > 1 {
		f.probes = f.probes[1:]
	}
	return p, true
}

func (f *fakeChrome) push(s chromeState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pushed = append(f.pushed, s)
}

func (f *fakeChrome) snapshot() ([]chromeState, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]chromeState(nil), f.pushed...), f.calls
}

func newTestWatcher(f *fakeChrome) *chromeWatcher {
	c := newChromeWatcher(f.probe, f.push)
	c.every = time.Millisecond
	return c
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestChromeWatcherPushesOnlyChanges(t *testing.T) {
	hidden, revealed := fsProbe(1134), fsProbe(1056)
	f := &fakeChrome{probes: []chromeProbe{hidden, hidden, revealed, revealed, revealed, hidden, {WindowTop: 947}}}
	c := newTestWatcher(f)
	c.sync(false)
	c.start()
	waitFor(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.stop == nil
	})
	got, _ := f.snapshot()
	want := []chromeState{
		{Fullscreen: true},
		{Fullscreen: true, Revealed: true, Top: 62},
		{Fullscreen: true},
		{},
	}
	if len(got) != len(want) {
		t.Fatalf("pushed %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("push %d = %+v, want %+v (all %+v)", i, got[i], want[i], got)
		}
	}
}

func TestChromeWatcherStopsPollingOutsideFullScreen(t *testing.T) {
	f := &fakeChrome{probes: []chromeProbe{{WindowTop: 947}}}
	c := newTestWatcher(f)
	c.start()
	waitFor(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.stop == nil
	})
	_, calls := f.snapshot()
	time.Sleep(20 * time.Millisecond)
	if _, after := f.snapshot(); after != calls {
		t.Fatalf("kept polling after leaving full screen: %d -> %d probes", calls, after)
	}
}

func TestChromeWatcherHaltAndSingleLoop(t *testing.T) {
	f := &fakeChrome{probes: []chromeProbe{fsProbe(1134)}}
	c := newTestWatcher(f)
	c.start()
	c.mu.Lock()
	first := c.done
	c.mu.Unlock()
	c.start() // already running: no second loop
	c.mu.Lock()
	second := c.done
	c.mu.Unlock()
	if first != second {
		t.Fatal("start ran a second loop")
	}
	waitFor(t, func() bool { _, n := f.snapshot(); return n > 3 })
	c.halt()
	select {
	case <-first:
	default:
		t.Fatal("halt returned before the loop ended")
	}
	_, calls := f.snapshot()
	time.Sleep(20 * time.Millisecond)
	if _, after := f.snapshot(); after != calls {
		t.Fatalf("polling after halt: %d -> %d", calls, after)
	}
	c.halt() // idempotent
}

func TestChromeWatcherForceResendsAfterReload(t *testing.T) {
	f := &fakeChrome{probes: []chromeProbe{fsProbe(1134)}}
	c := newTestWatcher(f)
	c.sync(false)
	c.sync(false)
	c.sync(true)
	if got, _ := f.snapshot(); len(got) != 2 {
		t.Fatalf("pushes = %+v, want first state plus the forced resend", got)
	}
}
