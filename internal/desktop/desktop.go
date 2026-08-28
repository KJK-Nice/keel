package desktop

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	stdruntime "runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:shell
var shellFS embed.FS

//go:embed macos/icon.png
var appIcon []byte

type nativeWindow struct {
	url       string
	navigated bool
	ctx       context.Context
}

func Run(url, _ string) error {
	assets, err := fs.Sub(shellFS, "shell")
	if err != nil {
		return err
	}
	w := &nativeWindow{url: desktopURL(url, stdruntime.GOOS == "darwin")}
	return wails.Run(&options.App{
		Title:             "Keel",
		Width:             1280,
		Height:            840,
		MinWidth:          800,
		MinHeight:         520,
		BackgroundColour:  &options.RGBA{R: 13, G: 13, B: 13, A: 255},
		AssetServer:       &assetserver.Options{Assets: assets},
		HideWindowOnClose: false,
		CSSDragProperty:   "--wails-draggable",
		CSSDragValue:      "drag",
		Menu:              w.appMenu(),
		OnStartup:         func(ctx context.Context) { w.ctx = ctx },
		OnDomReady:        w.domReady,
		Mac: &mac.Options{
			TitleBar:   overlayTitleBar(),
			Appearance: mac.DefaultAppearance,
			About: &mac.AboutInfo{
				Title:   "Keel",
				Message: "Local SSH workspace",
				Icon:    appIcon,
			},
		},
		Windows: &windows.Options{
			Theme: windows.SystemDefault,
		},
		Linux: &linux.Options{
			Icon:             appIcon,
			ProgramName:      "Keel",
			WebviewGpuPolicy: linux.WebviewGpuPolicyNever,
		},
	})
}

func (w *nativeWindow) appMenu() *menu.Menu {
	m := menu.NewMenu()
	if stdruntime.GOOS == "darwin" {
		m.Append(menu.AppMenu())
	}
	file := m.AddSubmenu("File")
	file.AddText("New Tab", keys.CmdOrCtrl("t"), w.menuAction("new-tab"))
	file.AddText("Close Tab", keys.CmdOrCtrl("w"), w.menuAction("close-tab"))
	file.AddText("Reopen Tab", keys.Combo("t", keys.CmdOrCtrlKey, keys.ShiftKey), w.menuAction("reopen-tab"))
	file.AddSeparator()
	file.AddText("New Host", keys.CmdOrCtrl("n"), w.menuAction("new-host"))
	file.AddText("Quick Connect", keys.Combo("o", keys.CmdOrCtrlKey, keys.ShiftKey), w.menuAction("quick-connect"))
	file.AddText("Join Session", keys.CmdOrCtrl("j"), w.menuAction("join"))
	file.AddSeparator()
	file.AddText("Find Host", keys.CmdOrCtrl("k"), w.menuAction("search"))
	file.AddSeparator()
	file.AddText("Split Right", keys.Combo("d", keys.CmdOrCtrlKey, keys.ShiftKey), w.menuAction("split-right"))
	file.AddText("Split Down", keys.Combo("e", keys.CmdOrCtrlKey, keys.ShiftKey), w.menuAction("split-down"))
	file.AddSeparator()
	file.AddText("Close Window", keys.Combo("w", keys.CmdOrCtrlKey, keys.ShiftKey), func(_ *menu.CallbackData) {
		if w.ctx != nil {
			runtime.Quit(w.ctx)
		}
	})
	view := m.AddSubmenu("View")
	view.AddText("Zoom In", keys.CmdOrCtrl("="), w.menuAction("zoom-in"))
	view.AddText("Zoom Out", keys.CmdOrCtrl("-"), w.menuAction("zoom-out"))
	view.AddText("Actual Size", keys.CmdOrCtrl("0"), w.menuAction("zoom-reset"))
	m.Append(menu.EditMenu())
	if stdruntime.GOOS == "darwin" {
		m.Append(menu.WindowMenu())
	}
	return m
}

func (w *nativeWindow) menuAction(name string) menu.Callback {
	return func(_ *menu.CallbackData) {
		if w.ctx == nil {
			return
		}
		runtime.WindowExecJS(w.ctx, shortcutJS(name))
	}
}

func (w *nativeWindow) domReady(ctx context.Context) {
	w.ctx = ctx
	if !w.navigated {
		w.navigated = true
		runtime.WindowExecJS(ctx, navigateJS(w.url))
		return
	}
	// location.replace drops the Wails runtime. Hidden titlebar has no
	// native drag region, so re-bind CSS drag on the Keel page.
	runtime.WindowExecJS(ctx, windowDragJS())
}

// overlayTitleBar hides the native "Keel" title so the tab strip can sit
// in that space, next to the traffic lights. FullSizeContent lets the
// webview paint under the titlebar. UseToolbar stays off so an empty
// NSToolbar does not steal the strip.
func overlayTitleBar() *mac.TitleBar {
	return mac.TitleBarHidden()
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

func navigateJS(url string) string {
	raw, err := json.Marshal(url)
	if err != nil {
		raw = []byte(`"/"`)
	}
	return fmt.Sprintf("if(!window.__keelNative){window.__keelNative=true;window.location.replace(%s);}", raw)
}

// windowDragJS restores Wails CSS window-move after navigating off wails://.
// Matches internal/frontend/runtime/desktop/main.js (defer drag to mousemove).
func windowDragJS() string {
	return `(function(){
		if (window.__keelWindowDrag) return;
		window.__keelWindowDrag = true;
		function invoke(msg) {
			if (window.WailsInvoke) { window.WailsInvoke(msg); return; }
			var w = window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.external;
			if (w && w.postMessage) { w.postMessage(msg); return; }
			var c = window.chrome && window.chrome.webview;
			if (c && c.postMessage) c.postMessage(msg);
		}
		var shouldDrag = false;
		function isDrag(e) {
			if (!e.target || e.target.nodeType !== 1) return false;
			var val = window.getComputedStyle(e.target).getPropertyValue("--wails-draggable");
			if (val) val = val.trim();
			return val === "drag" && e.buttons === 1 && e.detail === 1;
		}
		window.addEventListener("mousedown", function(e) { shouldDrag = isDrag(e); });
		window.addEventListener("mouseup", function() { shouldDrag = false; });
		window.addEventListener("mousemove", function(e) {
			if (!shouldDrag) return;
			shouldDrag = false;
			if ((e.buttons !== undefined ? e.buttons : e.which) > 0) invoke("drag");
		});
	})();`
}

func shortcutJS(name string) string {
	raw, err := json.Marshal(name)
	if err != nil {
		return ""
	}
	return "window.__keelShortcut&&window.__keelShortcut(" + string(raw) + ")"
}
