package desktop

import (
	_ "embed"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// The Keel mark (macos/icon.svg) without its tile, black on clear at 44px:
// a template image, so macOS draws it in the menu bar's own colour.
//
//go:embed tray-template.png
var trayIcon []byte

// menuBarItem reports whether Keel lives in the menu bar on goos: closing
// the window then hides it (and the Dock icon) instead of quitting, and
// the menu bar item brings it back. Quit Keel or ⌘Q quits.
func menuBarItem(goos string) bool {
	return goos == "darwin"
}

// trayRefresh is how often the menu re-reads keel serve while Keel runs:
// the session count and the Claude Code switch can change from the page
// or another client at any time.
const trayRefresh = 3 * time.Second

// Menu labels.
const (
	trayShow       = "Show Keel"
	trayHide       = "Hide Keel"
	trayNewTab     = "New Tab"
	trayQuick      = "Quick Connect…"
	trayClaudeCode = "Allow Claude Code (keel mcp)"
	trayQuit       = "Quit Keel"
	agentChangedJS = `window.dispatchEvent(new Event("keel-agent-settings-changed"))`
)

// startTray adds the menu bar item. Its menu: keel serve's status, show or
// hide the window, New Tab, Quick Connect, the Claude Code switch, Quit.
func (w *nativeWindow) startTray() {
	tray := w.app.SystemTray.New()
	tray.SetTemplateIcon(trayIcon)
	tray.SetTooltip("Keel")

	menu := application.NewMenu()
	status := menu.Add(statusLabel(runtimeStatus{})).SetEnabled(false)
	menu.AddSeparator()
	toggle := menu.Add(trayHide).OnClick(func(*application.Context) {
		if w.win.IsVisible() {
			w.hideWindow()
		} else {
			w.showWindow()
		}
		w.refreshTray()
	})
	menu.Add(trayNewTab).OnClick(func(*application.Context) { w.showAndRun("new-tab") })
	menu.Add(trayQuick).OnClick(func(*application.Context) { w.showAndRun("quick-connect") })
	menu.AddSeparator()
	claude := menu.AddCheckbox(trayClaudeCode, false).SetEnabled(false)
	claude.OnClick(func(ctx *application.Context) {
		on := ctx.ClickedMenuItem().Checked()
		go func() {
			// Not saved: the refresh shows what keel serve holds.
			if w.serve != nil && w.serve.setClaudeCode(on) == nil && w.exec != nil {
				// An open Settings · Agent shows the new value.
				w.exec(agentChangedJS)
			}
			w.syncTray()
		}()
	})
	menu.AddSeparator()
	menu.Add(trayQuit).OnClick(func(*application.Context) { w.app.Quit() })
	tray.SetMenu(menu)

	w.tray = &trayItems{status: status, toggle: toggle, claude: claude}
	// startTray runs before app.Run, and Wails cannot take a main-thread
	// call (InvokeAsync, IsVisible) until Run has made the platform app: a
	// refresh before then dereferenced nil and took Keel down at launch.
	// Refresh only once the app has started.
	w.app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		if w.started.Swap(true) {
			return
		}
		w.refreshTray()
		go func() {
			t := time.NewTicker(trayRefresh)
			defer t.Stop()
			for range t.C {
				if w.quitting.Load() {
					return
				}
				w.syncTray()
			}
		}()
	})
}

type trayItems struct {
	status, toggle, claude *application.MenuItem
}

// refreshTray re-reads keel serve and the window into the menu, off the
// calling (often main) thread: the reads are HTTP calls.
func (w *nativeWindow) refreshTray() {
	if w.tray != nil && w.started.Load() {
		go w.syncTray()
	}
}

// syncTray does nothing until the app has started (startTray).
func (w *nativeWindow) syncTray() {
	if w.tray == nil || !w.started.Load() {
		return
	}
	s := runtimeStatus{}
	if w.serve != nil {
		s = w.serve.status()
	}
	visible := w.win != nil && w.win.IsVisible()
	application.InvokeAsync(func() {
		w.tray.status.SetLabel(statusLabel(s))
		if visible {
			w.tray.toggle.SetLabel(trayHide)
		} else {
			w.tray.toggle.SetLabel(trayShow)
		}
		w.tray.claude.SetChecked(s.known && s.claudeCode)
		w.tray.claude.SetEnabled(s.known)
	})
}

// keepInMenuBar makes closing the window (its close button, File → Close
// Window) hide it and the Dock icon. Quit still closes it: the shutdown
// hook runs before Wails closes windows. Opening Keel again (Finder,
// Spotlight, the Dock while it is there) shows the window and the Dock
// icon.
func (w *nativeWindow) keepInMenuBar() {
	w.app.OnShutdown(func() { w.quitting.Store(true) })
	w.app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) {
		w.showWindow()
		w.refreshTray()
	})
	w.win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if w.quitting.Load() {
			return
		}
		e.Cancel()
		w.hideWindow()
	})
}

func (w *nativeWindow) hideWindow() {
	w.win.Hide()
	setDockVisible(false)
	w.refreshTray()
}

func (w *nativeWindow) showWindow() {
	setDockVisible(true)
	w.win.Show()
	w.win.Focus()
}

// showAndRun brings the window forward and runs a menu action in the page
// (runShortcut in term.js), as File → New Tab does.
func (w *nativeWindow) showAndRun(action string) {
	w.showWindow()
	if w.exec != nil {
		w.exec(shortcutJS(action))
	}
	w.refreshTray()
}
