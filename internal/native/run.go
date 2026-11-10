package native

import (
	"fmt"
	"os"
	"path/filepath"
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

// registerFonts adds JetBrains Mono (woff2) as the mono family, read from cmd/keel/web/fonts when that
// directory is under the working directory; otherwise the system monospace is used. Embedding is phase N2.
func registerFonts() {
	dir := filepath.Join("cmd", "keel", "web", "fonts")
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return
	}
	for _, wt := range []string{"400", "500"} {
		b, err := os.ReadFile(filepath.Join(dir, "jetbrains-mono-latin-"+wt+"-normal.woff2"))
		if err == nil {
			err = ui.RegisterFont(b, monoFamily)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "font "+wt+":", err)
		}
	}
}

// liveBody gives p a libghostty terminal on its connection.
func liveBody(p *pane) { liveBodyT(p, false) }

// liveBodyT is liveBody; transparent leaves the terminal's background undrawn so the pane's translucent
// ground shows through.
func liveBodyT(p *pane, transparent bool) {
	term, err := terminal.New(terminal.Options{
		Conn: p.conn, Theme: terminalTheme(), Transparent: transparent,
		Font: terminal.Font{Family: monoStack, Size: uiFontSize},
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
	translucent := pr.Opacity < opacityMax
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
		win.OnClosed(w.closeWindow)
		w.start()
		if path := os.Getenv("KEEL_NATIVE_CAPTURE"); path != "" {
			capture(win, w, path)
		}
	})
	return mygo.App.Run()
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
