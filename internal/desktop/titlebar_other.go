//go:build !darwin

package desktop

import "github.com/wailsapp/wails/v3/pkg/application"

// probeChrome has nothing to read outside macOS: there is no titlebar
// overlay, so the page keeps its default layout.
func probeChrome(*application.WebviewWindow) (chromeProbe, bool) {
	return chromeProbe{}, false
}
