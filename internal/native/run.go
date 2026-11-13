package native

import (
	"embed"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"keel/internal/authtoken"
	"keel/internal/native/keelclient"
	"keel/internal/paths"
)

// terminalTheme is Keel's terminal palette from the tokens of the theme in use.
func terminalTheme() *terminal.Theme {
	return terminalColors(accent, termFg, termBg, ansi)
}

//go:embed fonts/*.woff2
var fontFiles embed.FS

// registerFonts adds the embedded JetBrains Mono and IBM Plex Mono (woff2) as their families. fonts/ holds the
// eight woff2 files and the two OFL licences (TestEmbeddedFontsAreTheShippedSet).
func registerFonts() {
	if platform == "linux" {
		// Not on Linux (issue #140, plan 037 round 2): on Ubuntu a registered family measured about 1.8 times too wide (29
		// characters of JetBrains Mono at 13 px came out 399 px, the system monospace 232 and the font's own advance gives
		// about 226), which made the terminal's cells about twice as wide as its glyphs, and JetBrains Mono and IBM Plex Mono
		// rendered alike. The stacks fall back to the system monospace until MyGo's Linux registration is understood.
		return
	}
	files, _ := fontFiles.ReadDir("fonts")
	for _, f := range files {
		family := monoFamily
		if strings.HasPrefix(f.Name(), "ibm-plex-mono") {
			family = plexFamily
		}
		b, err := fontFiles.ReadFile("fonts/" + f.Name())
		if err == nil {
			err = ui.RegisterFont(b, family)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "font "+f.Name()+":", err)
		}
	}
}

// liveBodyT gives p a libghostty terminal on its connection; transparent leaves the terminal's background undrawn so the pane's translucent
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
	th := terminalTheme() // the same colours in a light and a dark window: the theme is Keel's choice, not the window's
	term, err := terminal.New(terminal.Options{
		Conn: newSyncConn(p), Theme: th, DarkTheme: th, Transparent: transparent, CopyOnSelect: copySel, RightClick: right,
		// A noticed line or a card takes ⇥, ⇧⇥, esc, the option keys and ⌘Z first; every other key goes on at once (suggestKey).
		OnKey: func(mods ui.Modifiers, key ui.Key) bool { return p.keyHook != nil && p.keyHook(mods, key) },
		Font:  terminal.Font{Family: p.fontFamily, Size: p.fontSize}, Padding: terminal.Padding{X: termPadX, Y: termPadY}, // the plugin's own default, written out: suggestgeom.go places marks by it
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
	p.sg.cursor = func() (int, string, bool) {
		_, rows := term.Size()
		return cursorRow(string(term.Snapshot()), rows)
	}
	p.screen = func() (bool, string) {
		_, rows := term.Size()
		return screenOf(string(term.Snapshot()), rows)
	}
	p.release = func() { term.Close() }
	p.setCopy = func(on bool) {
		if on {
			term.SetCopyOnSelect(terminal.CopyOnSelectClipboard)
		} else {
			term.SetCopyOnSelect(terminal.CopyOnSelectNone)
		}
	}
	p.setTheme = func() { t := terminalTheme(); term.SetTheme(t, t) }
	p.setFont = func(size float32) { term.SetFont(terminal.Font{Family: p.fontFamily, Size: size}) }
	p.search = &termSearch{
		start: term.Search, next: term.SearchNext, prev: term.SearchPrevious,
		status: func() (int, int) { s := term.SearchStatus(); return s.Selected, s.Total },
	}
}

// Options say which keel serve the window talks to. Addr and DataDir are the ones internal/paths gives (Run), or
// what keel app already prepared. AppBuild is the build id of the app (internal/buildid); "" never warns.
type Options struct {
	Addr, DataDir, AppBuild string
}

// Run opens the window on a running keel serve (the address and data directory of internal/paths) and blocks
// until the app quits. It does not start keel serve.
func Run() error {
	return RunWith(Options{Addr: paths.Addr(), DataDir: paths.DataDir()})
}

// RunWith is Run for the given address and data directory.
func RunWith(o Options) error {
	tok, err := authtoken.Read(o.DataDir)
	if err != nil {
		return fmt.Errorf("read the keel serve token (is keel serve running?): %w", err)
	}
	cl := keelclient.NewAt(o.Addr, tok)
	pr := loadPrefs(o.DataDir)
	registerFonts()
	w := newWorkspace(clientBackend{cl})
	w.appBuild = o.AppBuild
	w.dataDir, w.prefs = o.DataDir, pr
	// Transparent cannot change on an open window: Window opacity applies live only when it starts below 100
	// (settings.go).
	translucent := pr.Opacity < opacityMax
	w.translucentWin = translucent
	if translucent {
		w.alpha = pr.alpha()
	}
	w.mkBody = func(p *pane) { liveBodyT(p, translucent) }
	if w.themeNative { // the window takes the theme as its own appearance, so its chrome and backdrop follow it
		w.appearanceHook = func(choice string) { go mygo.Theme.SetSource(mygo.ThemeSource(choice)) }
		mygo.Theme.SetSource(mygo.ThemeSource(pr.Theme))
	}

	mygo.App.SetWindowStateStore(newWindowStateStore(o.DataDir))
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title: "Keel", Width: 1280, Height: 800, StateKey: "main",
			TitleBarStyle: mygo.TitleBarHidden,
			// Hidden, not HiddenInset: HiddenInset's toolbar makes a taller bar than sizeTitlebar. The position
			// centres the lights vertically in that row; x is the usual window padding.
			TrafficLightPosition: &mygo.Point{X: space3, Y: (sizeTitlebar - trafficLightSize) / 2},
			Transparent:          translucent,
			Content:              ui.View(w.view),
		})
		w.post = win.Update
		w.saveDialog = func(name string) (string, error) {
			return mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: win, Title: "Save " + name, DefaultPath: name, CreateDirectories: true})
		}
		w.cornerR = windowCornerRadius()
		// The window has no shadow (plan 037 part C).
		win.OnShow(func() { setWindowHasShadow(win, false) })
		if translucent { // the blur is set when the window shows, and again whenever Settings changes it
			blur := func(radius int) { blurOnce(win, radius)() }
			w.blurHook = blur
			win.OnShow(func() { w.post(w.applyBlur) })
		}
		win.OnClosed(w.closeWindow)
		(&shell{w: w, win: win}).start()
		w.start()
		w.startPoll(pollEvery)
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
