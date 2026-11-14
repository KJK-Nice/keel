package native

import (
	"context"
	"errors"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The Group page (docs/ui-redesign.md §17, board GroupRoles): one
// folder's hosts with their roles, its notes for the agent, its variables, "Run a runbook on this group" and the Ansible
// export drawer. A tab page, one instance, named after the folder; the folder it shows is kept in the saved workspace
// (saved.go). Roles and variables are saved one at a time by keel serve, which checks them; this client holds what keel
// serve answered in memory and writes nothing to disk. Nothing on this page reads a pane, calls the agent's provider or
// types: a run started from here is a group run (grouprun.go), whose every host × step is a proposal the user approves.
//
// Variables are config, not secrets (keel serve refuses a secret-looking value or one that holds a vault value): they are
// shown, filled into commands by keel serve alone and exported. The export is whatever keel serve built, and a variable it
// withheld is not in it.

// downloadFile is the export drawer's Download: the system's save dialog (MyGo's Dialog.Save, set by Run as w.saveDialog)
// names a path, and the shown file's text is written there, off the UI thread. A cancelled dialog writes nothing and says
// nothing. The text is what keel serve built (no secret is in it), and nothing here runs it.
func (w *workspace) downloadFile(name, content string) {
	base := path.Base(name)
	if w.saveDialog == nil {
		w.warn("Could not save " + base + ": this window has no save dialog.")
		return
	}
	w.work(func() {
		dest, err := w.saveDialog(base)
		if err == nil && dest != "" {
			err = os.WriteFile(dest, []byte(content), 0o644)
		}
		w.post(func() {
			switch {
			case err != nil:
				w.warn("Could not save " + base + ": " + err.Error())
			case dest != "":
				w.say("Saved " + base + ".")
			}
		})
	})
}

const (
	groupRoleHelp   = "Roles tell runbooks and the agent what each host is. A step for a role runs on every host with it."
	groupNoHosts    = "No hosts in this folder yet. Drag hosts onto it in the sidebar."
	groupRoleBad    = "A role is one word of letters, digits, -, _ or . (for example control-plane)."
	groupLockedText = "Unlock the vault to see this group."
	groupNewRole    = "__new__" // the picker's "New role…"
	groupNotesTitle = "Notes for the agent · whole group"
	groupNotesHint  = "Add a note for every host in this group"
	groupNotesHelp  = "Sent with a question only when a pane on a host in this group is read and its Agent can read is on. Redacted like pane text. Stored encrypted in the vault."
	groupVarsHelp   = "Runbook steps use these as {{name}}, filled in only when you press Type in a group run. Not secret: they are shown, typed and exported. Keep secrets in the vault."
	groupVarsEmpty  = "No variables yet. Add one a runbook step can use, such as k8s_version."
	groupNoBooks    = "No runbooks yet. Add one on the Runbooks page."
	groupExportHelp = "Built from this folder's hosts, roles and group variables. Change a role or a variable and this updates."
	groupExportSafe = "Keel does not run Ansible. Save the files and run ansible-playbook yourself. No passwords, keys or tokens are written into them; a host whose key or password is in the vault gets a comment instead."
)

// groupPage is the state of the Group tab.
type groupPage struct {
	id      string // the folder shown
	title   string // its name once read: the tab's name
	group   keelclient.Group
	members []keelclient.GroupMember
	loaded  bool
	err     string
	gen     int // a read that answers after another began is ignored

	roleFor  string // the member whose "New role…" field is open
	roleText string
	roleH    ui.Handle
	roleSeen bool

	notes notesEditor

	varName, varValue   string
	varNameH, varValueH ui.Handle
	varNameFocus        bool
	varEdit, varEditTxt string // the variable being edited, and its text
	varEditH            ui.Handle
	varFocusEdit        bool
	varsErr             string
	varsBusy            bool

	run groupRunMenu
	ex  ansibleDrawer
}

// groupRunMenu is "Run a runbook on this group" and its list of runbooks.
type groupRunMenu struct {
	open    bool
	books   []keelclient.Runbook // runbooks with steps
	loading bool
	err     string
	busy    bool
}

// ansibleDrawer is the Export to Ansible drawer (board AnsibleExport).
type ansibleDrawer struct {
	open    bool
	format  string // ini | yaml
	runbook string // "" for the inventory only
	mode    string // rolling | parallel
	user    bool
	jump    bool
	files   []keelclient.ExportFile
	file    int
	gen     int
	err     string
	focus   bool
}

func newGroupPage() *groupPage {
	return &groupPage{ex: ansibleDrawer{format: "ini", mode: "rolling", user: true}}
}

func (p *groupPage) load(w *workspace)                { w.loadGroupPage() }
func (p *groupPage) view(w *workspace, c *ui.Context) { w.groupPageView(c, p) }
func (p *groupPage) tabTitle() string                 { return p.title }
func (p *groupPage) snapshot(d *savedDoc)             { d.GroupID = p.id }

func (p *groupPage) restore(w *workspace, d *savedDoc) {
	p.id = d.GroupID
	p.notes = notesEditor{group: true, groupID: d.GroupID}
	if g, ok := groupByID(w.folders, d.GroupID); ok {
		p.title = g.Name
	}
}

// groupPageState is the page of the Group tab, or nil.
func (w *workspace) groupPageState() *groupPage { return pageAs[*groupPage](w.pageTab("group")) }

// openGroupPage opens the folder's page (the folder menu's Roles and runbooks, the palette's Group …), or shows it in the
// tab that is open: one instance. runMenu also opens its "Run a runbook on this group" list.
func (w *workspace) openGroupPage(id string, runMenu bool) {
	if _, ok := groupByID(w.folders, id); !ok {
		w.warn("Group not found")
		return
	}
	t := w.pageTab("group")
	if t == nil {
		t = w.addPageTab("group")
	}
	p := pageAs[*groupPage](t)
	if p.id != id {
		*p = *newGroupPage()
		p.id = id
		if g, ok := groupByID(w.folders, id); ok {
			p.title = g.Name
		}
	}
	p.notes = notesEditor{group: true, groupID: id}
	p.run.open = runMenu
	w.selectTab(slices.Index(w.tabs, t)) // reads the page
	if runMenu {
		w.loadGroupRunbooks(p)
	}
}

// loadGroupPage reads the folder's members (and, with them, its roles and variables), its notes and the runbooks.
func (w *workspace) loadGroupPage() {
	p := w.groupPageState()
	if p == nil || p.id == "" {
		return
	}
	p.gen++
	gen, id := p.gen, p.id
	w.loadNotes(&p.notes)
	w.loadGroupRunbooks(p)
	callIf(w, func() bool { return w.groupPageState() == p && p.gen == gen }, func(c context.Context) (keelclient.GroupInfo, error) { return w.be.GroupMembers(c, id) },
		func(info keelclient.GroupInfo, err error) {
			if err != nil {
				var se *keelclient.StatusError
				if errors.As(err, &se) && se.Code == 423 {
					p.err = groupLockedText
				} else {
					p.err = hostError(err)
				}
				return
			}
			p.group, p.members, p.loaded, p.err = info.Group, info.Members, true, ""
			if info.Group.Name != "" {
				p.title = info.Group.Name
			}
			w.exportRefresh(p)
		})
}

// loadGroupRunbooks reads the runbooks that have steps: the list of "Run a runbook on this group" and of the export's playbook.
func (w *workspace) loadGroupRunbooks(p *groupPage) {
	p.run.loading = true
	callIf(w, func() bool { return w.groupPageState() == p }, w.be.Runbooks, func(l keelclient.RunbookList, err error) {
		p.run.loading = false
		if err != nil { // runbooks need the vault; the inventory alone still works
			p.run.books, p.run.err = nil, hostError(err)
			return
		}
		p.run.err = ""
		p.run.books = p.run.books[:0]
		for _, b := range l.Runbooks {
			if len(b.Steps) > 0 {
				p.run.books = append(p.run.books, b)
			}
		}
	})
}

// ---- roles ----

// setRole saves one host's role (PUT /api/groups/{id}/roles). keel serve checks it again.
func (w *workspace) setRole(p *groupPage, hostID, role string) {
	r, ok := cleanRole(role)
	if !ok {
		p.err = groupRoleBad
		return
	}
	id := p.id
	callErrIf(w, func() bool { return w.groupPageState() == p && p.id == id }, func(c context.Context) error { _, err := w.be.SetGroupRole(c, id, hostID, r); return err },
		func(err error) {
			p.roleFor = ""
			if err != nil {
				p.err = hostError(err)
				return
			}
			for i := range p.members {
				if p.members[i].HostID == hostID {
					p.members[i].Role = r
				}
			}
			p.err = ""
			w.exportRefresh(p)
		})
}

// pickRole is the role picker's choice: "New role…" opens the field, anything else is saved at once.
func (w *workspace) pickRole(p *groupPage, hostID, v string) {
	if v == groupNewRole {
		p.roleFor, p.roleText, p.roleSeen = hostID, "", false
		return
	}
	w.setRole(p, hostID, v)
}

// endNewRole keeps the role typed (ok, and not empty) or leaves the field.
func (w *workspace) endNewRole(p *groupPage, ok bool) {
	host, text := p.roleFor, p.roleText
	if !ok || strings.TrimSpace(text) == "" {
		p.roleFor = ""
		return
	}
	w.setRole(p, host, text)
}

// ---- variables ----

// setVar saves a variable (PUT /api/groups/{id}/vars); an empty value removes it. keel serve refuses a value that looks like a
// secret or holds a vault value, and its words are shown.
func (w *workspace) setVar(p *groupPage, name, value string, onDone func()) {
	if p.varsBusy {
		return
	}
	n, v, bad := cleanVar(name, value)
	if bad != "" {
		p.varsErr = bad
		return
	}
	p.varsBusy = true
	id := p.id
	callIf(w, func() bool { return w.groupPageState() == p && p.id == id }, func(c context.Context) (keelclient.Group, error) { return w.be.SetGroupVar(c, id, n, v) },
		func(g keelclient.Group, err error) {
			p.varsBusy = false
			if err != nil {
				p.varsErr = hostError(err)
				return
			}
			p.group.Vars, p.varsErr, p.varEdit = g.Vars, "", ""
			if onDone != nil {
				onDone()
			}
			w.exportRefresh(p)
		})
}

func (w *workspace) addVar(p *groupPage) {
	w.setVar(p, p.varName, p.varValue, func() { p.varName, p.varValue, p.varNameFocus = "", "", true })
}

func (w *workspace) startEditVar(p *groupPage, v keelclient.GroupVar) {
	p.varEdit, p.varEditTxt, p.varFocusEdit, p.varsErr = v.Name, v.Value, true, ""
}

// saveEditVar is ↵ on an edited value: an empty one is refused (Remove is for that).
func (w *workspace) saveEditVar(p *groupPage) {
	if strings.TrimSpace(p.varEditTxt) == "" {
		p.varsErr = "Remove the variable instead of saving it empty."
		return
	}
	w.setVar(p, p.varEdit, p.varEditTxt, nil)
}

// ---- run a runbook on this group ----

// toggleRunMenu opens or closes the list of runbooks.
func (w *workspace) toggleRunMenu(p *groupPage) {
	p.run.open = !p.run.open
	if p.run.open {
		w.loadGroupRunbooks(p)
	}
}

// ---- Export to Ansible ----

func (w *workspace) toggleExport(p *groupPage) {
	p.ex.open = !p.ex.open
	p.ex.focus = p.ex.open
	if p.ex.open {
		w.loadGroupRunbooks(p)
		w.exportRefresh(p)
	}
}

// exportRefresh asks keel serve for the files again (open drawer only): a role or a variable changed, or an option did.
func (w *workspace) exportRefresh(p *groupPage) {
	if !p.ex.open || p.id == "" {
		return
	}
	p.ex.gen++
	gen, id := p.ex.gen, p.id
	q := keelclient.AnsibleQuery{Format: p.ex.format, RunbookID: p.ex.runbook, Mode: p.ex.mode, User: p.ex.user, Jump: p.ex.jump}
	callIf(w, func() bool { return w.groupPageState() == p && p.ex.gen == gen }, func(c context.Context) ([]keelclient.ExportFile, error) { return w.be.GroupAnsible(c, id, q) },
		func(files []keelclient.ExportFile, err error) {
			if err != nil {
				p.ex.files, p.ex.err = nil, hostError(err)
				return
			}
			p.ex.files, p.ex.err = files, ""
			if p.ex.file >= len(files) {
				p.ex.file = 0
			}
		})
}
