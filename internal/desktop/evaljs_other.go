//go:build !darwin

package desktop

import "github.com/wailsapp/wails/v3/pkg/application"

// evalJS falls back to Wails' ExecJS, which waits for a runtime the
// loopback page never loads (see jsExec in evaljs.go). On Windows and
// Linux native menu actions therefore still do not reach the page; the
// keyboard shortcuts work because the page handles them itself (#36).
func evalJS(win *application.WebviewWindow, js string) {
	if win == nil || js == "" {
		return
	}
	win.ExecJS(js)
}
