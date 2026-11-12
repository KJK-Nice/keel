package native

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
	"keel/internal/native/sshparse"
)

// New host and Edit host (docs/ui-redesign.md §5.8): one dialog, laid out as hostDialog in
// internal/ui/dialogs.templ, with the rules of term.js (fillHostForm, hostDraft, forgetHostKey, the host-delete
// handler) and ui/hosttest.js + lib/hosttest.js for Test connection. The webview posts a form to
// /ui/forms/host (httpapi/forms.go hostFromForm); the native client sends the same values as JSON to
// POST /api/hosts or PUT /api/hosts/{id}.
//
// Secrets. The password, the private key and its passphrase are typed into masked fields, go only in the one
// request that needs them (Save or Test connection) and are cleared when the dialog closes. The dialog never
// shows a saved secret: a blank field on an update means "keep the saved one" (store.putSecretLocked). Nothing
// here logs, stores or formats a secret; Test connection's key offer is trusted only by the user's click.

// hostForm is what the fields hold. Text fields are strings as typed.
type hostForm struct {
	name, tags, address, port, username, startup, keepSec string
	groupID, jump, identityID, color, auth                string

	password, privateKey, passphrase string // secrets: masked, write-only

	keepAlive, autoReconnect, agentReadable, production, agentNotes bool
}

// String prints a form without its secrets (so that %v and %+v of a dialog never show them).
func (f hostForm) String() string {
	return fmt.Sprintf("{%s %s@%s:%s %s}", f.name, f.username, f.address, f.port, f.auth)
}

func (f hostForm) GoString() string { return "hostForm" + f.String() }

func (d hostDialog) String() string { return fmt.Sprintf("hostDialog{%s %s}", d.id, d.f) }

func (d hostDialog) GoString() string { return d.String() }

// hostTest is Test connection's state (ui/hosttest.js createHostTest).
type hostTest struct {
	running, cancelled bool
	cancel             context.CancelFunc
	gen                int
	sent               [sha256.Size]byte // a hash of the values the last test sent, never the values
	state              string            // the dot: "live" | "connecting" | "dead" | ""
	short, detail      string
	res                *keelclient.TestResult
}

// hostConfirm is the confirm dialog that replaces the host dialog while it asks (term.js askConfirm: the host
// dialog closes while it asks, and opens again after).
type hostConfirm struct {
	title, message, action string
	run                    func()
	cancelH, actionH       ui.Handle
	focused                bool // the keyboard has been put on Cancel
}

type hostDialog struct {
	id     string           // "" for New host
	saved  *keelclient.Host // the host being edited, as listed
	f      hostForm
	err    string // the server's reason, or a field's (host-form-error)
	notes  *sshparse.Result
	groups []keelclient.Group
	idents []keelclient.Identity
	key    *keelclient.KnownKey // the saved host key (Edit host)
	nb     *notesEditor         // Notes for the agent: this host's (notes.go); empty until the host is saved

	test    hostTest
	confirm *hostConfirm
	saving  bool
	focus   bool

	nameH, tagsH, addrH, portH, userH, startH, keepH, passH, keyH, phraseH ui.Handle
}

const (
	hostDialogWidth = 760 // .modal--host
	hostColsMax     = 620 // .k-host-cols max-height
	defaultColor    = "var(--ansi-10)"
	maxStartup      = 1024 // store.MaxStartupCommand
	maxPort         = 65535
	maxKeepAlive    = 600
	defaultKeep     = 30
)

// hostColors is store.HostColors, in picker order.
var hostColors = []struct{ name, value string }{
	{"Green", "var(--ansi-10)"}, {"Blue", "var(--ansi-12)"}, {"Magenta", "var(--ansi-13)"},
	{"Yellow", "var(--ansi-11)"}, {"Red", "var(--ansi-9)"}, {"Cyan", "var(--ansi-14)"},
}

// authKinds is the Authentication control's options, in the webview's order.
var authKinds = []struct{ value, label string }{
	{"password", "Password"}, {"key", "Private key"}, {"identity", "Identity"}, {"local", "Local shell"},
}

// isProductionGroup: the group's own name is production, any case (term.js isProductionGroup).
func isProductionGroup(groups []keelclient.Group, id string) bool {
	for _, g := range groups {
		if g.ID == id {
			return strings.EqualFold(strings.TrimSpace(g.Name), "production")
		}
	}
	return false
}

// newHostForm is term.js fillHostForm: the defaults of a new host, or the values of a saved one.
func newHostForm(h *keelclient.Host) hostForm {
	f := hostForm{port: "22", color: defaultColor, auth: "password", keepAlive: true, keepSec: strconv.Itoa(defaultKeep),
		autoReconnect: true, agentReadable: true, agentNotes: true}
	if h == nil {
		return f
	}
	f.name, f.groupID, f.tags = h.Name, h.GroupID, strings.Join(h.Tags, ", ")
	f.address, f.username, f.startup, f.jump, f.identityID = h.Address, h.Username, h.StartupCommand, h.JumpHost, h.IdentityID
	f.port = "22"
	if h.Port != 0 {
		f.port = strconv.Itoa(h.Port)
	}
	if h.AuthType != "" {
		f.auth = h.AuthType
	}
	if h.Color != "" {
		f.color = h.Color
	}
	f.keepAlive = h.KeepAliveSec != -1
	if h.KeepAliveSec > 0 {
		f.keepSec = strconv.Itoa(h.KeepAliveSec)
	}
	f.autoReconnect = !h.NoAutoReconnect
	f.agentReadable = h.AgentReadable == nil || *h.AgentReadable
	f.production = h.Production != nil && *h.Production
	f.agentNotes = h.AgentNotes == nil || *h.AgentNotes
	return f
}

// hostPrefill is lib/newtab.js hostPrefill: the fields a pasted command fills. The name is the host; the user
// edits it. Auth stays the form's default: nothing about keys or passwords is carried over.
func hostPrefill(r *sshparse.Result) keelclient.Host {
	return keelclient.Host{Name: r.Host, Address: r.Host, Port: r.Port, Username: r.User}
}

// ---- opening and closing ----

// newHost opens New host.
func (w *workspace) newHost() { w.openHostDialog(nil, nil, "") }

// saveAsHost is the New tab page's "Save as host": New host filled from a pasted command, with its notes.
func (w *workspace) saveAsHost(r *sshparse.Result) {
	h := hostPrefill(r)
	w.openHostDialog(&h, r, "")
}

// editHost opens Edit host for a saved host.
func (w *workspace) editHost(h keelclient.Host) { w.openHostDialog(&h, nil, h.ID) }

// editPaneHost is the failure banner's Edit host (term.js editPaneHost): the pane's host, as saved now.
func (w *workspace) editPaneHost(p *pane) {
	for _, h := range w.hosts {
		if h.ID == p.host.ID {
			w.editHost(h)
			return
		}
	}
	w.warn("Host not found")
}

// openHostDialog opens the dialog. id is set for a saved host; h with no id only prefills New host.
func (w *workspace) openHostDialog(h *keelclient.Host, notes *sshparse.Result, id string) {
	w.closeHostDialog()
	d := &hostDialog{id: id, f: newHostForm(h), notes: notes, focus: true, nb: &notesEditor{hostID: id}}
	if id != "" {
		d.saved = h
	}
	w.hostDlg = d
	w.loadNotes(d.nb)
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		groups, _ := w.be.Groups(c) // a failed list leaves the picker with "No group", as the webview's catch does
		idents, _ := w.be.Identities(c)
		var key *keelclient.KnownKey
		if id != "" && h.AuthType != "local" {
			if k, err := w.be.HostKey(c, id); err == nil {
				key = &k
			}
		}
		w.post(func() {
			if w.hostDlg != d {
				return
			}
			d.groups, d.idents, d.key = groups, idents, key
			d.defaultIdentity()
		})
	})
}

// closeHostDialog closes without a request. A test under way is cancelled and the secrets are cleared.
func (w *workspace) closeHostDialog() {
	d := w.hostDlg
	if d == nil {
		return
	}
	d.stopTest()
	d.f.password, d.f.privateKey, d.f.passphrase = "", "", ""
	w.hostDlg = nil
	if t := w.tab(); t != nil {
		if t.panes[t.focused] != nil {
			w.wantFocus = t.focused
		} else if t.page != nil {
			t.page.focus = true
		}
	}
}

// defaultIdentity picks the first identity for a new Identity host, as fillIdentitySelect does ({first: !saved}):
// a saved Identity host with none must not be shown, and saved, with the first one instead.
func (d *hostDialog) defaultIdentity() {
	if d.f.auth != "identity" || d.f.identityID != "" || len(d.idents) == 0 {
		return
	}
	if d.saved != nil && d.saved.AuthType == "identity" {
		return
	}
	d.f.identityID = d.idents[0].ID
}

// ---- values ----

func atoiClamp(s string, def, lo, hi int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || (lo > 0 && n <= 0) {
		return def
	}
	return min(max(n, lo), hi)
}

// detail is hostFromForm: the host the form says.
func (f hostForm) detail(id string) keelclient.HostDetail {
	local := f.auth == "local"
	h := keelclient.HostDetail{
		ID: id, Name: strings.TrimSpace(f.name), GroupID: f.groupID, Tags: parseTags(f.tags),
		Address: strings.TrimSpace(f.address), Port: atoiClamp(f.port, 22, 0, maxPort), Username: strings.TrimSpace(f.username),
		AuthType: f.auth, Color: f.color, JumpHost: f.jump, StartupCommand: strings.TrimSpace(f.startup),
		AgentReadable: boolp(f.agentReadable), Production: boolp(f.production), AgentNotes: boolp(f.agentNotes),
	}
	if h.Port == 0 {
		h.Port = 22
	}
	switch f.auth {
	case "identity":
		h.IdentityID = f.identityID
	case "password":
		h.Password = f.password
	case "key":
		h.PrivateKey, h.Passphrase = f.privateKey, f.passphrase
	}
	switch {
	case local:
		h.KeepAliveSec, h.JumpHost, h.StartupCommand = 0, "", ""
	case f.keepAlive:
		h.KeepAliveSec = atoiClamp(f.keepSec, defaultKeep, 1, maxKeepAlive)
	default:
		h.KeepAliveSec = -1
	}
	h.NoAutoReconnect = !local && !f.autoReconnect
	return h
}

// testDetail is hostDraft: the values Test connection sends and an edit is compared with.
func (f hostForm) testDetail(id string) keelclient.HostDetail {
	h := f.detail(id)
	h.GroupID, h.Tags, h.Color, h.StartupCommand, h.NoAutoReconnect = "", nil, "", "", false
	h.AgentReadable, h.Production, h.AgentNotes = nil, nil, nil
	return h
}

func boolp(b bool) *bool { return &b }

// parseTags is lib/groups.js parseHostTags.
func parseTags(s string) []string {
	var out []string
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// validate checks the form with the server's own words (httpapi validateHost, store ValidateStartupCommand), so
// that a field's problem shows without a request. "" is fine.
func (f hostForm) validate() string {
	if strings.TrimSpace(f.name) == "" {
		return "name is required"
	}
	if f.auth != "local" {
		if strings.TrimSpace(f.address) == "" {
			return "address is required"
		}
		if strings.TrimSpace(f.username) == "" {
			return "username is required"
		}
	}
	if f.auth == "identity" && strings.TrimSpace(f.identityID) == "" {
		return "identity is required"
	}
	return validStartup(strings.TrimSpace(f.startup))
}

// validStartup is store.ValidateStartupCommand: one line of printable text, at most 1024 bytes.
func validStartup(cmd string) string {
	switch {
	case cmd == "":
		return ""
	case len(cmd) > maxStartup:
		return fmt.Sprintf("startup command is longer than %d bytes", maxStartup)
	case !utf8.ValidString(cmd):
		return "startup command is not valid text"
	}
	for _, r := range cmd {
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return "startup command must be one line with no control characters"
		}
	}
	return ""
}

// ---- Save, Delete ----

// saveHost is Save host and ⌘↵: it creates or updates the host and closes. A refusal stays in the dialog as
// the line under the fields.
func (w *workspace) saveHost() {
	d := w.hostDlg
	if d == nil || d.saving || d.confirm != nil {
		return
	}
	if msg := d.f.validate(); msg != "" {
		d.err = msg
		return
	}
	h := d.f.detail(d.id)
	d.saving, d.err = true, ""
	d.f.password, d.f.privateKey, d.f.passphrase = "", "", "" // they are in h now, and h only goes in this request
	d.stopTest()
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		saved, err := w.be.SaveHost(c, h)
		h = keelclient.HostDetail{}
		w.post(func() {
			d.saving = false
			if err != nil {
				d.err = hostError(err)
				return
			}
			if w.hostDlg == d {
				w.closeHostDialog()
			}
			w.say("Host saved")
			w.afterHostWrite(saved)
		})
	})
}

// hostError is the line the dialog shows for a refused request: the server's own reason, else the status.
func hostError(err error) string {
	var se *keelclient.StatusError
	switch {
	case errors.As(err, &se) && se.Code == 423: // store.ErrLocked
		return "The vault is locked."
	case errors.As(err, &se) && se.Code == 404:
		return "It was deleted elsewhere; close this dialog."
	case errors.As(err, &se) && se.Msg != "":
		return se.Msg
	}
	return err.Error()
}

// afterHostWrite takes a saved host into the list at once, then reads the list again (it is the server's).
func (w *workspace) afterHostWrite(saved keelclient.Host) {
	if saved.ID != "" {
		replaced := false
		for i := range w.hosts {
			if w.hosts[i].ID == saved.ID {
				w.hosts[i], replaced = saved, true
			}
		}
		if !replaced {
			w.hosts = append(w.hosts, saved)
		}
		w.setHosts(w.hosts)
	}
	w.reloadHosts()
}

// askDeleteHost is the Delete host button and the host menu's Delete: it asks first (term.js: askConfirm
// `Remove ${host.name}?`). The local shell cannot be removed.
func (w *workspace) askDeleteHost(h keelclient.Host) {
	if h.AuthType == "local" {
		w.warn("Local Shell is part of the workspace and cannot be removed")
		return
	}
	run := func() { w.deleteHost(h) }
	if d := w.hostDlg; d != nil && d.id == h.ID {
		d.confirm = &hostConfirm{title: "Confirm", message: "Remove " + h.Name + "?", action: "Remove", run: run}
		return
	}
	w.askConfirm(&hostConfirm{title: "Confirm", message: "Remove " + h.Name + "?", action: "Remove", run: run})
}

// deleteHost deletes the host. Its open sessions are left as they are, as the webview leaves them: they keep
// running, and a pane keeps the host it had (setHosts).
func (w *workspace) deleteHost(h keelclient.Host) {
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		err := w.be.DeleteHost(c, h.ID)
		w.post(func() {
			if err != nil {
				w.fail("delete host", err)
				return
			}
			if d := w.hostDlg; d != nil && d.id == h.ID {
				w.closeHostDialog()
			}
			w.hosts = slices.DeleteFunc(slices.Clone(w.hosts), func(o keelclient.Host) bool { return o.ID == h.ID })
			w.reloadHosts()
		})
	})
}

// duplicateHost is the host menu's Duplicate: the server copies the host as "name (copy)".
func (w *workspace) duplicateHost(h keelclient.Host) {
	if h.AuthType == "local" {
		w.say("Local Shell cannot be duplicated") // toast(…) without the error flag, as term.js
		return
	}
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		dup, err := w.be.DuplicateHost(c, h.ID)
		w.post(func() {
			if err != nil {
				w.fail("duplicate host", err)
				return
			}
			name := dup.Name
			if name == "" {
				name = h.Name + " (copy)"
			}
			w.say("Duplicated as " + name)
			w.reloadHosts()
		})
	})
}

// setAgentReadable is the host menu's "Agent can read".
func (w *workspace) setAgentReadable(h keelclient.Host, on bool) {
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		err := w.be.SetAgentReadable(c, h.ID, on)
		w.post(func() {
			if err != nil {
				w.fail("agent can read", err)
				return
			}
			w.reloadHosts()
		})
	})
}

// ---- the confirm dialog ----

// askConfirm shows a confirm dialog on its own (no host dialog under it).
func (w *workspace) askConfirm(c *hostConfirm) { w.confirm = c }

// ---- the host key line ----

// askForgetKey is Forget host key: it asks first, with the webview's words.
func (w *workspace) askForgetKey() {
	d := w.hostDlg
	if d == nil || d.key == nil || !d.key.Known || d.saved == nil {
		return
	}
	name, host := d.saved.Name, d.key.Hostname
	d.confirm = &hostConfirm{
		title:   "Forget host key?",
		message: "Keel forgets the saved key for " + name + " (" + host + "). The next connect shows the server's key and asks you to trust it again.",
		action:  "Forget key",
		run:     func() { w.forgetKey(d) },
	}
}

func (w *workspace) forgetKey(d *hostDialog) {
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		err := w.be.ForgetHostKey(c, d.id)
		w.post(func() {
			if err != nil {
				w.fail("forget host key", err)
				return
			}
			if w.hostDlg == d {
				d.key = &keelclient.KnownKey{}
			}
			w.say("Host key forgotten")
		})
	})
}

// loadHostKey reads the saved key again (after a key was trusted).
func (w *workspace) loadHostKey(d *hostDialog) {
	if d.id == "" || d.saved == nil || d.saved.AuthType == "local" {
		return
	}
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		k, err := w.be.HostKey(c, d.id)
		w.post(func() {
			if w.hostDlg == d && err == nil {
				d.key = &k
			}
		})
	})
}

// keyTypeLabel is lib/hosttest.js keyTypeLabel: the short name ssh-keygen -l prints.
func keyTypeLabel(t string) string {
	switch {
	case t == "ssh-ed25519" || strings.HasPrefix(t, "sk-ssh-ed25519"):
		return "ED25519"
	case strings.HasPrefix(t, "ecdsa-") || strings.HasPrefix(t, "sk-ecdsa-"):
		return "ECDSA"
	case t == "ssh-rsa" || strings.HasPrefix(t, "rsa-"):
		return "RSA"
	case t == "ssh-dss":
		return "DSA"
	}
	return strings.ToUpper(t)
}

// ---- Test connection ----

// testText is lib/hosttest.js hostTestText: the dot (state), its label (short) and the explanation (detail).
func testText(res keelclient.TestResult) (state, short, detail string) {
	where := ""
	switch res.Hop {
	case "jump":
		where = "Jump host " + res.HopName + ": "
	case "target":
		where = "Through the jump host: "
	}
	switch res.Kind {
	case "ok":
		return "live", fmt.Sprintf("Connected in %d ms", res.LatencyMs), ""
	case "auth":
		return "dead", "Authentication failed", where + "the server refused the password or key. " + res.Message
	case "unknown-host":
		return "connecting", "Host key unknown", where + res.Hostname + " presented a key Keel has not seen. Compare it with the server's own fingerprint before you trust it."
	case "host-key-mismatch":
		return "dead", "Host key changed", where + res.Hostname + " presented a different key than the one saved. Only replace it if you know the server was reinstalled."
	case "timeout":
		return "dead", "Timed out", where + "no answer within 10 seconds. " + res.Message
	case "dns":
		return "dead", "Host not found", where + "the name does not resolve. " + res.Message
	case "refused":
		return "dead", "Connection refused", where + "nothing accepts SSH on that port. " + res.Message
	case "cancelled":
		return "", "Test cancelled", ""
	}
	return "dead", "Could not connect", where + res.Message
}

// fingerprintText is lib/hosttest.js fingerprintText: the offered key as the dialog shows it, or "".
func fingerprintText(res keelclient.TestResult) string {
	if _, ok := res.HostKeyOffer(); !ok {
		return ""
	}
	t := ""
	if res.KeyType != "" {
		t = keyTypeLabel(res.KeyType) + " "
	}
	t += res.Fingerprint
	if res.Expected != "" {
		t += " (saved: " + res.Expected + ")"
	}
	return t
}

func (d *hostDialog) draftHash() [sha256.Size]byte {
	b, _ := json.Marshal(d.f.testDetail(d.id))
	return sha256.Sum256(b)
}

// stopTest cancels a test under way and drops its result (ui/hosttest.js reset).
func (d *hostDialog) stopTest() {
	if d.test.cancel != nil {
		d.test.cancel()
	}
	d.test = hostTest{gen: d.test.gen + 1}
}

// dropStaleTest drops the result once the values differ from what the test sent, so that Trust and save never
// applies to values the test did not use (ui/hosttest.js edited).
func (d *hostDialog) dropStaleTest() {
	t := &d.test
	if !t.running && t.res == nil && t.short == "" {
		return
	}
	if d.draftHash() != t.sent {
		d.stopTest()
	}
}

// runHostTest is Test connection, or Cancel test while one runs. It runs off the UI thread; the server stops
// it after 10 s, and the button cancels it.
func (w *workspace) runHostTest() {
	d := w.hostDlg
	if d == nil || d.f.auth == "local" {
		return
	}
	if d.test.running {
		d.test.cancelled = true
		d.test.cancel()
		return
	}
	h := d.f.testDetail(d.id)
	c, cancel := context.WithTimeout(context.Background(), callTimeout)
	gen := d.test.gen + 1
	d.test = hostTest{running: true, cancel: cancel, gen: gen, sent: d.draftHash(), state: "connecting", short: "Testing…"}
	w.work(func() {
		res, err := w.be.TestHost(c, h)
		h = keelclient.HostDetail{}
		w.post(func() {
			cancel()
			if w.hostDlg != d || d.test.gen != gen { // the dialog closed or refilled meanwhile: drop the result
				return
			}
			t := &d.test
			t.running, t.cancel = false, nil
			switch {
			case t.cancelled:
				t.state, t.short, t.detail, t.res = "", "Test cancelled", "", nil
			case err != nil:
				t.state, t.short, t.res = "dead", "Could not test", nil
				t.detail = hostError(err)
			default:
				t.res = &res
				t.state, t.short, t.detail = testText(res)
			}
		})
	})
}

// trustTest is the result's trust button: Trust and save, or Replace saved key (which asks first). It sends back
// the server's own offer and nothing else, then tests again. Only the user's click gets here.
func (w *workspace) trustTest() {
	d := w.hostDlg
	if d == nil || d.test.res == nil {
		return
	}
	offer, ok := d.test.res.HostKeyOffer()
	if !ok {
		return
	}
	replace := offer.Kind == "host-key-mismatch"
	send := func() {
		w.work(func() {
			c, cancel := ctx()
			defer cancel()
			err := w.be.TrustHostKey(c, offer, replace)
			w.post(func() {
				if err != nil {
					w.fail("trust host key", err)
					return
				}
				w.say("Host key saved")
				if w.hostDlg == d {
					w.loadHostKey(d)
					d.test = hostTest{gen: d.test.gen + 1}
					w.runHostTest()
				}
			})
		})
	}
	if replace {
		d.confirm = &hostConfirm{
			title:   "Replace host key?",
			message: "Replace the saved key for " + offer.Hostname + " with " + offer.Fingerprint + "? Only do this if you know the server was reinstalled.",
			action:  "Replace key", run: send,
		}
		return
	}
	send()
}

// ---- pickers ----

type pickEntry struct{ value, label, desc string }

// folderEntries is groupOptionEntries: "No group" and every folder as "Parent / Child", by name.
func folderEntries(groups []keelclient.Group) []pickEntry {
	byID := map[string]keelclient.Group{}
	for _, g := range groups {
		byID[g.ID] = g
	}
	label := func(g keelclient.Group) string {
		parts := []string{g.Name}
		for seen := 0; g.ParentID != "" && seen < 16; seen++ {
			p, ok := byID[g.ParentID]
			if !ok {
				break
			}
			parts = append(parts, p.Name)
			g = p
		}
		slices.Reverse(parts)
		return strings.Join(parts, " / ")
	}
	out := []pickEntry{{"", "No group", ""}}
	var rest []pickEntry
	for _, g := range groups {
		rest = append(rest, pickEntry{g.ID, label(g), ""})
	}
	sort.SliceStable(rest, func(i, j int) bool { return strings.ToLower(rest[i].label) < strings.ToLower(rest[j].label) })
	return append(out, rest...)
}

// jumpEntries is fillJumpSelect: None, or a saved SSH host with no jump host of its own (one hop), not this host.
// A jump host that no longer exists stays listed, so saving cannot quietly turn a jump connect into a direct one.
func jumpEntries(hosts []keelclient.Host, self, current string) []pickEntry {
	out := []pickEntry{{"", "None", ""}}
	found := current == ""
	for _, h := range hosts {
		if h.ID == "" || h.ID == self || h.AuthType == "local" || h.JumpHost != "" {
			if h.ID == current {
				found = true
				out = append(out, pickEntry{h.ID, h.Name, hostSubtitle(h)})
			}
			continue
		}
		if h.ID == current {
			found = true
		}
		name := h.Name
		if name == "" {
			name = h.Address
		}
		out = append(out, pickEntry{h.ID, name, hostSubtitle(h)})
	}
	if !found {
		out = append(out, pickEntry{current, "Missing host — pick another", ""})
	}
	return out
}

// identityEntries is fillIdentitySelect: every identity with its kind and user, and a missing one kept listed.
func identityEntries(ids []keelclient.Identity, current string) []pickEntry {
	var out []pickEntry
	found := current == ""
	for _, id := range ids {
		kind := "Password"
		if id.Kind == "key" {
			kind = "Private key"
		}
		desc := kind
		if id.Username != "" {
			desc = kind + " · " + id.Username
		}
		if id.ID == current {
			found = true
		}
		out = append(out, pickEntry{id.ID, id.Name, desc})
	}
	if !found {
		out = append(out, pickEntry{current, "Missing identity — pick another", ""})
	}
	return out
}

// setAuth changes the Authentication option.
func (d *hostDialog) setAuth(a string) {
	d.f.auth = a
	d.defaultIdentity()
}

// setFolder picks a folder; choosing one named production turns Treat as production on (it never turns it off).
func (d *hostDialog) setFolder(id string) {
	d.f.groupID = id
	if isProductionGroup(d.groups, id) {
		d.f.production = true
	}
}
