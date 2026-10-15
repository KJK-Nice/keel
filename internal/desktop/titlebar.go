package desktop

import (
	"fmt"
	"log"
	"math"
	"os"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// chromeProbe is one reading of the macOS window chrome, in points with
// AppKit's bottom-left screen origin.
type chromeProbe struct {
	Fullscreen     bool    // the window is in native full screen
	BarSeparate    bool    // the traffic lights live in another window (full screen)
	ScreenTop      float64 // top edge of the window's screen
	MenuBarHeight  float64 // [NSApp.mainMenu menuBarHeight]
	WindowTop      float64 // top edge of the window
	BarTop         float64 // top edge of the window holding the traffic lights
	BarBottom      float64 // bottom edge of that window
	TitlebarBottom float64 // bottom edge of the titlebar view holding the lights
}

// chromeState is what the page needs: full screen or not, and how far the
// menu bar plus titlebar overlay reaches into the window when macOS shows it.
type chromeState struct {
	Fullscreen bool
	Revealed   bool
	Top        int // CSS px the page moves down to clear the overlay
}

// maxOverlayTop bounds the overlay height so a bad reading never shoves
// the page far down. The menu bar plus the inset titlebar is about 62pt
// (74pt under a notch).
const maxOverlayTop = 120

// chromeStateOf turns a probe into the page state.
//
// In full screen macOS parks the toolbar window (menu bar strip plus the
// titlebar with the traffic lights) just above the screen and slides it
// down over the window when the pointer reaches the top. It is revealed
// as soon as its bottom edge enters the window.
//
// Top is where the titlebar's bottom edge ends up once the slide is
// done: the titlebar keeps its offset from the bar's top edge, and the
// bar stops with its top just under the menu bar. Using the end position,
// not the current one, moves the page once instead of chasing the
// animation. If the window itself starts lower (under the menu bar or a
// notch), the overlap shrinks by that much.
func chromeStateOf(p chromeProbe) chromeState {
	if !p.Fullscreen {
		return chromeState{}
	}
	s := chromeState{Fullscreen: true}
	if !p.BarSeparate || p.WindowTop-p.BarBottom <= 0.5 {
		return s
	}
	finalBarTop := p.ScreenTop - p.MenuBarHeight
	finalBottom := p.TitlebarBottom + (finalBarTop - p.BarTop)
	top := int(math.Ceil(p.WindowTop - finalBottom - 0.01))
	if top <= 0 {
		return s
	}
	if top > maxOverlayTop {
		top = maxOverlayTop
	}
	s.Revealed = true
	s.Top = top
	return s
}

// chromeJS hands the state to the page: html[data-fullscreen],
// html[data-titlebar-revealed] and --titlebar-overlay drive keel.css.
func chromeJS(s chromeState) string {
	return fmt.Sprintf(`(function(){var d=document.documentElement;`+
		`d.toggleAttribute("data-fullscreen",%t);`+
		`d.toggleAttribute("data-titlebar-revealed",%t);`+
		`d.style.setProperty("--titlebar-overlay","%dpx");})();`,
		s.Fullscreen, s.Revealed, s.Top)
}

// chromePollEvery is how often the overlay is read while in full screen.
// macOS has no public notification for the menu bar reveal.
const chromePollEvery = 100 * time.Millisecond

// chromeWatcher polls the window chrome only while the window is in full
// screen and pushes changes to the page. One goroutine at most.
type chromeWatcher struct {
	probe func() (chromeProbe, bool)
	push  func(chromeState)
	every time.Duration

	syncMu sync.Mutex // one probe-and-push at a time, so pushes keep probe order
	mu     sync.Mutex
	stop   chan struct{}
	done   chan struct{}
	last   chromeState
	pushed bool
	debug  bool
}

func newChromeWatcher(probe func() (chromeProbe, bool), push func(chromeState)) *chromeWatcher {
	return &chromeWatcher{probe: probe, push: push, every: chromePollEvery, debug: os.Getenv("KEEL_CHROME_DEBUG") == "1"}
}

// sync reads the chrome once and pushes it if it changed (or always,
// with force, after the page reloads). It reports whether the window is
// in full screen.
func (c *chromeWatcher) sync(force bool) bool {
	c.syncMu.Lock()
	defer c.syncMu.Unlock()
	p, ok := c.probe()
	if !ok {
		return false
	}
	s := chromeStateOf(p)
	c.mu.Lock()
	changed := force || !c.pushed || s != c.last
	c.last, c.pushed = s, true
	c.mu.Unlock()
	if c.debug && changed {
		log.Printf("chrome probe %+v -> %+v", p, s)
	}
	if changed {
		c.push(s)
	}
	return s.Fullscreen
}

// start begins polling unless it already runs. The loop ends by itself
// when the window leaves full screen.
func (c *chromeWatcher) start() {
	c.mu.Lock()
	if c.stop != nil {
		c.mu.Unlock()
		return
	}
	stop, done := make(chan struct{}), make(chan struct{})
	c.stop, c.done = stop, done
	c.mu.Unlock()
	go c.loop(stop, done)
}

func (c *chromeWatcher) loop(stop, done chan struct{}) {
	defer close(done)
	defer func() {
		c.mu.Lock()
		if c.stop == stop {
			c.stop, c.done = nil, nil
		}
		c.mu.Unlock()
	}()
	t := time.NewTicker(c.every)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			if !c.sync(false) {
				return
			}
		}
	}
}

// halt stops polling and waits for the loop to end.
func (c *chromeWatcher) halt() {
	c.mu.Lock()
	stop, done := c.stop, c.done
	c.stop, c.done = nil, nil
	c.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	<-done
}

// watchChrome wires the watcher to a window's full-screen and lifecycle
// events. Off macOS probeChrome reports nothing and this does nothing.
func watchChrome(win *application.WebviewWindow) *chromeWatcher {
	c := newChromeWatcher(
		func() (chromeProbe, bool) { return probeChrome(win) },
		func(s chromeState) { evalChromeJS(win, chromeJS(s)) },
	)
	win.OnWindowEvent(events.Mac.WindowDidEnterFullScreen, func(*application.WindowEvent) {
		c.sync(false)
		c.start()
	})
	win.OnWindowEvent(events.Mac.WindowDidExitFullScreen, func(*application.WindowEvent) {
		c.halt()
		c.sync(false)
	})
	win.OnWindowEvent(events.Mac.WebViewDidFinishNavigation, func(*application.WindowEvent) {
		// A reload starts from the windowed layout; give it the current state.
		if c.sync(true) {
			c.start()
		}
	})
	win.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		c.halt()
	})
	return c
}
