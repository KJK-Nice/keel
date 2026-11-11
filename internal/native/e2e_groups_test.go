//go:build nativee2e

package native

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// Folders, the Group page and group runs against a real keel serve that answers from its in-process fake
// (KEEL_AGENT_PROVIDER=fake, set for every daemon by startServeAt): nothing here reaches a model provider or calls keel mcp's
// routes. Hosts are Local Shells (authType local) made through the API, each in its own scratch /bin/sh; every command a run
// types is a harmless echo and every value made up. A group run proposes nothing without a pane open, so each scenario opens
// the hosts' sessions as a person does, by clicking them.

func (e *win) apiLocalHost(name string, readable bool) string {
	e.t.Helper()
	code, body := e.raw("POST", "/api/hosts", map[string]any{"name": name, "authType": "local", "agentReadable": readable})
	if code != 200 && code != 201 {
		e.t.Fatalf("POST /api/hosts %s: %d %s", name, code, body)
	}
	var h struct{ ID string }
	if err := json.Unmarshal([]byte(body), &h); err != nil || h.ID == "" {
		e.t.Fatalf("host %s: %v %s", name, err, body)
	}
	return h.ID
}

func (e *win) apiGroup(name, parent string) keelclient.Group {
	e.t.Helper()
	c, cancel := ctx()
	defer cancel()
	g, err := e.cl.SaveGroup(c, keelclient.Group{Name: name, ParentID: parent})
	if err != nil {
		e.t.Fatal(err)
	}
	return g
}

func (e *win) apiMove(ids []string, group string) {
	e.t.Helper()
	c, cancel := ctx()
	defer cancel()
	if err := e.cl.MoveHosts(c, ids, group); err != nil {
		e.t.Fatal(err)
	}
}

func (e *win) apiGroups() []keelclient.Group {
	e.t.Helper()
	c, cancel := ctx()
	defer cancel()
	g, err := e.cl.Groups(c)
	if err != nil {
		e.t.Fatal(err)
	}
	return g
}

func (e *win) apiGroupNamed(name string) (keelclient.Group, bool) {
	for _, g := range e.apiGroups() {
		if g.Name == name {
			return g, true
		}
	}
	return keelclient.Group{}, false
}

func (e *win) apiRunbook(name string, steps ...keelclient.RunbookStepInput) string {
	e.t.Helper()
	c, cancel := ctx()
	defer cancel()
	res, err := e.cl.SaveRunbook(c, "", keelclient.RunbookInput{Name: name, Steps: steps})
	if err != nil {
		e.t.Fatal(err)
	}
	return res.Runbook.ID
}

// reread makes the window read the hosts and the folders again, and waits for the tree to show label.
func (e *win) reread(label string) {
	e.t.Helper()
	e.w.reloadHosts()
	e.pump(func() bool { return e.hasLabel(label) }, label+" in the tree")
}

// openHostPane clicks a host in the tree and waits for its session; it returns the session id.
func (e *win) openHostPane(name string) string {
	e.t.Helper()
	e.click("host " + name)
	var sid string
	e.pump(func() bool {
		sid = e.sidOfHost(name)
		return sid != "" && strings.Contains(e.scrollback(sid), "$ ")
	}, "the session on "+name)
	return sid
}

func (e *win) sidOfHost(name string) string {
	for _, t := range e.w.tabs {
		for _, p := range t.panes {
			if p.host.Name == name && p.sessionID() != "" {
				return p.sessionID()
			}
		}
	}
	return ""
}

func (e *win) menuPick(label, item string) {
	e.t.Helper()
	if err := e.tt.RightClick(label); err != nil {
		e.t.Fatal(err)
	}
	e.step()
	if err := e.tt.ChooseMenuItem(item); err != nil {
		e.t.Fatal(err)
	}
	e.step()
}

func (e *win) logTypes() []string { return e.logMessages("agent.type") }

// groupRun is the run page's run, as the window holds it.
func (e *win) groupRun() *keelclient.GroupRun {
	if p := e.w.groupRunPageState(); p != nil {
		return p.run
	}
	return nil
}

// startRunFromGroupPage opens the folder's page from its menu and starts the runbook from "Run a runbook on this group".
func (e *win) startRunFromGroupPage(folder, runbook string) {
	e.t.Helper()
	e.menuPick("folder "+folder, "Roles and runbooks")
	e.pump(func() bool { return e.w.groupPageState() != nil && e.hasLabel("Run a runbook on this group") }, "the Group page")
	e.click("Run a runbook on this group")
	e.pump(func() bool { return e.hasLabel("run runbook " + runbook) }, "the runbook in the list")
	e.click("run runbook " + runbook)
	e.pump(func() bool { return e.groupRun() != nil && e.w.tab() == e.w.pageTab("grouprun") }, "the run page")
}

func (e *win) backToRun() {
	e.t.Helper()
	e.click("tab " + e.w.pageTab("grouprun").name())
}

// ---- folders ----

func TestE2EFolders(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	hostID := e.apiLocalHost("fx-host", true)
	e.reread("host fx-host")

	// New group, from the more-host-actions menu: a folder in the vault, in the tree.
	if err := e.tt.Click("More host actions"); err != nil {
		t.Fatal(err)
	}
	e.step()
	if err := e.tt.ChooseMenuItem("New group"); err != nil {
		t.Fatal(err)
	}
	e.pump(func() bool { return e.hasLabel("dialog New group") }, "the New group dialog")
	e.fillField("Group label", "fx-top")
	e.click("Save group")
	e.pump(func() bool { _, ok := e.apiGroupNamed("fx-top"); return ok && e.hasLabel("folder fx-top") }, "the folder")
	top, _ := e.apiGroupNamed("fx-top")

	// New subgroup, from its menu.
	e.menuPick("folder fx-top", "New subgroup")
	e.pump(func() bool { return e.hasLabel("dialog New group") }, "the New group dialog for the subgroup")
	e.fillField("Group label", "fx-sub")
	e.click("Save group")
	e.pump(func() bool {
		g, ok := e.apiGroupNamed("fx-sub")
		return ok && g.ParentID == top.ID && e.hasLabel("folder fx-sub")
	}, "the subfolder under fx-top")
	sub, _ := e.apiGroupNamed("fx-sub")

	// Move a host by the menu: Move to folder… is Edit host, whose Folder field is the same field.
	e.menuPick("host fx-host", "Move to folder…")
	e.pump(func() bool { return e.hasLabel("dialog Edit host") }, "Edit host")
	e.click("Folder picker")
	e.step()
	if err := e.tt.ChooseMenuItem("fx-top"); err != nil {
		t.Fatal(err)
	}
	e.step()
	e.click("Save host")
	e.pump(func() bool { h, ok := e.hostNamed("fx-host"); return ok && h.GroupID == top.ID }, "the host in fx-top, on the server")
	e.pump(func() bool { return e.hasLabel("folder fx-top") && !e.hasLabel("dialog Edit host") }, "the dialog closed")
	_ = hostID

	// Move it by drag onto the subfolder.
	e.drag("host fx-host", "folder fx-sub")
	e.pump(func() bool { h, _ := e.hostNamed("fx-host"); return h.GroupID == sub.ID }, "the host in fx-sub after the drag")

	// A folder dragged onto its own descendant is refused with the app's words and nothing changes.
	e.drag("folder fx-top", "folder fx-sub")
	e.pump(func() bool { return e.hasLabel("toast " + msgGroupCycle) }, "the refusal")
	if g, _ := e.apiGroupNamed("fx-top"); g.ParentID != "" {
		t.Errorf("fx-top moved under %q", g.ParentID)
	}

	// Rename in the tree.
	e.menuPick("folder fx-top", "Rename")
	e.pump(func() bool { return e.hasLabel("Rename folder") }, "the name field")
	e.step() // the field takes the keyboard on the frame after it shows
	e.key(ui.Cmd, ui.KeyA)
	e.tt.Type("fx-renamed")
	e.step()
	e.key(0, ui.KeyEnter)
	e.pump(func() bool { _, ok := e.apiGroupNamed("fx-renamed"); return ok && e.hasLabel("folder fx-renamed") }, "the renamed folder")

	// Delete asks first; Cancel changes nothing; Remove moves the host and the subfolder up one level.
	e.menuPick("folder fx-renamed", "Delete")
	e.pump(func() bool { return e.tt.HasText("Remove group fx-renamed? Hosts and subgroups move up one level.") }, "the confirm")
	e.click("Cancel")
	e.step()
	if _, ok := e.apiGroupNamed("fx-renamed"); !ok {
		t.Fatal("Cancel deleted the folder")
	}
	e.menuPick("folder fx-renamed", "Delete")
	e.pump(func() bool { return e.tt.HasText("Remove group fx-renamed? Hosts and subgroups move up one level.") }, "the confirm again")
	e.click("Remove")
	e.pump(func() bool { _, ok := e.apiGroupNamed("fx-renamed"); return !ok }, "the folder gone")
	e.pump(func() bool {
		g, ok := e.apiGroupNamed("fx-sub")
		return ok && g.ParentID == "" && e.hasLabel("folder fx-sub")
	}, "the subfolder at the top")
	h, _ := e.hostNamed("fx-host")
	if h.GroupID != sub.ID {
		t.Errorf("the host is in %q, want fx-sub (the folder it was in was not deleted)", h.GroupID)
	}
	e.shot("TestE2EFolders")
}

// drag presses on one labelled element and lets go over another.
func (e *win) drag(from, to string) {
	e.t.Helper()
	a, ok := e.tt.Find(from)
	if !ok {
		e.t.Fatalf("no %q", from)
	}
	b, ok := e.tt.Find(to)
	if !ok {
		e.t.Fatalf("no %q", to)
	}
	e.tt.Press(a.X+a.W/2, a.Y+a.H/2)
	e.tt.Move(a.X+a.W/2+12, a.Y+a.H/2+12)
	e.tt.Move(b.X+b.W/2, b.Y+b.H/2)
	e.step()
	e.tt.Release(b.X+b.W/2, b.Y+b.H/2)
	e.step()
}

// ---- the Group page ----

const e2eVarSecret = "AKIAIOSFODNN7EXAMPLE" // AWS's documented example key: a secret-looking value keel serve refuses

func TestE2EGroupPageRolesNotesVars(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	a := e.apiLocalHost("gp-1", true)
	b := e.apiLocalHost("gp-2", true)
	g := e.apiGroup("gp-folder", "")
	e.apiMove([]string{a, b}, g.ID)
	e.reread("folder gp-folder")
	e.menuPick("folder gp-folder", "Roles and runbooks")
	e.pump(func() bool {
		return e.w.groupPageState() != nil && e.hasLabel("member gp-1") && e.hasLabel("member gp-2")
	}, "the Group page with its hosts")

	// Roles: a new role for gp-1, the same from the menu for gp-2.
	e.click("Role of gp-1 picker")
	e.step()
	if err := e.tt.ChooseMenuItem("New role…"); err != nil {
		t.Fatal(err)
	}
	e.pump(func() bool { return e.hasLabel("New role for gp-1") }, "the new role field")
	e.tt.Type("worker")
	e.key(0, ui.KeyEnter)
	e.pump(func() bool { return e.roleOf(g.ID, a) == "worker" }, "gp-1's role on the server")
	e.click("Role of gp-2 picker")
	e.step()
	if err := e.tt.ChooseMenuItem("worker"); err != nil {
		t.Fatal(err)
	}
	e.pump(func() bool { return e.roleOf(g.ID, b) == "worker" }, "gp-2's role on the server")
	e.pump(func() bool { return e.hasLabel("2 worker") }, "the chip")

	// A group note: on the server for the group, not for a host.
	e.click("Add a note")
	e.tt.Type("gp-note: drain a worker before restarting containerd")
	e.step()
	e.key(0, ui.KeyEnter)
	e.pump(func() bool {
		for _, n := range e.serverNotes() {
			if n.GroupID == g.ID && n.HostID == "" && strings.Contains(n.Text, "gp-note") {
				return true
			}
		}
		return false
	}, "the group note on the server")

	// Variables: one is kept, one a secret-looking value keel serve refuses.
	// The note's answer gives its field the keyboard on the next frame: wait for that before moving to the variables.
	e.pump(func() bool { return len(e.w.groupPageState().notes.notes) == 1 && e.tt.Focused("Add a note") }, "the note listed and its field focused")
	e.fillField("Variable name", "k8s_version")
	e.fillField("Variable value", "1.31.2")
	e.click("Add variable")
	e.pump(func() bool { return e.groupVars(g.ID) == "k8s_version=1.31.2" }, "the variable on the server")
	e.fillField("Variable name", "api_token")
	e.fillField("Variable value", e2eVarSecret)
	e.click("Add variable")
	e.pump(func() bool { return strings.Contains(e.texts(), "Keep secrets in the vault") }, "keel serve's refusal")
	if got := e.groupVars(g.ID); got != "k8s_version=1.31.2" {
		t.Fatalf("variables on the server: %q", got)
	}
	e.fillField("Variable value", "") // the refused value leaves the field
	e.fillField("Variable name", "")
	e.step()
	e.shot("TestE2EGroupPageRolesNotesVars")

	// The window closes and comes back on the same page; the secret is in no shot, log, pref or the workspace file.
	e.w.closeWindow()
	e.settle(300 * time.Millisecond)
	e2 := newWin(t, addr, dir, nil)
	e2.pump(func() bool {
		return e2.w.groupPageState() != nil && e2.hasLabel("member gp-1") && e2.tt.HasText("k8s_version") && e2.hasLabel("2 worker")
	}, "the Group page back")
	if p := e2.w.groupPageState(); p == nil || p.id != g.ID {
		t.Fatalf("the page came back on %+v", p)
	}
	e2.noSecretDrawn(e2e2VarSecretList()...)
	for _, l := range e2.logEntries() {
		if strings.Contains(l.Message, e2eVarSecret) {
			t.Errorf("the log holds the refused value: %s", l.Kind)
		}
	}
	files, _ := filepath.Glob(filepath.Join(dir, "keel-native*"))
	for _, f := range files {
		if b, _ := os.ReadFile(f); strings.Contains(string(b), e2eVarSecret) {
			t.Errorf("%s holds the refused value", f)
		}
	}
}

func e2e2VarSecretList() []string { return []string{e2eVarSecret} }

func (e *win) roleOf(groupID, hostID string) string {
	c, cancel := ctx()
	defer cancel()
	info, err := e.cl.GroupMembers(c, groupID)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, m := range info.Members {
		if m.HostID == hostID {
			return m.Role
		}
	}
	return ""
}

func (e *win) groupVars(groupID string) string {
	c, cancel := ctx()
	defer cancel()
	info, err := e.cl.GroupMembers(c, groupID)
	if err != nil {
		e.t.Fatal(err)
	}
	var out []string
	for _, v := range info.Group.Vars {
		out = append(out, v.Name+"="+v.Value)
	}
	return strings.Join(out, ",")
}

// ---- group runs ----

// runSetup makes a folder of n Local Shell hosts wk-1…, a runbook of the given commands (every host), starts the run from the
// Group page and opens a pane on each host. The window polls fast so keel serve's proposals arrive quickly.
func runSetup(t *testing.T, n int, commands ...string) (*win, []string, string) {
	t.Helper()
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.w.startPoll(200 * time.Millisecond)
	var ids []string
	for i := 1; i <= n; i++ {
		ids = append(ids, e.apiLocalHost(fmt.Sprintf("wk-%d", i), true))
	}
	g := e.apiGroup("gx-run", "")
	e.apiMove(ids, g.ID)
	var steps []keelclient.RunbookStepInput
	for i, c := range commands {
		steps = append(steps, keelclient.RunbookStepInput{Title: fmt.Sprintf("Step %d", i+1), Command: c})
	}
	e.apiRunbook("gx print", steps...)
	e.reread("folder gx-run")
	e.startRunFromGroupPage("gx-run", "gx print")
	return e, ids, g.ID
}

func (e *win) setMode(mode string) {
	e.t.Helper()
	label := map[string]string{"parallel": "Parallel", "rolling": "Rolling"}[mode]
	e.click(label)
	e.pump(func() bool { return e.groupRun() != nil && e.groupRun().Mode == mode }, mode+" mode")
}

// openPanes opens a session on each host and comes back to the run page.
func (e *win) openRunPanes(hosts ...string) (sids []string) {
	e.t.Helper()
	for _, h := range hosts {
		sids = append(sids, e.openHostPane(h))
	}
	e.backToRun()
	return sids
}

func (e *win) cellState(step int, host string) string {
	if r := e.groupRun(); r != nil && step < len(r.Steps) {
		for _, c := range r.Steps[step].Cells {
			if c.Host == host {
				return c.State
			}
		}
	}
	return ""
}

func (e *win) waitTypeAll(n int) {
	e.t.Helper()
	label := fmt.Sprintf("Type into %d panes…", n)
	if n == 1 {
		label = "Type into 1 pane…"
	}
	e.pump(func() bool { return e.hasLabel(label) }, label)
}

// rowsOf are the open group dialog's rows.
func (e *win) dialogRows() []*groupRow {
	if e.w.approve == nil || e.w.approve.group == nil {
		return nil
	}
	return e.w.approve.group.rows
}

func (e *win) holds(sid, s string) bool { return strings.Contains(e.scrollback(sid), s) }

func TestE2EGroupRunParallel(t *testing.T) {
	e, _, _ := runSetup(t, 2, "echo grp-1", "echo grp-2")
	e.setMode("parallel")
	sids := e.openRunPanes("wk-1", "wk-2")
	e.waitTypeAll(2)

	// The group dialog: two rows, none ticked, the exact bytes once. Type with nothing ticked is not possible.
	e.click("Type into 2 panes…")
	e.pump(func() bool { return len(e.dialogRows()) == 2 }, "the group dialog")
	for _, r := range e.dialogRows() {
		if r.checked {
			t.Fatalf("%s is ticked in advance", r.host)
		}
	}
	if !e.tt.HasText("1 line · 12 bytes · the same bytes in every pane") || !e.tt.HasText("Tick the hosts to type into") {
		t.Errorf("the dialog does not say what it types: %v", e.tt.Texts())
	}
	e.shot("TestE2EGroupRunParallel-dialog")
	e.w.approveType(false)
	e.w.approveType(true)
	e.settle(300 * time.Millisecond)
	if e.holds(sids[0], "grp-1") || e.holds(sids[1], "grp-1") || len(e.logTypes()) != 0 {
		t.Fatal("something was typed with nothing ticked")
	}

	// Tick wk-1 only: only its scrollback gets grp-1.
	e.click("Type into wk-1")
	e.click("Type into 1 pane")
	e.pump(func() bool { return e.holds(sids[0], "grp-1") && e.w.approve == nil }, "grp-1 in wk-1 and the dialog closed")
	e.settle(300 * time.Millisecond)
	if e.holds(sids[1], "grp-1") {
		t.Fatal("the unticked wk-2 received the command")
	}
	e.pump(func() bool { return e.cellState(0, "wk-1") == "typed" && e.cellState(0, "wk-2") == "proposed" }, "wk-1 typed and wk-2 still waiting")

	// Then the other one.
	e.waitTypeAll(1)
	e.click("Type into 1 pane…")
	e.pump(func() bool { return len(e.dialogRows()) == 1 }, "the one-host dialog")
	if !e.dialogRows()[0].checked || e.dialogRows()[0].host != "wk-2" {
		t.Fatalf("the one-host dialog: %+v", e.dialogRows()[0])
	}
	e.click("Type into 1 pane")
	e.pump(func() bool { return e.holds(sids[1], "grp-1") && e.w.approve == nil }, "grp-1 in wk-2")

	// No shell sends marks: the user marks each host done, and step 2 is proposed to both.
	e.pump(func() bool { return e.cellState(0, "wk-1") == "typed" && e.cellState(0, "wk-2") == "typed" }, "both typed")
	e.click("Mark done wk-1")
	e.click("Mark done wk-2")
	e.pump(func() bool {
		return e.groupRun().Current == 1 && e.cellState(1, "wk-1") == "proposed" && e.cellState(1, "wk-2") == "proposed"
	}, "step 2 proposed for both")
	e.waitTypeAll(2)

	// Step 2 for both in one approval.
	e.click("Type into 2 panes…")
	e.pump(func() bool { return len(e.dialogRows()) == 2 }, "the step 2 dialog")
	e.click("Type into wk-1")
	e.click("Type into wk-2")
	e.click("Type into 2 panes")
	e.pump(func() bool { return e.holds(sids[0], "grp-2") && e.holds(sids[1], "grp-2") && e.w.approve == nil }, "grp-2 in both")

	// One agent.type per host × step, and no text in the log.
	e.pump(func() bool { return len(e.logTypes()) == 4 }, "four agent.type rows")
	for _, m := range e.logTypes() {
		if strings.Contains(m, "grp-") || strings.Contains(m, "echo") {
			t.Errorf("agent.type holds the command: %q", m)
		}
	}
	e.settle(300 * time.Millisecond)
	e.shot("TestE2EGroupRunParallel")
}

func TestE2EGroupRunRolling(t *testing.T) {
	e, _, _ := runSetup(t, 2, "echo grp-1")
	sids := e.openRunPanes("wk-1", "wk-2") // rolling is the default
	if e.groupRun().Mode != "rolling" {
		t.Fatalf("mode %q", e.groupRun().Mode)
	}
	// Only the first host is offered; the second only after the first is done.
	e.waitTypeAll(1)
	e.settle(600 * time.Millisecond)
	if e.cellState(0, "wk-2") == "proposed" {
		t.Fatal("rolling proposed the second host before the first was done")
	}
	e.click("Type into 1 pane…")
	e.pump(func() bool {
		return len(e.dialogRows()) == 1 && e.dialogRows()[0].host == "wk-1" && e.dialogRows()[0].checked
	}, "the one-host dialog for wk-1")
	e.click("Type into 1 pane")
	e.pump(func() bool { return e.holds(sids[0], "grp-1") && e.w.approve == nil }, "grp-1 in wk-1")
	e.settle(600 * time.Millisecond)
	if e.cellState(0, "wk-2") == "proposed" || e.holds(sids[1], "grp-1") {
		t.Fatal("the second host was offered, or typed, while the first was still waiting for its result")
	}
	e.pump(func() bool { return e.cellState(0, "wk-1") == "typed" }, "wk-1 typed")
	e.click("Mark done wk-1")
	e.pump(func() bool { return e.cellState(0, "wk-2") == "proposed" }, "wk-2 offered after wk-1 is done")
	e.waitTypeAll(1)
	e.click("Type into 1 pane…")
	e.pump(func() bool { return len(e.dialogRows()) == 1 && e.dialogRows()[0].host == "wk-2" }, "the dialog for wk-2")
	e.click("Type into 1 pane")
	e.pump(func() bool { return e.holds(sids[1], "grp-1") }, "grp-1 in wk-2")
	e.pump(func() bool { return e.cellState(0, "wk-2") == "typed" }, "wk-2 typed")
	e.click("Mark done wk-2")
	e.pump(func() bool { return e.groupRun().Status == "finished" }, "the run finished")
	for _, s := range []string{"marked done", "All steps are done or skipped."} {
		e.pump(func() bool { return strings.Contains(e.texts(), s) }, s)
	}
	e.shot("TestE2EGroupRunRolling")
}

func TestE2EGroupRunVariables(t *testing.T) {
	const value = "7421905" // made up; a port number
	e, _, gid := runSetup(t, 1, "echo port-{{port}}")
	c, cancel := ctx()
	if _, err := e.cl.SetGroupVar(c, gid, "port", value); err != nil {
		t.Fatal(err)
	}
	cancel()
	sids := e.openRunPanes("wk-1")
	e.waitTypeAll(1)
	// The run page shows the name and never the value.
	if !e.tt.HasText("Uses 1 group variable · filled in at Type") || !strings.Contains(e.texts(), "{{port}}") || strings.Contains(e.texts(), value) {
		t.Fatalf("the run page before Type: %v", e.tt.Texts())
	}
	e.click("Type into 1 pane…")
	e.pump(func() bool { return len(e.dialogRows()) == 1 }, "the dialog")
	// The dialog shows the filled bytes.
	if !strings.Contains(e.texts(), "echo port-"+value) || !strings.Contains(e.texts(), "{{port}} group variable") {
		t.Errorf("the dialog does not show the filled bytes and where they came from: %v", e.tt.Texts())
	}
	e.shot("TestE2EGroupRunVariables-dialog")
	e.click("Type into 1 pane")
	e.pump(func() bool { return e.holds(sids[0], "port-"+value) && e.w.approve == nil }, "the filled command in the scrollback")
	// After the dialog the page is back to names; the log holds the number of values, never one.
	e.pump(func() bool { return len(e.logTypes()) == 1 }, "agent.type")
	if strings.Contains(e.texts(), value) {
		t.Error("the value is on the run page after the Type")
	}
	for _, l := range e.logEntries() {
		if strings.Contains(l.Message, value) {
			t.Errorf("a log row holds the value: %s %q", l.Kind, l.Message)
		}
	}
	// A variable with no value stops that host's Type before anything is written: remove it and ask again.
	c, cancel = ctx()
	if _, err := e.cl.SetGroupVar(c, gid, "port", ""); err != nil {
		t.Fatal(err)
	}
	cancel()
}

func TestE2EGroupRunFollowerAndClosedPane(t *testing.T) {
	e, _, _ := runSetup(t, 3, "echo grp-1")
	e.setMode("parallel")
	sids := e.openRunPanes("wk-1", "wk-2", "wk-3")
	e.waitTypeAll(3)
	e.click("Type into 3 panes…")
	e.pump(func() bool { return len(e.dialogRows()) == 3 }, "the dialog with three rows")

	// While the dialog is open another client takes control of wk-2's session (this window follows it) and wk-3's pane is closed.
	other, err := e.cl.Attach(sids[1], 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	go io.Copy(io.Discard, other) // a connection is read for its frames to be handled
	e.pump(func() bool { return other.Snapshot().Status == "ready" }, "the other client attached")
	// This window owns the session, so it passes control to the other client (as TestE2EFollowerAndTakeControl does): from
	// then on this window follows wk-2's pane.
	var wk2 *pane
	for _, tb := range e.w.tabs {
		for _, p := range tb.panes {
			if p.host.Name == "wk-2" {
				wk2 = p
			}
		}
	}
	e.pump(func() bool { return other.Snapshot().YouID != 0 }, "the other client's id")
	if err := wk2.conn.(*keelclient.Conn).GrantControl(other.Snapshot().YouID); err != nil {
		t.Fatal(err)
	}
	var wk3 *pane
	var wk3tab *tab
	for _, tb := range e.w.tabs {
		for _, p := range tb.panes {
			if p.host.Name == "wk-3" {
				wk3, wk3tab = p, tb
			}
		}
	}
	e.w.closePane(wk3tab, wk3.id)
	e.pump(func() bool { return e.tt.HasText(msgGroupPaneFollow) && e.tt.HasText(msgGroupPaneGone) }, "both rows saying why they cannot be typed")
	e.shot("TestE2EGroupRunFollowerAndClosedPane")

	// Ticking all three: the two that cannot be typed are not counted, and only wk-1 types.
	for _, h := range []string{"wk-1", "wk-2", "wk-3"} {
		_ = e.tt.Click("Type into " + h) // a disabled box ignores the press
		e.step()
	}
	e.pump(func() bool { return e.hasLabel("Type into 1 pane") }, "the button counting only wk-1")
	e.click("Type into 1 pane")
	e.pump(func() bool { return e.holds(sids[0], "grp-1") && e.w.approve == nil }, "grp-1 in wk-1")
	e.settle(400 * time.Millisecond)
	if e.holds(sids[1], "grp-1") {
		t.Error("the followed pane was typed into")
	}
	if len(e.logTypes()) != 1 {
		t.Errorf("%d agent.type rows, want 1", len(e.logTypes()))
	}
	// Their proposals still wait in keel serve, but the window offers neither the followed pane nor the closed one for typing.
	e.pump(func() bool {
		return e.cellState(0, "wk-2") == "proposed" && e.cellState(0, "wk-3") == "proposed" && e.hasLabel("Nothing to type now")
	}, "wk-2 and wk-3 waiting and nothing offered")
	if cells := typeable(e.groupRun(), e.w.groupPaneOpen); len(cells) != 0 {
		t.Errorf("typeable: %v", cells)
	}
}

// ---- the Ansible export ----

func TestE2EAnsibleExport(t *testing.T) {
	const old = "bluewhale-fixture-77" // made up; saved as a variable first, then as a host's password
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	a := e.apiLocalHost("an-1", true)
	b := e.apiLocalHost("an-2", true)
	g := e.apiGroup("an-folder", "")
	e.apiMove([]string{a, b}, g.ID)
	c, cancel := ctx()
	for _, v := range [][2]string{{"k8s_version", "1.31.2"}, {"old_pw", old}} {
		if _, err := e.cl.SetGroupVar(c, g.ID, v[0], v[1]); err != nil {
			t.Fatal(err)
		}
	}
	cancel()
	// The value then goes into the vault as a password: the export leaves the variable out.
	if code, body := e.raw("POST", "/api/hosts", map[string]any{"name": "an-pw", "address": "10.9.9.9", "port": 22, "username": "ops", "authType": "password", "password": old}); code != 200 && code != 201 {
		t.Fatalf("POST /api/hosts: %d %s", code, body)
	}
	e.apiRunbook("an print", keelclient.RunbookStepInput{Title: "Print", Command: "echo {{k8s_version}}"})
	e.reread("folder an-folder")
	e.menuPick("folder an-folder", "Roles and runbooks")
	e.pump(func() bool { return e.w.groupPageState() != nil && e.hasLabel("member an-1") }, "the Group page")

	e.click("Export to Ansible")
	e.pump(func() bool { p := e.w.groupPageState(); return p != nil && len(p.ex.files) == 1 }, "the inventory")
	p := e.w.groupPageState()
	inv := p.ex.files[0]
	if inv.Name != "inventory.ini" || !strings.Contains(inv.Content, "an-1") || !strings.Contains(inv.Content, "k8s_version") {
		t.Fatalf("inventory %+v", inv)
	}
	// What the server withheld is not in the export. (The page's own variables list still shows the value, as the app's does:
	// a variable is config until the vault holds the same value, and the list is not the export.)
	if strings.Contains(inv.Content, old) {
		t.Fatal("a withheld value is in the export")
	}
	if !strings.Contains(inv.Content, "old_pw") || !strings.Contains(inv.Content, "left out") {
		t.Errorf("the inventory does not say old_pw was left out: %s", inv.Content)
	}
	if !e.tt.HasText("inventory.ini") || !strings.Contains(e.texts(), "left out") {
		t.Errorf("the drawer does not show the file: %v", e.tt.Texts())
	}
	// Copy puts the shown file on the clipboard, exactly.
	e.click("Copy")
	if got := e.tt.Clipboard(); got != inv.Content {
		t.Errorf("the clipboard holds %q, not the inventory", got)
	}
	e.click("Close notification")
	// Download saves the shown file where the save dialog (the test's) says, exactly as keel serve built it.
	saved := filepath.Join(t.TempDir(), "downloaded.ini")
	e.w.saveDialog = func(name string) (string, error) { return saved, nil }
	e.click("Download")
	e.pump(func() bool { b, err := os.ReadFile(saved); return err == nil && string(b) == inv.Content }, "the downloaded file to hold the inventory")
	e.click("Close notification")
	// YAML and a playbook from the runbook are asked of keel serve and shown as it built them.
	e.click("YAML")
	e.pump(func() bool { return len(p.ex.files) == 1 && p.ex.files[0].Name == "inventory.yml" }, "the YAML inventory")
	e.click("Playbook from a runbook picker")
	e.step()
	if err := e.tt.ChooseMenuItem("Playbook: an print"); err != nil {
		t.Fatal(err)
	}
	e.pump(func() bool { return len(p.ex.files) == 2 && strings.HasPrefix(p.ex.files[1].Name, "playbooks/") }, "the playbook")
	if !strings.Contains(p.ex.files[1].Content, "k8s_version | quote") || strings.Contains(p.ex.files[1].Content, old) {
		t.Errorf("playbook %s", p.ex.files[1].Content)
	}
	e.shot("TestE2EAnsibleExport")
	// The window wrote nothing but its own files: no export file in the data dir.
	for _, name := range []string{"inventory.ini", "inventory.yml", "playbooks"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("%s was written", name)
		}
	}
	if !slices.Contains(e.logMessagesLike("group", "ansible export"), true) {
		t.Errorf("no group log row for the export: %v", e.logMessages("group"))
	}
}

func (e *win) logMessagesLike(kind, sub string) (out []bool) {
	for _, m := range e.logMessages(kind) {
		out = append(out, strings.Contains(m, sub))
	}
	return out
}
