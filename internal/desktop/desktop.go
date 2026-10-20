package desktop

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/url"
	stdruntime "runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:shell
var shellFS embed.FS

//go:embed macos/icon.png
var appIcon []byte

type nativeWindow struct {
	url  string
	app  *application.App
	win  *application.WebviewWindow
	exec jsExec // runs script in the page; nil until the window exists
}

func Run(url, _ string) error {
	assets, err := fs.Sub(shellFS, "shell")
	if err != nil {
		return err
	}
	w := &nativeWindow{url: desktopURL(url, stdruntime.GOOS)}
	app := application.New(application.Options{
		Name:        "Keel",
		Description: "Local SSH workspace",
		Icon:        appIcon,
		Assets: application.AssetOptions{
			Handler:        application.AssetFileServerFS(assets),
			DisableLogging: true,
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		Linux: application.LinuxOptions{
			ProgramName: "Keel",
		},
		// The page's theme choice, for the window appearance (appearance.go).
		RawMessageHandler: w.rawMessage,
	})
	w.app = app
	app.Menu.Set(w.appMenu())
	win := app.Window.NewWithOptions(webviewOptions(w.url))
	w.win = win
	w.exec = windowJS(win)
	// windowDragJS is the window's JS option (webviewOptions): Wails runs it
	// itself after every page load on macOS and Linux, outside the ExecJS
	// queue. A WindowRuntimeReady hook never fired: the page has no runtime.
	if stdruntime.GOOS == "darwin" {
		watchChrome(win)
	}
	return app.Run()
}

func (w *nativeWindow) appMenu() *application.Menu {
	m := application.NewMenu()
	// The macOS roles build their items from Wails' global application,
	// which is nil until application.New runs; skip them without an app.
	macRoles := stdruntime.GOOS == "darwin" && w.app != nil
	if macRoles {
		m.AddRole(application.AppMenu)
	}
	w.addItems(m.AddSubmenu("File"), fileMenu)
	w.addItems(m.AddSubmenu("View"), viewMenu(stdruntime.GOOS))
	m.AddRole(application.EditMenu)
	if macRoles {
		m.AddRole(application.WindowMenu)
	}
	return m
}

// agentPanelAccel is the agent panel's chord: ⌘I on macOS. Elsewhere the
// app modifier is Ctrl, and Ctrl+I is Tab in a terminal (readline
// completion), so the panel takes Ctrl+Shift+I and Ctrl+I stays with the
// shell. lib/shortcuts.js maps the same chords (shortcuts_test.go).
func agentPanelAccel(goos string) string {
	if goos == "darwin" {
		return "CmdOrCtrl+i"
	}
	return "Ctrl+Shift+i"
}

// menuEntry is one item of a Keel submenu; an empty label is a separator.
// action is a window.__keelShortcut name (runShortcut in term.js), or
// actionQuit, which Go handles itself.
type menuEntry struct {
	label  string
	accel  string
	action string
}

// actionQuit closes the window, and the app quits with its last window.
const actionQuit = "quit"

var fileMenu = []menuEntry{
	{"New Tab", "CmdOrCtrl+t", "new-tab"},
	{"Close Tab", "CmdOrCtrl+w", "close-tab"},
	{"Reopen Tab", "CmdOrCtrl+Shift+t", "reopen-tab"},
	{},
	{"New Host", "CmdOrCtrl+n", "new-host"},
	{"Quick Connect", "CmdOrCtrl+Shift+o", "quick-connect"},
	{"Join Session", "CmdOrCtrl+j", "join"},
	{},
	{"Find Host", "", "search"}, // no ⌘K: that is the command palette
	{},
	{"Split Right", "CmdOrCtrl+Shift+d", "split-right"},
	{"Split Down", "CmdOrCtrl+Shift+e", "split-down"},
	{"Split Notebook", "", "split-notebook"},
	{},
	{"Close Window", "CmdOrCtrl+Shift+w", actionQuit},
	{"Stop Keel runtime", "", "stop-runtime"},
}

func viewMenu(goos string) []menuEntry {
	return []menuEntry{
		{"Find in Terminal", "CmdOrCtrl+f", "find-term"},
		{"Toggle Vault Sidebar", "CmdOrCtrl+b", "toggle-sidebar"},
		{"Toggle Agent Panel", agentPanelAccel(goos), "toggle-agent"},
		{},
		{"Focus View", "CmdOrCtrl+Shift+enter", "focus-pane"},
		{},
		{"Zoom In", "CmdOrCtrl+=", "zoom-in"},
		{"Zoom Out", "CmdOrCtrl+-", "zoom-out"},
		{"Actual Size", "CmdOrCtrl+0", "zoom-reset"},
	}
}

func (w *nativeWindow) addItems(menu *application.Menu, entries []menuEntry) {
	for _, e := range entries {
		if e.label == "" {
			menu.AddSeparator()
			continue
		}
		item := menu.Add(e.label).OnClick(w.menuAction(e.action))
		if e.accel != "" {
			item.SetAccelerator(e.accel)
		}
	}
}

// menuAction runs one menu item. A ⌘-key reaches the menu only when the
// page did not take it: WKWebView offers key equivalents to the page
// first and hands them back to AppKit only if keydown was not
// preventDefault-ed. A chord the page handles therefore runs once, from
// the page, and the menu runs it only when clicked or when the page
// passed the key on.
func (w *nativeWindow) menuAction(name string) func(*application.Context) {
	return func(*application.Context) {
		if name == actionQuit {
			if w.app != nil {
				w.app.Quit()
			}
			return
		}
		if w.exec != nil {
			w.exec(shortcutJS(name))
		}
	}
}

func webviewOptions(url string) application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Name:               "main",
		Title:              "Keel",
		Width:              1280,
		Height:             840,
		MinWidth:           800,
		MinHeight:          520,
		BackgroundType:     application.BackgroundTypeTranslucent,
		BackgroundColour:   application.NewRGBA(0, 0, 0, 0),
		URL:                url,
		JS:                 windowDragJS(),
		UseApplicationMenu: true,
		Mac:                macWindowChrome(),
		Windows: application.WindowsWindow{
			BackdropType: application.Acrylic,
		},
		Linux: application.LinuxWindow{
			WebviewGpuPolicy: application.WebviewGpuPolicyNever,
		},
	}
}

func macWindowChrome() application.MacWindow {
	return application.MacWindow{
		TitleBar: overlayTitleBar(),
		Backdrop: application.MacBackdropTranslucent,
	}
}

// overlayTitleBar hides the native "Keel" title so the tab strip can sit
// in that space, next to the traffic lights. HiddenInset drops the lights
// into a taller titlebar so they stay centered beside 42px tabs.
func overlayTitleBar() application.MacTitleBar {
	return application.MacTitleBarHiddenInset
}

// desktopURL marks the page as the native window on goos: native=1, the
// titlebar tab strip on macOS, and appearance=native where the window
// takes the page's theme as its own appearance (appearance.go).
func desktopURL(raw, goos string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Set("native", "1")
	if goos == "darwin" {
		q.Set("chrome", "titlebar")
	} else {
		q.Del("chrome")
	}
	if followsThemeNatively(goos) {
		q.Set(nativeAppearanceParam, "native")
	} else {
		q.Del(nativeAppearanceParam)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// windowDragJS restores Wails CSS window-move after loading the loopback UI.
// Matches the v3 runtime drag hit-test (--wails-draggable: drag).
func windowDragJS() string {
	return `(function(){
		if (window.__keelWindowDrag) return;
		window.__keelWindowDrag = true;
		function invoke(msg) {
			if (window._wails && window._wails.invoke) { window._wails.invoke(msg); return; }
			if (window.WailsInvoke) { window.WailsInvoke(msg); return; }
			var w = window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.external;
			if (w && w.postMessage) { w.postMessage(msg); return; }
			var c = window.chrome && window.chrome.webview;
			if (c && c.postMessage) c.postMessage(msg);
		}
		function dragTarget(e) {
			var t = e.target;
			if (!t) return null;
			if (t.nodeType !== 1) t = t.parentElement;
			return t;
		}
		function isDrag(e) {
			var el = dragTarget(e);
			if (!el || !el.isConnected) return false;
			var val = window.getComputedStyle(el).getPropertyValue("--wails-draggable");
			return !!(val && val.trim() === "drag");
		}
		var canDrag = false;
		function setWindowDragging(on) {
			document.documentElement.classList.toggle("is-window-dragging", on);
		}
		window.addEventListener("mousedown", function(e) {
			canDrag = e.button === 0 && e.detail === 1 && isDrag(e);
			if (canDrag) setWindowDragging(true);
		}, true);
		function endWindowDrag() {
			canDrag = false;
			setWindowDragging(false);
		}
		window.addEventListener("mouseup", endWindowDrag, true);
		window.addEventListener("pointerup", endWindowDrag, true);
		window.addEventListener("blur", endWindowDrag);
		window.addEventListener("mousemove", function(e) {
			if (!canDrag) return;
			if ((e.buttons !== undefined ? e.buttons : 1) === 0) { endWindowDrag(); return; }
			canDrag = false;
			invoke("wails:drag");
		}, true);
		window.addEventListener("dblclick", function(e) {
			if (!isDrag(e)) return;
			e.preventDefault();
			invoke("wails:drag:doubleclick");
		}, true);
	})();`
}

func shortcutJS(name string) string {
	raw, err := json.Marshal(name)
	if err != nil {
		return ""
	}
	return "window.__keelShortcut&&window.__keelShortcut(" + string(raw) + ")"
}
