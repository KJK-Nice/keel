package desktop

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:shell
var shellFS embed.FS

type nativeWindow struct {
	url       string
	navigated bool
}

func Run(url, _ string) error {
	assets, err := fs.Sub(shellFS, "shell")
	if err != nil {
		return err
	}
	w := &nativeWindow{url: url}
	return wails.Run(&options.App{
		Title:             "Keel",
		Width:             1280,
		Height:            840,
		MinWidth:          800,
		MinHeight:         520,
		BackgroundColour:  &options.RGBA{R: 13, G: 13, B: 13, A: 255},
		AssetServer:       &assetserver.Options{Assets: assets},
		HideWindowOnClose: false,
		OnDomReady:        w.domReady,
		Mac: &mac.Options{
			TitleBar:   mac.TitleBarDefault(),
			Appearance: mac.DefaultAppearance,
			About: &mac.AboutInfo{
				Title:   "Keel",
				Message: "Local SSH workspace",
			},
		},
		Windows: &windows.Options{
			Theme: windows.SystemDefault,
		},
		Linux: &linux.Options{
			ProgramName:      "Keel",
			WebviewGpuPolicy: linux.WebviewGpuPolicyNever,
		},
	})
}

func (w *nativeWindow) domReady(ctx context.Context) {
	if w.navigated {
		return
	}
	w.navigated = true
	runtime.WindowExecJS(ctx, navigateJS(w.url))
}

func navigateJS(url string) string {
	raw, err := json.Marshal(url)
	if err != nil {
		raw = []byte(`"/"`)
	}
	return fmt.Sprintf("if(!window.__keelNative){window.__keelNative=true;window.location.replace(%s);}", raw)
}
