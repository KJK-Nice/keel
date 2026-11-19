package native

import (
	_ "embed"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"

	"keel/internal/native/keelclient"
)

// The app shell around the window: the menu bar, the menu bar item (tray) and keeping Keel there when the window is
// closed. Every item runs a workspace function the window already has (as the chord does), and none types into a session.

//go:embed macos/tray-template.png
var trayIcon []byte

// appIcon is packaging/macos/menu-icon.svg at 32 px: the tray menu's header shows it at 16pt, MyGo's fixed box.
//
//go:embed macos/app-icon.png
var appIcon []byte

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
	status   *mygo.MenuItem // the header: "Keel Server" over the serve status
	appVer   *mygo.MenuItem
	serveVer *mygo.MenuItem
	toggle   *mygo.MenuItem
	claude   *mygo.MenuItem
}

// Tray labels.
const (
	trayServer     = "Keel Server"
	trayStop       = "Stop Keel Runtime…"
	trayShow       = "Show Keel"
	trayHide       = "Hide Keel"
	trayNewTab     = "New Tab"
	trayQuick      = "Quick Connect…"
	trayClaudeCode = "Allow Claude Code (keel mcp)"
	traySettings   = "Settings…"
	trayQuit       = "Quit Keel"
	// The session count and the Claude Code switch can change from another client. The tray reads GET /api/vault and
	// GET /api/agent/settings for them; at 3 s that was one call a few seconds all day for a menu seen now and then (the
	// window's own poll, pollEvery, is what notices a locked vault). Showing or hiding the window refreshes it at once.
	trayRefresh = 20 * time.Second
)

// statusLabel is the header's second line: the state of keel serve.
func statusLabel(running, locked bool, sessions int) string {
	switch {
	case !running:
		return "Not running"
	case locked:
		return "Running · vault locked"
	case sessions == 1:
		return "Running · 1 session"
	default:
		return fmt.Sprintf("Running · %d sessions", sessions)
	}
}

// versionLabels are the two version rows: the app (its release, then its commit) and the keel serve it talks to
// (its commit; "" when it is not running). Builds are build ids (internal/buildid), shown short as About shows them.
func versionLabels(release, appBuild, serveBuild string) (app, serve string) {
	if release == "" {
		release = "dev"
	}
	app = "Keel " + release
	if v := versionOf(appBuild); v != "" {
		app += " (" + v + ")"
	}
	serve = "keel serve —"
	if v := versionOf(serveBuild); v != "" {
		serve = "keel serve " + v
	}
	return app, serve
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
	// macOS's app menu is the default one with Check for Updates… after About (roles are left out elsewhere).
	app := &mygo.MenuItem{Role: mygo.RoleAppMenu}
	if isMac() {
		// Labelled with the app's name, as RoleAppMenu is (the automation harness quits through it).
		app = &mygo.MenuItem{Label: mygo.App.Name(), Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout}, updater.MenuItem(), mygo.Separator(),
			{Role: mygo.RoleServices}, mygo.Separator(),
			{Role: mygo.RoleHide}, {Role: mygo.RoleHideOthers}, {Role: mygo.RoleUnhide}, mygo.Separator(),
			{Role: mygo.RoleQuit},
		}}
	}
	return mygo.NewMenu([]*mygo.MenuItem{
		app,
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
	// The header opens a submenu (macOS draws the ›), as Rex's does; its icon is the app's, in colour.
	s.status = &mygo.MenuItem{Label: trayServer, Subtitle: statusLabel(false, false, 0), Submenu: []*mygo.MenuItem{
		{Label: trayStop, Click: func(*mygo.MenuItem, *mygo.Window) { s.show(); s.run((*workspace).askStopRuntime) }},
	}}
	if icon, err := mygo.NewMenuIcon(appIcon, false); err == nil {
		s.status.Icon = icon
	}
	// Choosing a version row copies both, as a bug report wants them.
	// shortcut: w.copy writes on the next frame, so with the window hidden the copy lands when it next draws; write
	// through mygo.Clipboard (and guard_test's rules) if that proves too late.
	copyVersions := func(*mygo.MenuItem, *mygo.Window) {
		text := s.appVer.Label + "\n" + s.serveVer.Label
		s.run(func(w *workspace) { w.copy(text, "versions") })
	}
	app, serve := versionLabels(mygo.App.Version(), s.w.appBuild, "")
	s.appVer = &mygo.MenuItem{Label: app, Click: copyVersions}
	s.serveVer = &mygo.MenuItem{Label: serve, Click: copyVersions}
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
	// Each window command shows the chord it has in the window.
	cmd := func(label, id string) *mygo.MenuItem {
		c := commandByID(id)
		return &mygo.MenuItem{Label: label, Accelerator: c.accel(), Click: func(*mygo.MenuItem, *mygo.Window) { s.show(); s.run(c.invoke) }}
	}
	menu := mygo.NewMenu([]*mygo.MenuItem{
		s.status, mygo.Separator(),
		s.appVer, s.serveVer, mygo.Separator(),
		s.toggle, cmd(trayNewTab, "tab.new"), cmd(trayQuick, "host.quick"),
		mygo.Separator(), s.claude, mygo.Separator(),
		updater.MenuItem(), cmd(traySettings, "settings"),
		{Label: trayQuit, Accelerator: "CmdOrCtrl+q", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
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
	running, locked, sessions, known, claude, build := false, false, 0, false, false, ""
	if v, err := s.w.be.Vault(c); err == nil {
		running, locked, sessions = true, v.Locked, v.Sessions
		if !v.Locked {
			if a, err := s.w.be.AgentSettings(c); err == nil {
				known, claude = true, a.Settings.ClaudeCode
			}
		}
		if rt, err := s.w.be.Runtime(c); err == nil {
			build = rt.Build
		}
	}
	s.status.SetSubtitle(statusLabel(running, locked, sessions))
	app, serve := versionLabels(mygo.App.Version(), s.w.appBuild, build)
	s.appVer.SetLabel(app)
	s.serveVer.SetLabel(serve)
	if s.win.IsVisible() {
		s.toggle.SetLabel(trayHide)
	} else {
		s.toggle.SetLabel(trayShow)
	}
	s.claude.SetChecked(known && claude)
	s.claude.SetEnabled(known)
}
