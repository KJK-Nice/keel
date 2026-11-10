package native

import (
	"embed"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
	"keel/internal/native/layout"
	"keel/internal/paths"
)

// terminalTheme is Keel's terminal palette from the tokens.
func terminalTheme() *terminal.Theme {
	th := &terminal.Theme{
		Foreground: tok(hexTermFg), Background: tok(hexTermBg),
		Cursor: tok(hexTermCursor), Selection: termSelection,
	}
	for i, h := range hexAnsi {
		th.Palette[i] = tok(h)
	}
	return th
}

//go:embed fonts/*.woff2
var fontFiles embed.FS

// registerFonts adds the embedded JetBrains Mono (woff2) as the mono family. fonts/ is a copy of
// cmd/keel/web/fonts (TestEmbeddedFontsMatchTheWebviews keeps them equal).
func registerFonts() {
	files, _ := fontFiles.ReadDir("fonts")
	for _, f := range files {
		b, err := fontFiles.ReadFile("fonts/" + f.Name())
		if err == nil {
			err = ui.RegisterFont(b, monoFamily)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "font "+f.Name()+":", err)
		}
	}
}

// liveBody gives p a libghostty terminal on its connection.
func liveBody(p *pane) { liveBodyT(p, false) }

// liveBodyT is liveBody; transparent leaves the terminal's background undrawn so the pane's translucent
// ground shows through.
func liveBodyT(p *pane, transparent bool) {
	copySel := terminal.CopyOnSelectNone
	if p.copyOnSelect {
		copySel = terminal.CopyOnSelectClipboard
	}
	right := terminal.RightClickContextMenu
	switch p.rightClick {
	case rightPaste:
		right = terminal.RightClickPaste
	case rightNothing:
		right = terminal.RightClickIgnore
	}
	term, err := terminal.New(terminal.Options{
		Conn: p.conn, Theme: terminalTheme(), Transparent: transparent, CopyOnSelect: copySel, RightClick: right,
		Font: terminal.Font{Family: monoStack, Size: p.fontSize},
		// Text size is the workspace's, for every pane (⌘+ ⌘- ⌘0): the wheel's zoom asks it too.
		OnZoom: func(z terminal.Zoom) bool {
			if p.zoom != nil {
				p.zoom(z.Steps, z.Reset)
			}
			return true
		},
	})
	if err != nil {
		msg := "terminal: " + err.Error()
		p.view = func(c *ui.Context) ui.Element {
			return ui.Box(c).Fill().Focusable().Children(func() { body(c, msg, flare) })
		}
		p.size = func() (int, int) { return 0, 0 }
		return
	}
	p.view = func(c *ui.Context) ui.Element { return terminal.View(c, term).Fill() }
	p.size = term.Size
	p.release = func() { term.Close() }
	p.setCopy = func(on bool) {
		if on {
			term.SetCopyOnSelect(terminal.CopyOnSelectClipboard)
		} else {
			term.SetCopyOnSelect(terminal.CopyOnSelectNone)
		}
	}
	p.setFont = func(size float32) { term.SetFont(terminal.Font{Family: monoStack, Size: size}) }
	p.search = &termSearch{
		start: term.Search, next: term.SearchNext, prev: term.SearchPrevious,
		status: func() (int, int) { s := term.SearchStatus(); return s.Selected, s.Total },
	}
}

// Run opens the window on a running keel serve and blocks until the app quits.
func Run() error {
	cl, err := keelclient.New()
	if err != nil {
		return err
	}
	pr, err := loadPrefs(paths.DataDir()).withEnv()
	if err != nil {
		return err
	}
	registerFonts()
	w := newWorkspace(clientBackend{cl})
	w.dataDir, w.termSize = paths.DataDir(), pr.TermSize
	w.win, w.term = pr, loadTermPrefs(paths.DataDir())
	// Transparent cannot change on an open window: Window opacity applies live only when it starts below 100
	// (settings.go).
	translucent := pr.Opacity < opacityMax
	w.translucentWin = translucent
	if translucent {
		w.alpha = pr.alpha()
	}
	w.mkBody = func(p *pane) { liveBodyT(p, translucent) }

	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title: "Keel", Width: 1280, Height: 800,
			TitleBarStyle: mygo.TitleBarHidden,
			// Hidden, not HiddenInset: HiddenInset's toolbar makes a taller bar than sizeTitlebar. The position
			// centres the lights vertically in that row; x is the usual window padding.
			TrafficLightPosition: &mygo.Point{X: space3, Y: (sizeTitlebar - trafficLightSize) / 2},
			Transparent:          translucent,
			Content:              ui.View(w.view),
		})
		w.post = win.Update
		if translucent { // the blur is set when the window shows, and again whenever Settings changes it
			blur := func(radius int) { blurOnce(win, radius)() }
			w.blurHook = blur
			win.OnShow(func() { w.post(w.applyBlur) })
		}
		win.OnClosed(w.closeWindow)
		w.start()
		w.startPoll(pollEvery)
		if path := os.Getenv("KEEL_NATIVE_CAPTURE"); path != "" {
			capture(win, w, path)
		}
	})
	return mygo.App.Run()
}

// blurOnce returns a function that blurs win in a goroutine (it waits for the main thread) and logs a
// failure once to stderr. A failure never stops the app: the window is just not blurred.
func blurOnce(win *mygo.Window, radius int) func() {
	var once sync.Once
	return func() {
		go func() {
			if err := setWindowBlur(win, radius); err != nil {
				once.Do(func() { fmt.Fprintln(os.Stderr, "keel-native: window blur:", err) })
			}
		}()
	}
}

// capture is for looking at the screen: it opens a local host split to the right, waits, writes
// a PNG of the page to path and quits.
func capture(win *mygo.Window, w *workspace, path string) {
	var once sync.Once
	win.OnShow(func() {
		once.Do(func() {
			time.AfterFunc(2*time.Second, func() {
				win.Update(func() {
					if len(w.tabs) == 0 {
						w.newTab()
					}
				})
				time.AfterFunc(3*time.Second, func() {
					win.Update(func() { w.splitFocused(layout.Row) })
					time.AfterFunc(4*time.Second, func() {
						png, err := win.CapturePage()
						if err == nil {
							err = os.WriteFile(path, png, 0o644)
						}
						if err != nil {
							fmt.Fprintln(os.Stderr, "capture:", err)
						}
						mygo.App.Quit()
					})
				})
			})
		})
	})
}
