//go:build nativee2e

package native

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// Snippets and Approve before type against a real keel serve and a real shell (Local Shell). The point of these
// scenarios is what reaches the PTY and when: nothing before Type, and after Type exactly Ctrl-U and the text the
// dialog showed (keel serve adds nothing, so the trailing newline the client adds is what runs the command).

func (e *win) snippetsOnServer() []keelclient.Snippet {
	e.t.Helper()
	_, body := e.raw("GET", "/api/snippets", nil)
	var out []keelclient.Snippet
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		e.t.Fatal(err)
	}
	return out
}

// snippetNamed is the snippet with that name on the daemon (a new vault is seeded with four of its own).
func (e *win) snippetNamed(name string) (keelclient.Snippet, bool) {
	for _, s := range e.snippetsOnServer() {
		if s.Name == name {
			return s, true
		}
	}
	return keelclient.Snippet{}, false
}

// inWindow reports that the window holds a snippet of that name (it asks the server again each time).
func (e *win) inWindow(name string) bool {
	e.w.reloadSnippets()
	for _, s := range e.w.snippets {
		if s.Name == name {
			return true
		}
	}
	return false
}

// addSnippet makes a snippet on the daemon the way another client would.
func (e *win) addSnippet(name, command string) {
	e.t.Helper()
	if code, body := e.raw("POST", "/api/snippets", map[string]any{"name": name, "command": command}); code != 201 {
		e.t.Fatalf("POST /api/snippets: %d %s", code, body)
	}
}

// openApprove waits for the Approve before type dialog.
func (e *win) waitApprove() {
	e.t.Helper()
	e.pump(func() bool { return e.hasLabel("dialog "+approveTitle) && e.w.approve != nil }, "the Approve before type dialog")
}

func TestE2EApproveBeforeType(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	sid := e.openLocal()

	// Create the snippet through the Snippets page: the sidebar section, Manage snippets, New snippet.
	e.click("Snippets")
	e.click("Manage snippets")
	e.pump(func() bool { return e.w.pageTab("snippets") != nil && e.w.tab().name() == "Snippets" }, "the Snippets page")
	e.click("New snippet")
	e.pump(func() bool { return e.hasLabel("dialog New snippet") && e.tt.Focused("Snippet dialog name") }, "the New snippet dialog")
	e.fillField("Snippet dialog name", "e2e arithmetic")
	e.fillField("Snippet dialog command", "echo snippet-$((40+2))")
	e.click("Save snippet")
	e.pump(func() bool { return e.w.snipDlg == nil && e.hasLabel("snippet e2e arithmetic") }, "the new snippet in the list")
	if got, ok := e.snippetNamed("e2e arithmetic"); !ok || got.Command != "echo snippet-$((40+2))\n" {
		t.Fatalf("snippet on the server: %+v", got)
	}
	e.shot("TestE2EApproveBeforeType-page")

	// Type into pane… opens the dialog, which shows exactly the text, and nothing is typed.
	e.click("Type into pane…")
	e.waitApprove()
	if e.w.active != 0 {
		t.Fatalf("active tab %d: the session tab was not shown for the approval", e.w.active)
	}
	for _, s := range []string{approveTitle, approveHint, "Session:", "echo snippet-$((40+2))", "Cancel", "Type"} {
		if !e.tt.HasText(s) {
			t.Errorf("the dialog lacks %q", s)
		}
	}
	e.shot("TestE2EApproveBeforeType-dialog")
	e.settle(time.Second) // pumping: the window is live and nothing may reach the shell
	if sb := e.scrollback(sid); strings.Contains(sb, "snippet-") {
		t.Fatalf("something was typed before Type: %q", sb)
	}
	// Escape closes it and still nothing is typed.
	e.key(0, ui.KeyEscape)
	e.pump(func() bool { return e.w.approve == nil }, "Escape to close the dialog")
	e.settle(300 * time.Millisecond)
	if strings.Contains(e.scrollback(sid), "snippet-") {
		t.Fatal("Escape typed")
	}
	// Open it again and press Type: the bytes are Ctrl-U and the text, newline included, so the shell runs it.
	e.click("tab Snippets")
	e.click("Type into pane…")
	e.waitApprove()
	e.click("Type")
	e.pump(func() bool { return e.w.approve == nil }, "the dialog to close after Type")
	e.waitScrollback(sid, "snippet-42") // the output: the echoed command line has "$((40+2))", never "snippet-42"
	if !strings.Contains(e.scrollback(sid), "echo snippet-$((40+2))") {
		t.Error("the command line is not in the scrollback")
	}
	e.shot("TestE2EApproveBeforeType-typed")
}

func TestE2ETypeClearsTheLineFirst(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	sid := e.openLocal()
	e.addSnippet("e2e clean", "echo clean-$((1+1))")
	e.pump(func() bool { return e.inWindow("e2e clean") }, "the snippet in the window")
	e.clickBody("1")
	e.tt.Type("garbage") // typed at the prompt, no Enter
	e.step()
	e.waitScrollback(sid, "garbage")
	e.click("Snippets")
	e.click("sidebar snippet e2e clean")
	e.waitApprove()
	e.click("Type")
	e.pump(func() bool { return e.w.approve == nil }, "the dialog to close")
	e.waitScrollback(sid, "clean-2")
	sb := e.scrollback(sid)
	if strings.Contains(sb, "garbage echo") || strings.Contains(sb, "garbageecho") {
		t.Fatalf("the typed line was not cleared first: %q", sb)
	}
	e.shot("TestE2ETypeClearsTheLineFirst")
}

func TestE2EFollowerCannotType(t *testing.T) {
	addr, dir := startServe(t, true)
	a := newWin(t, addr, dir, nil)
	sid := a.openLocal()
	a.addSnippet("e2e follow", "echo follow-typed-$((40+2))")

	b := newWin(t, addr, dir, nil)
	both := []*win{a, b}
	pumpAll(t, both, func() bool {
		return len(b.w.tabs) == 1 && b.snap().Role == "follower" && len(a.snap().Attached) == 2 && b.inWindow("e2e follow")
	}, "B following with the snippet loaded")
	b.click("Snippets")
	b.click("sidebar snippet e2e follow")
	b.waitApprove()
	pumpAll(t, both, func() bool { return b.tt.HasText(msgFollowing) }, "the dialog to say B is following")
	b.shot("TestE2EFollowerCannotType")
	_ = b.tt.Click("Type") // it is off
	for end := time.Now().Add(700 * time.Millisecond); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		a.step()
		b.step()
	}
	if strings.Contains(a.scrollback(sid), "follow-typed") {
		t.Fatal("a follower typed")
	}
	if b.w.approve == nil {
		t.Fatal("the dialog closed on a refused Type")
	}

	// A passes control to B (a control frame, as the follower test does); the same dialog can now type.
	conn := a.w.focusedPane().conn.(*keelclient.Conn)
	if err := conn.GrantControl(b.snap().YouID); err != nil {
		t.Fatal(err)
	}
	pumpAll(t, both, func() bool { return b.snap().Role == "driver" && !b.tt.HasText(msgFollowing) }, "B driving")
	b.click("Type")
	pumpAll(t, both, func() bool { return b.w.approve == nil }, "the dialog to close")
	a.waitScrollback(sid, "follow-typed-42")
}

func TestE2ESnippetFromPaletteAndSidebar(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	sid := e.openLocal()
	e.addSnippet("e2e palette", "echo palette-$((40+2))")
	e.pump(func() bool { return e.inWindow("e2e palette") }, "the snippet in the window")

	// ⌘K: the Snippets group, after Actions, with the Approve first badge; ↵ opens the dialog, not the terminal.
	e.key(ui.Cmd, ui.KeyK)
	e.tt.Type("e2e palette")
	e.step()
	if !e.tt.HasText("Type snippet: e2e palette") || !e.hasLabel("badge Approve first") {
		t.Fatal("the palette row or its badge is missing")
	}
	e.shot("TestE2ESnippetFromPaletteAndSidebar-palette")
	e.key(0, ui.KeyEnter)
	e.waitApprove()
	e.settle(500 * time.Millisecond)
	if strings.Contains(e.scrollback(sid), "palette-") {
		t.Fatal("↵ in the palette typed")
	}
	e.click("Cancel")
	e.pump(func() bool { return e.w.approve == nil }, "Cancel to close")

	// The sidebar's Snippets section: a click opens the dialog too.
	e.click("Snippets")
	e.click("sidebar snippet e2e palette")
	e.waitApprove()
	e.settle(500 * time.Millisecond)
	if strings.Contains(e.scrollback(sid), "palette-") {
		t.Fatal("a sidebar click typed")
	}
	e.click("Type")
	e.pump(func() bool { return e.w.approve == nil }, "the dialog to close")
	e.waitScrollback(sid, "palette-42")
}

func TestE2ESnippetsPageComesBack(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.openLocal()
	e.addSnippet("e2e keep", "uptime")
	e.click("Snippets")
	e.pump(func() bool { return e.inWindow("e2e keep") && e.hasLabel("sidebar snippet e2e keep") }, "the snippet in the sidebar")
	e.click("Manage snippets")
	e.pump(func() bool { return e.w.pageTab("snippets") != nil && e.hasLabel("snippet e2e keep") }, "the Snippets page")
	// Edit through the page: the name and the command, saved with the button.
	e.click("snippet e2e keep")
	e.fillField("Snippet name", "e2e kept")
	e.fillField("Snippet commands", "uname -a")
	e.click("Save")
	e.pump(func() bool {
		s, ok := e.snippetNamed("e2e kept")
		return ok && s.Command == "uname -a\n"
	}, "the edit on the server")
	if _, ok := e.snippetNamed("e2e keep"); ok {
		t.Fatal("the old name is still on the server")
	}
	e.shot("TestE2ESnippetsPageComesBack")
	// "Let the agent suggest this snippet" is off for a new snippet; the switch and Save turn it on, on the server.
	if s, _ := e.snippetNamed("e2e kept"); s.AgentUsable {
		t.Fatal("a new snippet may be suggested by the agent")
	}
	ed := &e.w.pageTab("snippets").pg.snippets.ed
	// The save is done when the window's list holds it too: until then the editor re-reads the old text.
	e.pump(func() bool { s, _ := e.w.snippet(ed.id); return !ed.saving && !ed.dirty() && s.Name == "e2e kept" && ed.name == "e2e kept" }, "the first save to finish")
	e.click("Let the agent suggest this snippet switch")
	e.pump(func() bool { return ed.agent && ed.dirty() }, "the switch on")
	e.click("Save")
	e.pump(func() bool { s, ok := e.snippetNamed("e2e kept"); return ok && s.AgentUsable }, "the flag on the server")
	e.settle(600 * time.Millisecond)
	e.w.closeWindow()

	e2 := newWin(t, addr, dir, nil)
	e2.pump(func() bool { return e2.w.pageTab("snippets") != nil && e2.hasLabel("snippet e2e kept") }, "the Snippets tab to come back with the edit")
	if e2.w.tab().name() != "Snippets" {
		t.Errorf("the page that was shown is not shown again: %s", e2.w.tab().name())
	}
}
