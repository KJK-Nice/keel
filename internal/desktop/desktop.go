package desktop

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/url"
	stdruntime "runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:shell
var shellFS embed.FS

//go:embed macos/icon.png
var appIcon []byte

type nativeWindow struct {
	url string
	app *application.App
	win *application.WebviewWindow
}

func Run(url, _ string) error {
	assets, err := fs.Sub(shellFS, "shell")
	if err != nil {
		return err
	}
	w := &nativeWindow{url: desktopURL(url, stdruntime.GOOS == "darwin")}
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
	})
	w.app = app
	app.Menu.Set(w.appMenu())
	win := app.Window.NewWithOptions(webviewOptions(w.url))
	w.win = win
	win.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		win.ExecJS(windowDragJS())
	})
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
	file := m.AddSubmenu("File")
	file.Add("New Tab").SetAccelerator("CmdOrCtrl+t").OnClick(w.menuAction("new-tab"))
	file.Add("Close Tab").SetAccelerator("CmdOrCtrl+w").OnClick(w.menuAction("close-tab"))
	file.Add("Reopen Tab").SetAccelerator("CmdOrCtrl+Shift+t").OnClick(w.menuAction("reopen-tab"))
	file.AddSeparator()
	file.Add("New Host").SetAccelerator("CmdOrCtrl+n").OnClick(w.menuAction("new-host"))
	file.Add("Quick Connect").SetAccelerator("CmdOrCtrl+Shift+o").OnClick(w.menuAction("quick-connect"))
	file.Add("Join Session").SetAccelerator("CmdOrCtrl+j").OnClick(w.menuAction("join"))
	file.AddSeparator()
	file.Add("Find Host").OnClick(w.menuAction("search"))
	file.AddSeparator()
	file.Add("Split Right").SetAccelerator("CmdOrCtrl+Shift+d").OnClick(w.menuAction("split-right"))
	file.Add("Split Down").SetAccelerator("CmdOrCtrl+Shift+e").OnClick(w.menuAction("split-down"))
	file.Add("Split Notebook").OnClick(w.menuAction("split-notebook"))
	file.AddSeparator()
	file.Add("Close Window").SetAccelerator("CmdOrCtrl+Shift+w").OnClick(func(*application.Context) {
		if w.app != nil {
			w.app.Quit()
		}
	})
	file.Add("Stop Keel runtime").OnClick(w.menuAction("stop-runtime"))
	view := m.AddSubmenu("View")
	view.Add("Find in Terminal").SetAccelerator("CmdOrCtrl+f").OnClick(w.menuAction("find-term"))
	view.Add("Toggle Vault Sidebar").SetAccelerator("CmdOrCtrl+b").OnClick(w.menuAction("toggle-sidebar"))
	view.Add("Toggle Agent Panel").SetAccelerator(agentPanelAccel(stdruntime.GOOS)).OnClick(w.menuAction("toggle-agent"))
	view.AddSeparator()
	view.Add("Focus Pane").SetAccelerator("CmdOrCtrl+Shift+enter").OnClick(w.menuAction("focus-pane"))
	view.AddSeparator()
	view.Add("Zoom In").SetAccelerator("CmdOrCtrl+=").OnClick(w.menuAction("zoom-in"))
	view.Add("Zoom Out").SetAccelerator("CmdOrCtrl+-").OnClick(w.menuAction("zoom-out"))
	view.Add("Actual Size").SetAccelerator("CmdOrCtrl+0").OnClick(w.menuAction("zoom-reset"))
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

func (w *nativeWindow) menuAction(name string) func(*application.Context) {
	return func(*application.Context) {
		if w.win == nil {
			return
		}
		w.win.ExecJS(shortcutJS(name))
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
// into a taller titlebar so they stay centered beside the tabs.
//
// ShowToolbarWhenFullscreen keeps that titlebar in the window in native
// full screen. Without it Wails returns NSApplicationPresentationAutoHideToolbar,
// so AppKit detaches the titlebar and toolbar and, when the pointer reaches
// the top edge, slides them down with the menu bar as an overlay: the menu
// bar covers the tab strip and the traffic lights land on the pane bar
// below it. With the toolbar kept in the window, the traffic lights stay
// where they are out of full screen (beside the tabs, in the 84px inset
// keel.css reserves) and revealing the menu bar moves the window down
// instead of drawing over it.
func overlayTitleBar() application.MacTitleBar {
	tb := application.MacTitleBarHiddenInset
	tb.ShowToolbarWhenFullscreen = true
	return tb
}

func desktopURL(raw string, titlebarTabs bool) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Set("native", "1")
	if titlebarTabs {
		q.Set("chrome", "titlebar")
	} else {
		q.Del("chrome")
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
