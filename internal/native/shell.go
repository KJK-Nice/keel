package native

import (
	_ "embed"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"

	"keel/internal/native/keelclient"
)

// The app shell around the window: the menu bar, the menu bar item (tray) and keeping Keel there when the window is
// closed. Every item runs a workspace function the window already has (as the chord does), and none types into a session.

//go:embed macos/tray-template.png
var trayIcon []byte

// menuEntry is one item of a Keel submenu; an empty label is a separator.
type menuEntry struct {
	label string
	accel string
	run   func(w *workspace)
}

// shell is the menu bar, the tray and the keep-in-menu-bar behaviour of one window.
type shell struct {
	w        *workspace
	win      *mygo.Window
	menuBar  bool        // closing the window hides it, and the Dock icon; Quit quits
	quitting atomic.Bool // Quit has begun: the window may close
	tray     *mygo.Tray
	status   *mygo.MenuItem
	toggle   *mygo.MenuItem
	claude   *mygo.MenuItem
}

// Tray labels.
const (
	trayShow       = "Show Keel"
	trayHide       = "Hide Keel"
	trayNewTab     = "New Tab"
	trayQuick      = "Quick Connect…"
	trayClaudeCode = "Allow Claude Code (keel mcp)"
	trayQuit       = "Quit Keel"
	// The session count and the Claude Code switch can change from another client. The tray reads GET /api/vault and
	// GET /api/agent/settings for them; at 3 s that was one call a few seconds all day for a menu seen now and then (the
	// window's own poll, pollEvery, is what notices a locked vault). Showing or hiding the window refreshes it at once.
	trayRefresh = 20 * time.Second
)

// statusLabel is desktop.statusLabel.
func statusLabel(running, locked bool, sessions int) string {
	switch {
	case !running:
		return "keel serve is not running"
	case locked:
		return "keel serve · vault locked"
	case sessions == 1:
		return "keel serve · 1 session"
	default:
		return fmt.Sprintf("keel serve · %d sessions", sessions)
	}
}

// run queues a workspace action on the UI thread, as a chord does.
func (s *shell) run(fn func(w *workspace)) {
	s.w.post(func() { s.w.act(func() { fn(s.w) }) })
}

func (s *shell) items(entries []menuEntry) []*mygo.MenuItem {
	var out []*mygo.MenuItem
	for _, e := range entries {
		if e.label == "" {
			out = append(out, mygo.Separator())
			continue
		}
		run := e.run
		out = append(out, &mygo.MenuItem{Label: e.label, Accelerator: e.accel, Click: func(*mygo.MenuItem, *mygo.Window) { s.run(run) }})
	}
	return out
}

// menu is the app menu, File, View and the Window menu. No Edit menu: its ⌘C and ⌘V would
// take the keys from the terminal's own copy and paste (the editing roles are for a page's text).
func (s *shell) menu() *mygo.Menu {
	file := s.items(menuEntries("File"))
	file = append(file, mygo.Separator(),
		&mygo.MenuItem{Label: "Close Window", Accelerator: "CmdOrCtrl+Shift+w", Click: func(*mygo.MenuItem, *mygo.Window) { s.closeWindow() }})
	file = append(file, s.items(menuEntries("Stop"))...)
	return mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Label: "File", Submenu: file},
		{Label: "View", Submenu: s.items(menuEntries("View"))},
		{Role: mygo.RoleWindowMenu},
	})
}

// closeWindow is File → Close Window: it hides the window in the menu bar, else quits.
func (s *shell) closeWindow() {
	if s.menuBar {
		s.hide()
		return
	}
	mygo.App.Quit()
}

func (s *shell) hide() {
	s.win.Hide()
	mygo.App.Dock.Hide()
	s.refreshTray()
}

func (s *shell) show() {
	mygo.App.Dock.Show()
	s.win.Show()
	s.win.Focus()
	s.refreshTray()
}

// start installs the menu bar, and on macOS the menu bar item and keep-in-menu-bar.
func (s *shell) start() {
	mygo.App.SetMenu(s.menu())
	s.menuBar = isMac() // Keel lives in the menu bar on macOS
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { s.quitting.Store(true) })
	if !s.menuBar {
		return
	}
	// Closing the window (its button, File → Close Window) hides it and the Dock icon; Quit still closes it.
	s.win.OnClose(func(e *mygo.CloseEvent) {
		if s.quitting.Load() {
			return
		}
		e.PreventDefault()
		s.hide()
	})
	// Opening Keel again (Finder, Spotlight, the Dock while it is there) shows the window and the Dock icon.
	mygo.App.OnActivate(func(visible bool) {
		if !visible && !s.quitting.Load() {
			s.show()
		}
	})
	s.startTray()
}

func (s *shell) startTray() {
	s.status = &mygo.MenuItem{Label: statusLabel(false, false, 0), Disabled: true}
	s.toggle = &mygo.MenuItem{Label: trayHide, Click: func(*mygo.MenuItem, *mygo.Window) {
		if s.win.IsVisible() {
			s.hide()
		} else {
			s.show()
		}
	}}
	s.claude = &mygo.MenuItem{Label: trayClaudeCode, Type: mygo.MenuItemCheckbox, Disabled: true, Click: func(it *mygo.MenuItem, _ *mygo.Window) {
		on := it.Checked // toggled before Click runs
		// Not saved here: the refresh shows what keel serve holds.
		s.w.work(func() {
			c, cancel := ctx()
			defer cancel()
			_, err := s.w.be.SaveAgentSettings(c, keelclient.AgentSettingsPatch{ClaudeCode: &on})
			s.w.post(func() {
				if err != nil {
					s.w.warn(errText("Claude Code", err))
				}
			})
			s.syncTray()
		})
	}}
	menu := mygo.NewMenu([]*mygo.MenuItem{
		s.status, mygo.Separator(), s.toggle,
		{Label: trayNewTab, Click: func(*mygo.MenuItem, *mygo.Window) { s.show(); s.run((*workspace).newTabKey) }},
		{Label: trayQuick, Click: func(*mygo.MenuItem, *mygo.Window) { s.show(); s.run(func(w *workspace) { w.openQuick("", nil) }) }},
		mygo.Separator(), s.claude, mygo.Separator(),
		{Label: trayQuit, Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	})
	tray, err := mygo.NewTray(mygo.TrayOptions{ID: "app.keel", Icon: trayIcon, IconIsTemplate: true, ToolTip: "Keel", Menu: menu})
	if err != nil { // no menu bar item: closing the window then quits, as without one
		s.menuBar = false
		return
	}
	s.tray = tray
	s.refreshTray()
	go func() {
		t := time.NewTicker(trayRefresh)
		defer t.Stop()
		for range t.C {
			if s.quitting.Load() {
				return
			}
			s.syncTray()
		}
	}()
}

func (s *shell) refreshTray() {
	if s.tray != nil {
		go s.syncTray()
	}
}

// syncTray re-reads keel serve and the window into the menu. It makes HTTP calls: not on the UI thread.
func (s *shell) syncTray() {
	if s.tray == nil {
		return
	}
	c, cancel := ctx()
	defer cancel()
	running, locked, sessions, known, claude := false, false, 0, false, false
	if v, err := s.w.be.Vault(c); err == nil {
		running, locked, sessions = true, v.Locked, v.Sessions
		if !v.Locked {
			if a, err := s.w.be.AgentSettings(c); err == nil {
				known, claude = true, a.Settings.ClaudeCode
			}
		}
	}
	s.status.SetLabel(statusLabel(running, locked, sessions))
	if s.win.IsVisible() {
		s.toggle.SetLabel(trayHide)
	} else {
		s.toggle.SetLabel(trayShow)
	}
	s.claude.SetChecked(known && claude)
	s.claude.SetEnabled(known)
}
