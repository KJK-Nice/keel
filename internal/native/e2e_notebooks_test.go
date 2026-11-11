//go:build nativee2e

package native

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// Notebooks against a real keel serve and a real shell (Local Shell): the document is shown rendered and edited as plain
// Markdown, kept byte for byte, and a fence reaches the shell only through the snippet paths. Every command is a harmless
// echo, df or uptime; the daemon is the test's own (KEEL_KEYRING=off, the fake agent provider).

// apiNotebook makes a notebook on the daemon the way another client would, and waits for the window to list it.
func (e *win) apiNotebook(name, md string) string {
	e.t.Helper()
	code, body := e.raw("POST", "/api/notebooks", map[string]any{"name": name, "cells": []map[string]string{{"id": "c1", "body": md}}})
	if code != 201 {
		e.t.Fatalf("POST /api/notebooks: %d %s", code, body)
	}
	var nb keelclient.Notebook
	if err := json.Unmarshal([]byte(body), &nb); err != nil {
		e.t.Fatal(err)
	}
	e.w.reloadNotebooks()
	e.pump(func() bool { _, ok := e.w.notebook(nb.ID); return ok }, "the notebook "+name+" in the window")
	return nb.ID
}

func (e *win) serverNotebooks() []keelclient.Notebook {
	e.t.Helper()
	c, cancel := ctx()
	defer cancel()
	l, err := e.cl.Notebooks(c)
	if err != nil {
		e.t.Fatal(err)
	}
	return l
}

func (e *win) serverNotebook(id string) keelclient.Notebook {
	e.t.Helper()
	for _, n := range e.serverNotebooks() {
		if n.ID == id {
			return n
		}
	}
	e.t.Fatalf("notebook %s is not on the server", id)
	return keelclient.Notebook{}
}

// openNotebookFromSidebar clicks the notebook's row in the sidebar's Snippets section, as a person does.
func (e *win) openNotebookFromSidebar(name string) *pane {
	e.t.Helper()
	e.click("Snippets")
	e.pump(func() bool { return e.hasLabel("sidebar notebook " + name) }, "the sidebar row of "+name)
	e.click("sidebar notebook " + name)
	var np *pane
	e.pump(func() bool {
		for _, p := range e.w.notebookPanes() {
			if strings.TrimSpace(p.nb.title) == name {
				np = p
			}
		}
		return np != nil
	}, "the notebook pane of "+name)
	return np
}

func TestE2ENotebookCreateEditReopen(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.pump(func() bool { return e.tt.HasText("Local Shell") }, "the host tree")

	// New notebook from the sidebar: a tab of its own, in Edit, with the starter text; it is saved at once.
	e.click("Snippets")
	e.pump(func() bool { return e.hasLabel("New notebook") }, "New notebook in the sidebar")
	e.click("New notebook")
	e.pump(func() bool {
		return len(e.w.notebookPanes()) == 1 && e.w.notebookPanes()[0].nb.id != "" && e.tt.Focused("Markdown document")
	}, "a new notebook in Edit")
	np := e.w.notebookPanes()[0]
	if !np.nb.editing || np.nb.md != defaultNotebookMD {
		t.Fatalf("editing %v md %q", np.nb.editing, np.nb.md)
	}
	// Replace the text: a heading and two fences with nothing between them.
	md := "# Release\n\nCheck the disk, then the load.\n\n```sh\ndf -h\n```\n```sh\nuptime\n```\n"
	e.key(ui.Cmd, ui.KeyA)
	e.tt.Type(md)
	e.step()
	e.click("Done")
	e.pump(func() bool { return !np.nb.auto.pending() && e.serverNotebook(np.nb.id).Markdown() == md }, "the typed Markdown on the server")
	got := e.serverNotebook(np.nb.id)
	if got.Markdown() != md || len(got.Cells) != 1 {
		t.Fatalf("the server holds %q (%d cells), want exactly what was typed %q", got.Markdown(), len(got.Cells), md)
	}
	if np.nb.editing || !e.tt.HasText("Release") || !e.tt.HasText("Check the disk, then the load.") || e.tt.HasText("# Release") {
		t.Fatalf("not rendered after Done: %q", e.tt.Texts())
	}
	// Close the pane, and open it again from the sidebar.
	e.click("Close pane")
	e.pump(func() bool { return len(e.w.notebookPanes()) == 0 }, "the notebook closed")
	np2 := e.openNotebookFromSidebar("Notebook")
	e.pump(func() bool { return e.tt.HasText("Release") && e.tt.HasText("df -h") && e.tt.HasText("uptime") }, "the document rendered again")
	if np2.nb.editing || np2.nb.md != md {
		t.Fatalf("reopened: editing %v md %q", np2.nb.editing, np2.nb.md)
	}
	copies := 0
	for _, s := range e.tt.Texts() {
		if strings.HasPrefix(s, "Copy code ") {
			copies++
		}
	}
	if copies != 2 {
		t.Errorf("%d Copy buttons: two fences are two blocks", copies)
	}
	if e.serverNotebook(np2.nb.id).Markdown() != md {
		t.Error("reopening changed the stored text")
	}
	e.shot("TestE2ENotebookCreateEditReopen")
}

func TestE2ENotebookTypeOneLineFence(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	sid := e.openLocal()
	id := e.apiNotebook("Disk check", "Look at the disk.\n\n```sh\ndf -h\n```\n")
	np := e.openNotebookFromSidebar("Disk check")
	e.pump(func() bool { return len(e.w.tab().panes) == 2 && e.tt.HasText("df -h") }, "the notebook beside the shell")
	fs := fences(np.nb.md)
	e.click(fenceName("Type into Local Shell", fs[0].Index))
	e.pump(func() bool { return strings.Contains(e.scrollback(sid), "df -h") }, "df -h on the prompt line")
	e.settle(time.Second) // if it had run, its output would be here by now
	e.noDialog("a read-only one-line fence")
	if sb := e.scrollback(sid); strings.Contains(sb, "Filesystem") {
		t.Fatalf("the command ran without Enter: %q", sb)
	}
	if !e.tt.HasText(typedNote("Local Shell")) {
		t.Errorf("no 'Typed into…' note: %q", e.tt.Texts())
	}
	found := false
	for _, l := range e.logEntries() {
		if l.Source == "direct" && strings.Contains(strings.ToLower(l.Kind+" "+l.Message), "notebook") {
			found = true
			t.Logf("log row: kind=%q source=%q message=%q", l.Kind, l.Source, l.Message)
		}
	}
	if !found {
		t.Errorf("no direct notebook row in the log: %+v", e.logEntries())
	}
	e.shot("TestE2ENotebookTypeOneLineFence-typed")
	e.pressEnterInPane() // the person's Enter; the client sent none
	e.waitScrollback(sid, "Filesystem")
	if e.serverNotebook(id).Markdown() != "Look at the disk.\n\n```sh\ndf -h\n```\n" {
		t.Error("typing changed the stored text")
	}
}

func TestE2ENotebookTypeTwoLineFenceThroughTheDialog(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	sid := e.openLocal()
	e.apiNotebook("Two lines", "```sh\necho nb-$((40+2))\necho second-line\n```\n")
	np := e.openNotebookFromSidebar("Two lines")
	e.pump(func() bool { return e.tt.HasText("second-line") }, "the notebook")
	fs := fences(np.nb.md)
	e.click(fenceName("Type into Local Shell", fs[0].Index))
	e.pump(func() bool { return e.w.approve != nil && e.hasLabel("dialog "+approveTitle) }, "the dialog")
	if got := e.w.approve.shown(); got != "echo nb-$((40+2))\necho second-line\n" {
		t.Fatalf("the dialog shows %q", got)
	}
	e.settle(time.Second)
	if sb := e.scrollback(sid); strings.Contains(sb, "nb-42") || strings.Contains(sb, "second-line") {
		t.Fatalf("typed before Type: %q", sb)
	}
	e.shot("TestE2ENotebookTypeTwoLineFenceThroughTheDialog-dialog")
	before := e.scrollback(sid)
	e.click("Cancel")
	e.settle(500 * time.Millisecond)
	if e.w.approve != nil || strings.Contains(e.scrollback(sid), "nb-42") || len(e.scrollback(sid)) != len(before) {
		t.Fatal("Cancel typed, or the dialog stayed")
	}
	e.click(fenceName("Type into Local Shell", fs[0].Index))
	e.pump(func() bool { return e.w.approve != nil && e.hasLabel("dialog "+approveTitle) }, "the dialog again")
	e.click("Type")
	e.waitScrollback(sid, "nb-42")
	e.waitScrollback(sid, "second-line")
	for _, l := range e.logEntries() {
		t.Logf("log row: kind=%q source=%q message=%q", l.Kind, l.Source, l.Message)
	}
}

func TestE2ENotebookPlaceholderRefused(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	sid := e.openLocal()
	e.apiNotebook("Placeholder", "```sh\necho keel-ph [secret:1]\n```\n")
	np := e.openNotebookFromSidebar("Placeholder")
	e.pump(func() bool { return e.tt.HasText("keel-ph") }, "the notebook")
	before := e.scrollback(sid)
	fs := fences(np.nb.md)
	e.click(fenceName("Type into Local Shell", fs[0].Index))
	e.pump(func() bool { return e.tt.HasText(placeholderRefused) }, "the app's sentence")
	e.settle(time.Second)
	e.noDialog("a placeholder")
	if sb := e.scrollback(sid); strings.Contains(sb, "keel-ph") || len(sb) != len(before) {
		t.Fatalf("something was typed: %q", sb)
	}
	e.shot("TestE2ENotebookPlaceholderRefused")
}

func TestE2ENotebookAdjacentFencesStaySeparate(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	sid := e.openLocal()
	e.apiNotebook("Adjacent", "```sh\nuptime\n```\n```sh\ndf -h\n```\n")
	np := e.openNotebookFromSidebar("Adjacent")
	e.pump(func() bool { return e.tt.HasText("uptime") && e.tt.HasText("df -h") }, "the notebook")
	fs := fences(np.nb.md)
	if len(fs) != 2 {
		t.Fatalf("fences %+v", fs)
	}
	e.click(fenceName("Type into Local Shell", fs[1].Index))
	e.pump(func() bool { return strings.Contains(e.scrollback(sid), "df -h") }, "the second fence's command")
	e.settle(time.Second)
	if sb := e.scrollback(sid); strings.Contains(sb, "uptime") {
		t.Fatalf("the first fence's code was typed too: %q", sb)
	}
}

func TestE2ENotebookPrivatePinRenameDelete(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.pump(func() bool { return e.tt.HasText("Local Shell") }, "the host tree")
	id := e.apiNotebook("Ops", "keep this text\n")
	e.w.openPage("notebooks")
	e.pump(func() bool { return e.hasLabel("notebook Ops") }, "the Notebooks page row")
	menu := func(item string) {
		e.t.Helper()
		if err := e.tt.RightClick("notebook Ops"); err != nil {
			e.t.Fatal(err)
		}
		e.step()
		if err := e.tt.ChooseMenuItem(item); err != nil {
			e.t.Fatalf("menu %q: %v (items %q)", item, err, e.tt.Menu())
		}
		e.step()
	}
	menu("Pin")
	e.pump(func() bool { return e.serverNotebook(id).Pinned }, "pinned on the server")
	menu("Make private")
	e.pump(func() bool { return e.serverNotebook(id).Private }, "private on the server")
	e.pump(func() bool { return e.hasLabel("badge Private") }, "the Private badge")
	menu("Let the agent read it")
	e.pump(func() bool { return !e.serverNotebook(id).Private }, "not private on the server")
	menu("Rename")
	e.pump(func() bool { return e.tt.Focused("Notebook rename name") }, "the rename field")
	e.key(ui.Cmd, ui.KeyA)
	e.tt.Type("Ops renamed")
	e.step()
	e.click("Rename")
	e.pump(func() bool { return e.serverNotebook(id).Name == "Ops renamed" }, "renamed on the server")
	if got := e.serverNotebook(id); got.Markdown() != "keep this text\n" || !got.Pinned {
		t.Fatalf("a rename changed the text or the pin: %+v", got)
	}
	e.pump(func() bool { return e.hasLabel("notebook Ops renamed") }, "the renamed row")
	e.shot("TestE2ENotebookPrivatePinRenameDelete")
	// Delete, and restore it from Recently deleted.
	if err := e.tt.RightClick("notebook Ops renamed"); err != nil {
		t.Fatal(err)
	}
	e.step()
	e.tt.ChooseMenuItem("Delete")
	e.step()
	e.pump(func() bool { return e.tt.HasText("Delete notebook “Ops renamed”?") }, "the question")
	e.click("Remove")
	e.pump(func() bool { return len(e.serverNotebooks()) == 0 }, "deleted on the server")
	e.click("Recently deleted")
	e.pump(func() bool {
		return e.tt.HasText("Recently deleted") && e.hasLabel("dialog Recently deleted") && e.tt.HasText("keep this text")
	}, "the trash with its text")
	e.click("Restore notebook")
	e.pump(func() bool { return len(e.serverNotebooks()) == 1 }, "restored on the server")
	if got := e.serverNotebooks()[0]; got.Name != "Ops renamed" || got.Markdown() != "keep this text\n" {
		t.Fatalf("restored %+v", got)
	}
	e.pump(func() bool { return e.tt.HasText("Restored “Ops renamed” to Notebooks.") }, "the toast")
}

func TestE2ENotebookHistoryRestore(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.openLocal()
	e.putAgentKey()
	first := "# Hist\n\nkeel-edit-me\n"
	id := e.apiNotebook("Hist", first)
	// The agent (the test's fake provider) edits it: keel serve keeps the version before the edit in History.
	code, body := e.raw("POST", "/api/agent/ask", map[string]any{"conversationId": "conv-history-1", "question": "edit the notebook hist", "panes": []string{}})
	if code != 200 {
		t.Fatalf("ask: %d %s", code, body)
	}
	e.pump(func() bool { return e.serverNotebook(id).AgentUnreviewed }, "the agent's edit on the server")
	edited := e.serverNotebook(id).Markdown()
	if !strings.Contains(edited, "keel-edited-by-agent") {
		t.Fatalf("the agent's edit: %q", edited)
	}
	e.w.reloadNotebooks()
	np := e.openNotebookFromSidebar("Hist")
	e.pump(func() bool {
		return strings.Contains(e.texts(), "Edited by the agent") && e.tt.HasText("Mark reviewed")
	}, "the review banner")
	e.shot("TestE2ENotebookHistoryRestore-banner")
	// History: the version before the agent's edit is first; restore it.
	e.click("History")
	e.pump(func() bool { return e.hasLabel("dialog History · Hist") && e.tt.HasText("Before the agent's edit") }, "History")
	e.pump(func() bool { return e.tt.HasText("From this version to now:") }, "the diff")
	e.shot("TestE2ENotebookHistoryRestore-dialog")
	e.click("Restore this version")
	e.pump(func() bool { return e.serverNotebook(id).Markdown() == first }, "the first version restored on the server")
	e.pump(func() bool { return np.nb.md == first && e.tt.HasText(restoredWord) }, "the pane showing the restored text and the toast")
	if e.serverNotebook(id).Markdown() != first {
		t.Error("not restored")
	}
}

func TestE2ENotebookRestoredWithTheWorkspace(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	sid := e.openLocal()
	e.apiNotebook("Kept", "a sentence that only the server may keep\n\n```sh\ndf -h\n```\n")
	e.openNotebookFromSidebar("Kept")
	e.pump(func() bool { return len(e.w.tab().panes) == 2 }, "the notebook beside the shell")
	e.w.closeWindow()
	b, err := os.ReadFile(filepath.Join(dir, "keel-native-workspace.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "a sentence that only") || strings.Contains(string(b), "df -h") || !strings.Contains(string(b), "notebookId") {
		t.Fatalf("the workspace file: %s", b)
	}
	e2 := newWin(t, addr, dir, nil)
	e2.pump(func() bool { return len(e2.w.tabs) == 1 && len(e2.w.tab().panes) == 2 }, "the tab with its two panes")
	e2.pump(func() bool { return e2.tt.HasText("a sentence that only the server may keep") }, "the notebook text, read from the server")
	if got := e2.sidOf(0); got != sid && !slices.Contains(e2.sessionIDs(), sid) {
		t.Errorf("the shell did not come back: %s", got)
	}
	e2.shot("TestE2ENotebookRestoredWithTheWorkspace")
}
