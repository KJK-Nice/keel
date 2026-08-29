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
	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:              "main",
		Title:             "Keel",
		Width:             1280,
		Height:            840,
		MinWidth:          800,
		MinHeight:         520,
		BackgroundColour:  application.NewRGB(13, 13, 13),
		URL:               w.url,
		JS:                windowDragJS(),
		UseApplicationMenu: true,
		Mac: application.MacWindow{
			TitleBar: overlayTitleBar(),
		},
		Linux: application.LinuxWindow{
			WebviewGpuPolicy: application.WebviewGpuPolicyNever,
		},
	})
	w.win = win
	win.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		win.ExecJS(windowDragJS())
	})
	return app.Run()
}

func (w *nativeWindow) appMenu() *application.Menu {
	m := application.NewMenu()
	if stdruntime.GOOS == "darwin" {
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
	file.Add("Find Host").SetAccelerator("CmdOrCtrl+k").OnClick(w.menuAction("search"))
	file.AddSeparator()
	file.Add("Split Right").SetAccelerator("CmdOrCtrl+Shift+d").OnClick(w.menuAction("split-right"))
	file.Add("Split Down").SetAccelerator("CmdOrCtrl+Shift+e").OnClick(w.menuAction("split-down"))
	file.AddSeparator()
	file.Add("Close Window").SetAccelerator("CmdOrCtrl+Shift+w").OnClick(func(*application.Context) {
		if w.app != nil {
			w.app.Quit()
		}
	})
	view := m.AddSubmenu("View")
	view.Add("Zoom In").SetAccelerator("CmdOrCtrl+=").OnClick(w.menuAction("zoom-in"))
	view.Add("Zoom Out").SetAccelerator("CmdOrCtrl+-").OnClick(w.menuAction("zoom-out"))
	view.Add("Actual Size").SetAccelerator("CmdOrCtrl+0").OnClick(w.menuAction("zoom-reset"))
	m.AddRole(application.EditMenu)
	if stdruntime.GOOS == "darwin" {
		m.AddRole(application.WindowMenu)
	}
	return m
}

func (w *nativeWindow) menuAction(name string) func(*application.Context) {
	return func(*application.Context) {
		if w.win == nil {
			return
		}
		w.win.ExecJS(shortcutJS(name))
	}
}

// overlayTitleBar hides the native "Keel" title so the tab strip can sit
// in that space, next to the traffic lights. HiddenInset drops the lights
// into a taller titlebar so they stay centered beside 42px tabs.
func overlayTitleBar() application.MacTitleBar {
	return application.MacTitleBarHiddenInset
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
