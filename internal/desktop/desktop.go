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
	if w.navigated {
		return
	}
	w.navigated = true
	runtime.WindowExecJS(ctx, navigateJS(w.url))
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

func shortcutJS(name string) string {
	raw, err := json.Marshal(name)
	if err != nil {
		return ""
	}
	return "window.__keelShortcut&&window.__keelShortcut(" + string(raw) + ")"
}
