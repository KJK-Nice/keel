package desktop

import "github.com/wailsapp/wails/v3/pkg/application"

// jsExec sends a script to the page. It is the one path Go uses to run
// script in the window: menu actions (shortcutJS) and the full-screen
// chrome state (chromeJS).
//
// Not Wails' WebviewWindow.ExecJS: that queues every script until the
// Wails JS runtime posts "wails:runtime:ready", and the loopback
// `keel serve` page never loads that runtime, so the queue never flushes
// (#36). Wails only injects its small runtime core (window._wails.invoke)
// and the window's JS option (windowDragJS) on each page load, and those
// skip the queue.
//
// On macOS evalJS calls -[WKWebView evaluateJavaScript:] directly
// (evaljs_darwin.go). The ExecJS gate is in Wails' shared window code, so
// WebView2 and WebKitGTK have it too; evaljs_other.go still uses ExecJS
// until they get a direct path.
type jsExec func(js string)

// windowJS is the jsExec for a native window.
func windowJS(win *application.WebviewWindow) jsExec {
	return func(js string) { evalJS(win, js) }
}
