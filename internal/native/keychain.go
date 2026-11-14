package native

import (
	"context"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The Keychain page (docs/ui-redesign.md §5.11): the identities in the vault, SSH keys then passwords, with the
// selected one beside the list. The words and rules are KeychainPage and IdentityList in internal/ui, identityDialog
// in dialogs.templ, identityDetailHTML in lib/vaultview.js and the identity handlers in term.js.
//
// What the page can show is what GET /api/identities says: the kind, the user, when it was added and whether a
// secret is stored. It never holds or shows a private key, a passphrase or a password (keelclient.Identity has no
// field for one). The fingerprint, public key, comment and algorithm badge of §5.11 are not in that answer and
// not in the webview; they wait for a server change (docs/native.md).

// keychainPage is the state of the Keychain tab.
type keychainPage struct {
	filter   string
	selected string // the identity id; the first one while this is not in the list
	idents   []keelclient.Identity
	loaded   bool
	err      string
	filterH  ui.Handle
}

func (p *keychainPage) load(w *workspace)                { w.loadIdentities() }
func (p *keychainPage) view(w *workspace, c *ui.Context) { w.keychainView(c, p) }
func (p *keychainPage) newItem(w *workspace)             { w.newIdentity() }

func (p *keychainPage) String() string {
	return fmt.Sprintf("keychainPage{%d identities}", len(p.idents))
}

func (p *keychainPage) GoString() string { return p.String() }

// identityKind is vaultview.js identityKind: anything but a key is a password.
func identityKind(id keelclient.Identity) string {
	if id.Kind == "key" {
		return "key"
	}
	return "password"
}

func identityKindLabel(id keelclient.Identity) string {
	if identityKind(id) == "key" {
		return "SSH key"
	}
	return "Password"
}

// identitiesOfKind is lists.templ identitiesOfKind.
func identitiesOfKind(ids []keelclient.Identity, kind string) []keelclient.Identity {
	var out []keelclient.Identity
	for _, id := range ids {
		if identityKind(id) == kind {
			out = append(out, id)
		}
	}
	return out
}

// identityHostsOf are the hosts that sign in with id (vaultview.js identityHosts).
func identityHostsOf(hosts []keelclient.Host, id string) []keelclient.Host {
	var out []keelclient.Host
	for _, h := range hosts {
		if h.AuthType == keelclient.AuthIdentity && h.IdentityID == id {
			out = append(out, h)
		}
	}
	return out
}


// identitySubtitle is the list line under an identity: kind, user, hosts (lists.templ identitySubtitle).
func identitySubtitle(hosts []keelclient.Host, id keelclient.Identity) string {
	parts := []string{identityKindLabel(id)}
	if id.Username != "" {
		parts = append(parts, id.Username)
	}
	parts = append(parts, countWord(len(identityHostsOf(hosts, id.ID)), "host", "hosts"))
	return strings.Join(parts, " · ")
}

// shown is the identities the filter keeps (name and user, any case).
func (p *keychainPage) shown() []keelclient.Identity {
	q := strings.ToLower(strings.TrimSpace(p.filter))
	if q == "" {
		return p.idents
	}
	var out []keelclient.Identity
	for _, id := range p.idents {
		if strings.Contains(strings.ToLower(id.Name+" "+id.Username), q) {
			out = append(out, id)
		}
	}
	return out
}

// current is the identity the detail shows: the selected one, else the first (term.js syncIdentityPage).
func (p *keychainPage) current() (keelclient.Identity, bool) {
	for _, id := range p.idents {
		if id.ID == p.selected {
			return id, true
		}
	}
	if len(p.idents) > 0 {
		return p.idents[0], true
	}
	return keelclient.Identity{}, false
}

// dateLabel is vaultview.js dateLabel in the form of logDateLabel: "Jan 2, 2006".
func dateLabel(id keelclient.Identity) string {
	if id.CreatedAt.IsZero() {
		return ""
	}
	return clockMDY(id.CreatedAt)
}

// ---- reading ----

// loadIdentities reads the identities (and with them the hosts, so that "Used by" is as the server has it).
func (w *workspace) loadIdentities() {
	call(w, w.be.Identities, func(ids []keelclient.Identity, err error) {
		t := w.pageTab("keychain")
		if t == nil {
			return
		}
		p := pageAs[*keychainPage](t)
		if err != nil {
			p.err = "Could not read the Keychain: " + hostError(err)
			return
		}
		p.err, p.loaded, p.idents = "", true, ids
	})
}

// ---- the identity dialog ----

// identityForm is what the identity dialog's fields hold. The secrets are write-only: they are never filled from
// a saved identity and are cleared when the dialog closes or the request is sent.
type identityForm struct {
	name, username, kind             string
	password, privateKey, passphrase string
}

// String prints a form without its secrets (so that %v and %+v of a dialog never show them).
func (f identityForm) String() string { return fmt.Sprintf("{%s %s %s}", f.name, f.username, f.kind) }

func (f identityForm) GoString() string { return "identityForm" + f.String() }

type identityDialog struct {
	id     string // "" for New identity
	f      identityForm
	err    string
	saving bool
	focus  bool

	nameH, userH, passH, keyH, phraseH ui.Handle
}

func (d *identityDialog) String() string { return fmt.Sprintf("identityDialog{%s %s}", d.id, d.f) }

func (d *identityDialog) GoString() string { return d.String() }

// newIdentity opens New identity.
func (w *workspace) newIdentity() { w.openIdentityDialog(nil) }

// editIdentity opens Edit identity: the name, user and kind of the saved one, and blank secrets.
func (w *workspace) editIdentity(id keelclient.Identity) { w.openIdentityDialog(&id) }

func (w *workspace) openIdentityDialog(id *keelclient.Identity) {
	w.closeIdentityDialog()
	d := &identityDialog{f: identityForm{kind: "password"}, focus: true}
	if id != nil {
		d.id = id.ID
		d.f = identityForm{name: id.Name, username: id.Username, kind: identityKind(*id)}
	}
	w.identDlg = d
}

// closeIdentityDialog closes without a request and clears the secrets.
func (w *workspace) closeIdentityDialog() {
	d := w.identDlg
	if d == nil {
		return
	}
	d.f.password, d.f.privateKey, d.f.passphrase = "", "", ""
	w.identDlg = nil
}

// validate is the server's own word for a missing name (httpapi validateIdentity); "" is fine.
func (f identityForm) validate() string {
	if strings.TrimSpace(f.name) == "" {
		return "name is required"
	}
	return ""
}

// input is the request this form makes: only the secret of the chosen kind is sent, and a blank one is left out so
// that the server keeps the saved one.
func (f identityForm) input(id string) keelclient.IdentityInput {
	in := keelclient.IdentityInput{ID: id, Name: strings.TrimSpace(f.name), Username: strings.TrimSpace(f.username), Kind: f.kind}
	switch f.kind {
	case "key":
		in.PrivateKey, in.Passphrase = f.privateKey, f.passphrase
	default:
		in.Password = f.password
	}
	return in
}

// saveIdentity is Save identity: it creates or updates the identity and closes. A refusal stays in the dialog.
func (w *workspace) saveIdentity() {
	d := w.identDlg
	if d == nil || d.saving {
		return
	}
	if msg := d.f.validate(); msg != "" {
		d.err = msg
		return
	}
	in := d.f.input(d.id)
	d.saving, d.err = true, ""
	d.f.password, d.f.privateKey, d.f.passphrase = "", "", "" // they are in in now, and in only goes in this request
	call(w, func(c context.Context) (keelclient.Identity, error) {
		saved, err := w.be.SaveIdentity(c, in)
		in = keelclient.IdentityInput{}
		return saved, err
	}, func(saved keelclient.Identity, err error) {
		d.saving = false
		if err != nil {
			d.err = hostError(err)
			return
		}
		if w.identDlg == d {
			w.closeIdentityDialog()
		}
		if t := w.pageTab("keychain"); t != nil && d.id == "" {
			pageAs[*keychainPage](t).selected = saved.ID
		}
		w.say("Identity saved")
		w.loadIdentities()
	})
}

// ---- delete ----

// askDeleteIdentity is Delete identity: it asks first, with the webview's words. An identity a host still uses is
// not asked about: the server would clear it from those hosts, so the page refuses (vaultview.js identityDetailHTML).
func (w *workspace) askDeleteIdentity(id keelclient.Identity) {
	if n := len(identityHostsOf(w.hosts, id.ID)); n > 0 {
		w.warn("Remove " + id.Name + " from " + countWord(n, "host", "hosts") + " first")
		return
	}
	w.askConfirm(&hostConfirm{title: "Confirm", message: "Delete identity “" + id.Name + "”?", action: "Remove", run: func() { w.deleteIdentity(id) }})
}

func (w *workspace) deleteIdentity(id keelclient.Identity) {
	callErr(w, func(c context.Context) error { return w.be.DeleteIdentity(c, id.ID) }, func(err error) {
		if err != nil {
			w.fail("delete identity", err)
			return
		}
		w.loadIdentities()
		w.reloadHosts()
	})
}
