package native

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The Group page on the fake backend: its hosts and roles, notes, variables, the list of runbooks and the Ansible drawer, and
// what each action sends. Every value is made up; a secret-looking one is refused by the fake as keel serve refuses it.

func groupRig(t *testing.T) *rig {
	t.Helper()
	r := folderRig(t)
	r.tt.SetSize(1280, 1800) // the page is long: every control is on screen
	r.fb.mem.runbooks = []keelclient.Runbook{
		{ID: "rb1", Name: "Restart workers", Steps: []keelclient.RunbookStep{{ID: "s1", Title: "Restart", Command: "echo restart", Role: "worker"}, {ID: "s2", Title: "Check", Command: "echo check", Role: "control-plane"}}},
		{ID: "rb2", Name: "Look around", Steps: []keelclient.RunbookStep{{ID: "s1", Title: "Look", Command: "echo look"}}},
		{ID: "rb3", Name: "Empty", Steps: nil},
	}
	return r
}

func (r *rig) openGroup(t *testing.T, folder string) *groupPage {
	t.Helper()
	r.menuOf(t, "folder "+folder)
	r.pick(t, "Roles and runbooks")
	r.tt.Frame()
	p := r.w.groupPageState()
	if p == nil {
		t.Fatal("the folder's Roles and runbooks did not open the Group page")
	}
	return p
}

func TestTheGroupPageIsOpenedByTheFolderMenuAndThePaletteAsOneInstance(t *testing.T) {
	r := groupRig(t)
	p := r.openGroup(t, "Infra")
	if r.w.tab() != r.w.pageTab("group") || p.id != "infra" || r.w.tab().name() != "Infra" {
		t.Fatalf("page %+v, tab %q", p, r.w.tab().name())
	}
	if miss := r.lacks("Infra", "Folder · 2 hosts.", "Run a runbook on this group", "Export to Ansible", "pg-1", "pg-2", "Address", "Agent", "Role", "Tags", groupRoleHelp, "Group variables", groupVarsEmpty); len(miss) > 0 {
		t.Errorf("the page lacks %q: %v", miss, r.tt.Texts())
	}
	// A folder below Infra: its page replaces the first (one instance), named after it.
	r.w.selectTab(0)
	r.tt.Frame()
	r.openGroup(t, "Apps")
	pages := 0
	for _, tb := range r.w.tabs {
		if tb.pg != nil && tb.pg.group != nil {
			pages++
		}
	}
	if pages != 1 || r.w.tab().name() != "Apps" || r.w.groupPageState().id != "apps" {
		t.Fatalf("%d group pages, %q", pages, r.w.tab().name())
	}
	// The palette has "Group <name> · roles and runbooks" for every folder.
	r.openPal(t)
	r.typePal("Group Replicas")
	rows := r.rows()
	if len(rows) == 0 || rows[0].title != "Group Replicas · roles and runbooks" {
		t.Fatalf("palette rows %v", rows)
	}
	r.press(0, ui.KeyEnter)
	if r.w.groupPageState().id != "rep" || r.w.tab().name() != "Replicas" {
		t.Fatalf("the palette opened %+v", r.w.groupPageState())
	}
	if len(r.fb.spawned) != 0 || len(r.fb.conns) != 0 {
		t.Error("opening the page spawned a session")
	}
}

func TestRolesAreAssignedAndClearedOneHostAtATimeAndCounted(t *testing.T) {
	r := groupRig(t)
	p := r.openGroup(t, "Infra")
	if !r.hasText("1 no role") && !r.hasText("2 no role") {
		t.Errorf("no role chip: %v", r.tt.Texts())
	}
	// New role… turns the picker into a field; ↵ saves.
	r.click(t, "Role of pg-1 picker")
	r.pick(t, "New role…")
	if p.roleFor != "h1" {
		t.Fatalf("New role… opened %q", p.roleFor)
	}
	r.fill(t, "New role for pg-1", " Worker ")
	r.press(0, ui.KeyEnter)
	if want := []string{"infra h1 worker"}; !slices.Equal(r.fb.grp.roleReqs, want) {
		t.Fatalf("requests %v, want %v", r.fb.grp.roleReqs, want)
	}
	// The second host takes the role from the menu.
	r.click(t, "Role of pg-2 picker")
	r.pick(t, "worker")
	r.tt.Frame()
	if want := []string{"infra h1 worker", "infra h2 worker"}; !slices.Equal(r.fb.grp.roleReqs, want) {
		t.Fatalf("requests %v, want %v", r.fb.grp.roleReqs, want)
	}
	if _, ok := r.tt.Find("2 worker"); !ok {
		t.Errorf("no 2 worker chip: %v", r.tt.Texts())
	}
	// No role clears it.
	r.click(t, "Role of pg-2 picker")
	r.pick(t, "No role")
	if got := r.fb.grp.roleReqs[len(r.fb.grp.roleReqs)-1]; got != "infra h2 " {
		t.Errorf("clearing sent %q", got)
	}
	if _, ok := r.tt.Find("1 worker"); !ok {
		t.Errorf("no 1 worker chip: %v", r.tt.Texts())
	}
}

func TestARoleOfTwoWordsIsRefusedBeforeARequestAndKeelServesRefusalIsShown(t *testing.T) {
	r := groupRig(t)
	p := r.openGroup(t, "Infra")
	r.click(t, "Role of pg-1 picker")
	r.pick(t, "New role…")
	r.fill(t, "New role for pg-1", "two words")
	r.press(0, ui.KeyEnter)
	if len(r.fb.grp.roleReqs) != 0 || p.err != groupRoleBad || !r.hasText(groupRoleBad) {
		t.Fatalf("requests %v, error %q", r.fb.grp.roleReqs, p.err)
	}
	// An empty field leaves it. keel serve's own refusal is shown as it said it.
	p.roleFor = "h1"
	p.roleText = ""
	r.w.endNewRole(p, true)
	if p.roleFor != "" || len(r.fb.grp.roleReqs) != 0 {
		t.Error("an empty role sent a request or kept the field")
	}
	r.fb.grp.roleErr = &keelclient.StatusError{Code: 400, Msg: "a group has at most 16 roles"}
	r.w.setRole(p, "h1", "extra")
	r.tt.Frame()
	if !r.hasText("a group has at most 16 roles") {
		t.Errorf("no refusal: %v", r.tt.Texts())
	}
}

func TestGroupNotesAreSavedForTheGroupNeverForAHostAndStayOutOfTheUsersNotes(t *testing.T) {
	r := groupRig(t)
	p := r.openGroup(t, "Infra")
	r.fill(t, "Add a note", "drain a worker before restarting containerd")
	r.click(t, "Add")
	if want := []string{"add group:infra:drain a worker before restarting containerd"}; !slices.Equal(r.fb.mem.noteReqs, want) {
		t.Fatalf("requests %v, want %v", r.fb.mem.noteReqs, want)
	}
	id := p.notes.notes[0].ID
	if miss := r.lacks(groupNotesTitle, "drain a worker before restarting containerd"); len(miss) > 0 {
		t.Errorf("the page lacks %q: %v", miss, r.tt.Texts())
	}
	if len(p.notes.notes) != 1 || p.notes.notes[0].GroupID != "infra" || p.notes.notes[0].HostID != "" {
		t.Errorf("notes %+v", p.notes.notes)
	}
	if got := userNotesOf(p.notes.notes); len(got) != 0 {
		t.Errorf("a group note is in the user's own notes: %v", got)
	}
	// Edit and Delete work on it as on any note.
	r.click(t, "Delete note")
	if !slices.Contains(r.fb.mem.noteReqs, "delete "+id) || len(p.notes.notes) != 0 {
		t.Errorf("requests %v, notes %v", r.fb.mem.noteReqs, p.notes.notes)
	}
}

func TestVariablesAreAddedEditedAndRemovedAndASecretLookingValueIsRefused(t *testing.T) {
	r := groupRig(t)
	p := r.openGroup(t, "Infra")
	r.fill(t, "Variable name", " K8S_Version ")
	r.fill(t, "Variable value", "1.31.2")
	r.click(t, "Add variable")
	g, _ := groupByID(r.fb.groups, "infra")
	if len(g.Vars) != 1 || g.Vars[0] != (keelclient.GroupVar{Name: "k8s_version", Value: "1.31.2"}) {
		t.Fatalf("variables %+v", g.Vars)
	}
	if miss := r.lacks("k8s_version", "1.31.2"); len(miss) > 0 || p.varName != "" || p.varValue != "" {
		t.Errorf("the variable is not listed, or the fields were kept: %v %q %q", miss, p.varName, p.varValue)
	}
	// Edit changes the value; Esc leaves it; an empty value is refused with the app's words.
	r.click(t, "Edit k8s_version")
	r.fill(t, "Value of k8s_version", "1.31.3")
	r.press(0, ui.KeyEnter)
	if g, _ = groupByID(r.fb.groups, "infra"); g.Vars[0].Value != "1.31.3" {
		t.Fatalf("variables %+v", g.Vars)
	}
	r.click(t, "Edit k8s_version")
	r.fill(t, "Value of k8s_version", "")
	r.press(0, ui.KeyEnter)
	if p.varsErr != "Remove the variable instead of saving it empty." {
		t.Errorf("error %q", p.varsErr)
	}
	r.press(0, ui.KeyEscape)
	if p.varEdit != "" {
		t.Error("Esc kept the field")
	}
	// A name that is no name, and a value of two lines, send nothing.
	n := len(r.fb.grp.varReqs)
	r.fill(t, "Variable name", "9x")
	r.fill(t, "Variable value", "a")
	r.click(t, "Add variable")
	if p.varsErr != varNameHelp || len(r.fb.grp.varReqs) != n {
		t.Errorf("a bad name: error %q, requests %v", p.varsErr, r.fb.grp.varReqs)
	}
	// A secret-looking value is keel serve's refusal: the words are shown, the value is not kept, listed or exported.
	r.fill(t, "Variable name", "api_token")
	r.fill(t, "Variable value", fakeVarSecret)
	r.click(t, "Add variable")
	if !r.hasText("Keep secrets in the vault") {
		t.Errorf("no refusal: %v", r.tt.Texts())
	}
	g, _ = groupByID(r.fb.groups, "infra")
	if len(g.Vars) != 1 {
		t.Errorf("the refused variable was kept: %+v", g.Vars)
	}
	if len(p.group.Vars) != 1 {
		t.Errorf("the page holds %+v", p.group.Vars)
	}
	// The refused value is in the field the user typed it in and nowhere else on the page, and the error line does not echo it.
	if p.varsErr != "Keep secrets in the vault" || strings.Contains(strings.Join(r.tt.Texts(), "\n"), fakeVarSecret) && p.varValue != fakeVarSecret {
		t.Errorf("error %q", p.varsErr)
	}
	for _, v := range p.group.Vars {
		if strings.Contains(v.Value, fakeVarSecret) {
			t.Error("the refused value is in the page's variables")
		}
	}
	// Remove removes it.
	r.click(t, "Remove k8s_version")
	if g, _ = groupByID(r.fb.groups, "infra"); len(g.Vars) != 0 || !r.hasText(groupVarsEmpty) {
		t.Errorf("variables %+v", g.Vars)
	}
}

func TestTheRunbookListOffersRunbooksWithStepsAndTheirRoles(t *testing.T) {
	r := groupRig(t)
	r.openGroup(t, "Infra")
	r.click(t, "Run a runbook on this group")
	for _, s := range []string{"Run on Infra", "Restart workers", "2 steps · Roles worker, control-plane", "Look around", "1 step · Every host"} {
		if !r.hasText(s) {
			t.Errorf("no %q: %v", s, r.tt.Texts())
		}
	}
	if r.hasText("Empty") {
		t.Error("a runbook with no steps is offered")
	}
	// The folder menu's Run a runbook… opens the page with the list open.
	r.w.closeTabs([]*tab{r.w.pageTab("group")})
	r.menuOf(t, "folder Apps")
	r.pick(t, "Run a runbook…")
	r.tt.Frame()
	if p := r.w.groupPageState(); p == nil || !p.run.open || !r.hasText("Run on Apps") {
		t.Errorf("the list is not open: %v", r.tt.Texts())
	}
	r.fb.mem.runbooks = nil
	r.w.loadGroupRunbooks(r.w.groupPageState())
	r.tt.Frame()
	if !r.hasText(groupNoBooks) {
		t.Errorf("no empty line: %v", r.tt.Texts())
	}
}

func TestTheAnsibleDrawerShowsWhatKeelServeBuiltAndCopiesIt(t *testing.T) {
	r := groupRig(t)
	r.fb.grp.files = []keelclient.ExportFile{
		{Name: "inventory.ini", Kind: "inventory", Lines: 3, Content: "[infra]\npg-1\n# old_pw: left out: its value is also in Keel's vault\n"},
		{Name: "playbooks/restart.yml", Kind: "playbook", Lines: 2, Content: "- hosts: infra\n"},
	}
	p := r.openGroup(t, "Infra")
	r.click(t, "Export to Ansible")
	if !p.ex.open || len(r.fb.grp.ansibleQs) == 0 {
		t.Fatal("the drawer did not ask keel serve for the export")
	}
	if q := r.fb.grp.ansibleQs[len(r.fb.grp.ansibleQs)-1]; q.Format != "ini" || q.RunbookID != "" || !q.User || q.Jump {
		t.Errorf("first query %+v", q)
	}
	for _, s := range []string{groupExportHelp, "inventory.ini", "playbooks/restart.yml", "3 lines · INI · from this folder's roles and variables", "# old_pw: left out: its value is also in Keel's vault", "Try it first: ansible-playbook -i inventory.ini playbooks/restart.yml --check", groupExportSafe} {
		if !r.hasText(s) {
			t.Errorf("the drawer lacks %q: %v", s, r.tt.Texts())
		}
	}
	// YAML, a playbook, parallel, no user, with the jump host: each is a request.
	r.click(t, "YAML")
	r.click(t, "Playbook from a runbook picker")
	r.pick(t, "Playbook: Restart workers")
	r.click(t, "Parallel")
	r.click(t, "Use ansible_user from each host's saved user")
	r.click(t, "Include the jump host as ansible_ssh_common_args")
	last := r.fb.grp.ansibleQs[len(r.fb.grp.ansibleQs)-1]
	if want := (keelclient.AnsibleQuery{Format: "yaml", RunbookID: "rb1", Mode: "parallel", User: false, Jump: true}); last != want {
		t.Errorf("last query %+v, want %+v", last, want)
	}
	if p.ex.file != 1 {
		t.Errorf("the playbook's tab is not shown: %d", p.ex.file)
	}
	// Copy puts the shown file on the clipboard, and nothing is written anywhere else.
	r.click(t, "Copy")
	if got := r.tt.Clipboard(); got != r.fb.grp.files[1].Content {
		t.Errorf("clipboard %q", got)
	}
	r.click(t, "Close notification") // the toast sits over the button until it goes
	r.click(t, "file inventory.ini")
	r.click(t, "Copy")
	if got := r.tt.Clipboard(); got != r.fb.grp.files[0].Content {
		t.Errorf("clipboard %q", got)
	}
	// A role changed while the drawer is open asks again.
	n := len(r.fb.grp.ansibleQs)
	r.w.setRole(p, "h1", "worker")
	if len(r.fb.grp.ansibleQs) != n+1 {
		t.Errorf("a role change did not refresh the export: %d queries", len(r.fb.grp.ansibleQs))
	}
	r.click(t, "Close export")
	if p.ex.open || r.hasText(groupExportHelp) {
		t.Error("the drawer stays")
	}
}

func TestTheGroupPageComesBackWithTheWorkspace(t *testing.T) {
	r := groupRig(t)
	p := r.openGroup(t, "Infra")
	d := r.w.snapshot()
	if d.GroupID != "infra" || !slices.Contains(d.Pages, "group") || d.ActivePage != "group" {
		t.Fatalf("snapshot %+v", d)
	}
	b2 := okBackend()
	b2.groups, b2.hosts = r.fb.groups, r.fb.hosts
	w2 := newRig(t, b2)
	w2.w.folders = b2.groups
	w2.w.restore(&d, nil)
	w2.tt.Frame()
	q := w2.w.groupPageState()
	if q == nil || q.id != p.id || q.title != "Infra" || w2.w.tab() != w2.w.pageTab("group") || !w2.hasText("pg-1") {
		t.Fatalf("the page did not come back: %+v", q)
	}
}

func TestAGroupPageForAFolderThatIsGoneSaysSo(t *testing.T) {
	r := groupRig(t)
	p := r.openGroup(t, "Infra")
	r.fb.groups = slices.DeleteFunc(r.fb.groups, func(g keelclient.Group) bool { return g.ID == "infra" })
	r.w.loadGroupPage()
	r.tt.Frame()
	if p.err == "" {
		t.Error("no error for a missing folder")
	}
}

// ---- the Runbooks editor's group fields ----

func TestTheRunbookEditorEditsARoleOneHostOnlyAndCaptures(t *testing.T) {
	r := rbRig(t)
	r.w.newRunbook()
	r.tt.Frame()
	r.fill(t, "Runbook name", "Join workers")
	r.fill(t, "Step 1 title", "Get the token")
	r.fill(t, "Step 1 command", "kubeadm token create --print-join-command")
	r.fill(t, "Step 1 role", " Control-Plane ")
	r.click(t, "Step 1 on one host only")
	r.click(t, "Capture a value")
	r.fill(t, "Step 1 capture 1 name", " Join_Token ")
	r.fill(t, "Step 1 capture 1 pattern", `--token\s+(\S+)`)
	r.click(t, "Add step")
	r.fill(t, "Step 2 title", "Join")
	r.fill(t, "Step 2 command", "kubeadm join --token {{join_token}}")
	r.fill(t, "Step 2 role", "worker")
	r.click(t, "Save")
	if len(r.fb.mem.rbSaved) != 1 {
		t.Fatalf("%d saves", len(r.fb.mem.rbSaved))
	}
	in := r.fb.mem.rbSaved[0]
	s1 := in.Steps[0]
	if s1.Role != "control-plane" || !s1.OneHost || !slices.Equal(s1.Captures, []keelclient.StepCapture{{Name: "join_token", Pattern: `--token\s+(\S+)`}}) {
		t.Errorf("step 1 %+v", s1)
	}
	if in.Steps[1].Role != "worker" || in.Steps[1].OneHost || len(in.Steps[1].Captures) != 0 || in.Steps[1].Command != "kubeadm join --token {{join_token}}" {
		t.Errorf("step 2 %+v", in.Steps[1])
	}
	// Remove capture takes it out of the next save; a name taken by another step is refused with the app's words and sends nothing.
	r.click(t, "Remove capture join_token")
	if got := r.w.runbooksPage().ed.steps[0].captures; len(got) != 0 {
		t.Fatalf("Remove capture left %v", got)
	}
	r.w.runbooksPage().ed.steps[1].captures = []keelclient.StepCapture{{Name: "x", Pattern: "(a)"}}
	r.tt.Frame()
	n := len(r.fb.mem.rbSaved)
	r.click(t, "Capture a value") // step 1's: the first button
	r.fill(t, "Step 1 capture 1 name", "x")
	r.fill(t, "Step 1 capture 1 pattern", "(b)")
	r.click(t, "Save")
	if len(r.fb.mem.rbSaved) != n || !r.hasText("Step 2: x is captured by another step already.") && !r.hasText("Step 1: x is captured by another step already.") {
		t.Errorf("saves %d, texts %v", len(r.fb.mem.rbSaved), r.tt.Texts())
	}
	if strings.Contains(strings.Join(r.tt.Texts(), "\n"), "internal") {
		t.Error("an internal word on screen")
	}
}
