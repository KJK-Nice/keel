package native

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// First run (docs/ui-redesign.md §5.15): step 1 creates the vault (or shows the one that exists), step 2 brings
// the hosts of the ssh config, step 3 says what the agent promises. Step 2 is also what the palette's
// "Import hosts from ~/.ssh/config" opens on its own. Like the Unlock screen it replaces the window while it shows.
//
// Secrets. The passphrase of a new vault and the passphrase of a key to copy are typed into masked fields, go
// only in the one request that needs them, are cleared when it is sent (and when the screen closes), and are in
// no log, error, file, preference or saved workspace. firstRun and importSel print without them.

const (
	stepVault = iota
	stepHosts
	stepAgent
)

const (
	flowFirstRun = "first-run"
	flowImport   = "import"
)

type firstRun struct {
	flow    string
	step    int
	vault   keelclient.Vault
	created bool
	busy    bool

	// Step 1.
	pass, pass2 string
	passErr     string
	existing    bool

	// Step 2.
	preview   *keelclient.SSHPreview
	sel       *importSel
	lead      string
	keyErrs   map[string]string
	notesOpen bool
	notice    string
	noticeBad bool
	gen       int // the preview read in flight; a stale answer is dropped

	// Step 3: the provider chosen ("none" or "gemini"), the key typed (masked; emptied as it is sent),
	// the switch "Suggest commands when I type plain language", and the provider's own words (GET /api/agent/providers).
	agentGemini  bool
	agentKey     string
	agentKeyH    ui.Handle
	agentErr     string
	agentOff     bool // the switch is off (it starts on)
	agentProv    *keelclient.AgentProvider
	agentProvGen int // the descriptor read in flight

	wantTries int
	footFn    func()
	want      string // the field that takes the keyboard in the next frame: "pass", "pass2" or "key:<path>"
	passH     ui.Handle
	pass2H    ui.Handle
	keyH      map[string]*ui.Handle
}

func (f *firstRun) String() string {
	return fmt.Sprintf("firstRun{%s step=%d created=%v busy=%v}", f.flow, f.step, f.created, f.busy)
}

func (f *firstRun) GoString() string { return f.String() }

func (f *firstRun) clearSecrets() {
	f.pass, f.pass2, f.agentKey = "", "", ""
	if f.sel != nil {
		f.sel.clearSecrets()
	}
}

func (f *firstRun) handleFor(path string) *ui.Handle {
	if f.keyH == nil {
		f.keyH = map[string]*ui.Handle{}
	}
	h := f.keyH[path]
	if h == nil {
		h = new(ui.Handle)
		f.keyH[path] = h
	}
	return h
}

// usesKeychain: the vault exists and keel serve opened it with the keychain.
func (f *firstRun) usesKeychain() bool { return !f.vault.NeedsSetup && f.vault.Mode == "keychain" }

// ---- opening and closing ----

// showFirstRun puts step 1 over the window, for a vault that needs setup or whose first run is open.
func (w *workspace) showFirstRun(v keelclient.Vault) {
	w.blocked = ""
	w.first = &firstRun{flow: flowFirstRun, step: stepVault, vault: v}
	w.first.want = "pass"
	if !v.NeedsSetup {
		w.first.want = ""
	}
}

func (w *workspace) closeFirstRun() {
	if f := w.first; f != nil {
		f.clearSecrets()
		w.first = nil
	}
}

// openImport is the palette's "Import hosts from ~/.ssh/config": step 2 on its own over the workspace.
func (w *workspace) openImport() {
	if w.first != nil || w.gate != nil {
		return
	}
	f := &firstRun{flow: flowImport}
	w.first = f
	callIf(w, func() bool { return w.first == f }, w.be.Vault, func(v keelclient.Vault, err error) {
		if err == nil {
			f.vault = v
		}
		w.showHostsStep(f)
	})
}

// ---- step 1 ----

func (w *workspace) submitVault() {
	f := w.first
	if f == nil || f.busy || f.step != stepVault {
		return
	}
	if !f.vault.NeedsSetup {
		w.showHostsStep(f)
		return
	}
	switch {
	case f.pass == "":
		f.passErr, f.want = "Choose a passphrase.", "pass"
		return
	case f.pass != f.pass2:
		f.passErr, f.want = "The passphrases don't match.", "pass2"
		return
	}
	pass := f.pass
	f.pass, f.pass2, f.passErr, f.busy = "", "", "", true // the passphrase is only in this request from here on
	callIf(w, func() bool { return w.first == f }, func(c context.Context) (v keelclient.Vault, err error) {
		err = w.be.SetupVault(c, pass)
		pass = ""
		if err == nil {
			v, err = w.be.Vault(c) // the first run goes on only while it is open
		}
		return v, err
	}, func(v keelclient.Vault, err error) {
		f.busy = false
		if err != nil {
			f.passErr, f.want = vaultError(err), "pass"
			return
		}
		f.created, f.vault = true, v
		if v.FirstRun {
			w.showHostsStep(f)
			return
		}
		w.closeFirstRun()
		w.start()
	})
}

// vaultError is the line under the passphrase fields: the server's own reason, else the status.
func vaultError(err error) string {
	var se *keelclient.StatusError
	if errors.As(err, &se) && se.Msg != "" {
		return se.Msg
	}
	if errors.Is(err, keelclient.ErrVaultExists) {
		return "The vault already exists"
	}
	return err.Error()
}

var vaultFileSuffix = regexp.MustCompile(`[\\/]keel\.vault$`)

func dataFolder(path string) string {
	if d := vaultFileSuffix.ReplaceAllString(path, ""); d != "" {
		return d
	}
	return "the Keel data folder"
}

func keychainDesc(f *firstRun, keychain string) string {
	switch {
	case f.usesKeychain():
		return "The vault key lives in your login keychain. Keel opens without asking."
	case f.vault.KeyringOff:
		return "Keel was started with KEEL_KEYRING=off, so it does not use the keychain."
	case f.vault.NeedsSetup:
		return keychain + " didn't answer, so Keel can't keep the vault key there. Use a passphrase, or allow Keel in " + keychain + " and open Keel again."
	}
	return "This vault opens with a passphrase."
}

func passDesc(f *firstRun, keychain string) string {
	if f.usesKeychain() {
		return "Keel already made this vault with " + keychain + ". It does not re-key a vault here."
	}
	return "You type it each time keel serve starts. Pick this on shared machines or when there is no keychain."
}

func vaultEnc(f *firstRun) string {
	if f.usesKeychain() {
		return "age, with the wrapping key in your keychain"
	}
	return "age, with a key made from your passphrase (scrypt)"
}

func nextLabel(f *firstRun) string {
	if f.vault.NeedsSetup {
		return "Create vault"
	}
	return "Continue"
}

func firstRunKeychain() string { return keychainName() }

// ---- step 2 ----

func (w *workspace) showHostsStep(f *firstRun) {
	f.step, f.preview, f.sel, f.notice, f.keyErrs, f.notesOpen = stepHosts, nil, nil, "", nil, false
	f.gen++
	gen := f.gen
	f.lead = "Reading " + f.sshConfig() + "…"
	callIf(w, func() bool { return w.first == f && f.gen == gen && f.step == stepHosts }, w.be.SSHConfigPreview, func(p keelclient.SSHPreview, err error) {
		if err != nil {
			f.lead = "Keel could not read " + f.sshConfig() + ": " + vaultError(err)
			return
		}
		f.preview, f.sel, f.lead = &p, defaultSelection(p), leadText(p)
	})
}

func (f *firstRun) sshConfig() string {
	if f.vault.SSHConfig != "" {
		return f.vault.SSHConfig
	}
	return "~/.ssh/config"
}

func (f *firstRun) addCount() int {
	if f.preview == nil || f.sel == nil {
		return 0
	}
	return selectedCount(*f.preview, f.sel)
}

func (f *firstRun) toggleHost(alias string) {
	if f.sel == nil {
		return
	}
	if f.sel.hosts[alias] {
		delete(f.sel.hosts, alias)
	} else {
		f.sel.hosts[alias] = true
	}
}

// toggleAll is the select-all box: every host off when all are on, else every host on.
func (f *firstRun) toggleAll() {
	if f.preview == nil || f.sel == nil {
		return
	}
	every := allState(*f.preview, f.sel) == checkOn
	for _, h := range f.preview.Hosts {
		if every {
			delete(f.sel.hosts, h.Alias)
		} else {
			f.sel.hosts[h.Alias] = true
		}
	}
}

func (f *firstRun) toggleNoJump(alias string) {
	if f.sel == nil {
		return
	}
	if f.sel.noJump[alias] {
		delete(f.sel.noJump, alias)
	} else {
		f.sel.noJump[alias] = true
	}
}

func (f *firstRun) toggleKey(k keelclient.ConfigKey) {
	if f.sel == nil || !copyable(k) {
		return
	}
	st := f.sel.keyOf(k.Path)
	st.on = !st.on
	delete(f.keyErrs, k.Path)
	if st.on && k.Encrypted {
		f.want = "key:" + k.Path
	}
}

// runImport is Add hosts and ↵ in a key's passphrase field.
func (w *workspace) runImport() {
	f := w.first
	if f == nil || f.step != stepHosts || f.busy || f.preview == nil || f.sel == nil {
		return
	}
	p, sel := *f.preview, f.sel
	if i := missingPassphrase(p, sel); i >= 0 {
		k := p.Keys[i]
		f.keyErrs = map[string]string{k.Path: "Enter the passphrase for " + k.Name + ", or turn this key off."}
		f.want = "key:" + k.Path
		return
	}
	if h := missingJump(p, sel); h != nil {
		f.notice, f.noticeBad = "Add "+h.Alias+"'s jump host too, or tick Without the jump to add it as a direct host.", true
		return
	}
	req := importRequest(p, sel)
	if len(req.Hosts) == 0 {
		return
	}
	f.busy, f.notice, f.keyErrs = true, "", nil
	sel.clearSecrets() // the passphrases are in req now, and req only goes in this request
	callIf(w, func() bool { return w.first == f }, func(c context.Context) (keelclient.ImportResult, error) {
		res, err := w.be.SSHConfigImport(c, req)
		req = keelclient.ImportRequest{}
		return res, err
	}, func(res keelclient.ImportResult, err error) {
		f.busy = false
		if err != nil {
			w.importRefused(f, p, err)
			return
		}
		f.notice, f.noticeBad = importedText(res), len(res.Failed) > 0
		w.afterHosts(f)
		if w.live {
			w.reloadHosts()
		}
	})
}

// importRefused shows a refusal: under the key it is about when it names one, else as the notice.
func (w *workspace) importRefused(f *firstRun, p keelclient.SSHPreview, err error) {
	var se *keelclient.StatusError
	if errors.As(err, &se) && se.Key != "" {
		for _, k := range p.Keys {
			if k.Path != se.Key {
				continue
			}
			msg := se.Msg
			switch se.ErrorKind {
			case "wrongPassphrase":
				msg = "That passphrase doesn't open " + k.Name + "."
			case "passphrase":
				msg = "Enter the passphrase for " + k.Name + "."
			}
			f.keyErrs = map[string]string{k.Path: msg}
			f.want = "key:" + k.Path
			return
		}
	}
	f.notice, f.noticeBad = vaultError(err), true
}

// afterHosts leaves step 2: to the Agent step in the first run, and back to the window when the palette opened
// the import on its own.
func (w *workspace) afterHosts(f *firstRun) {
	if f.flow == flowFirstRun {
		w.showAgentStep(f)
		return
	}
	w.closeFirstRun()
	if f.notice != "" {
		if f.noticeBad {
			w.warn(f.notice)
		} else {
			w.say(f.notice)
		}
	}
}

// ---- step 3 ----

// showAgentStep chooses Not now, empties the key, turns the switch on, and reads the provider's own
// words once (the descriptor of Google Gemini; a failed read leaves the card's defaults).
func (w *workspace) showAgentStep(f *firstRun) {
	f.step = stepAgent
	f.agentGemini, f.agentKey, f.agentErr, f.agentOff = false, "", "", false
	if f.agentProv != nil {
		return
	}
	f.agentProvGen++
	gen := f.agentProvGen
	callIf(w, func() bool { return w.first == f && f.agentProvGen == gen }, w.be.AgentProviders, func(list []keelclient.AgentProvider, err error) {
		if err != nil {
			return
		}
		for i := range list {
			if list[i].ID == "gemini" {
				f.agentProv = &list[i]
			}
		}
	})
}

// pickAgentProvider: choosing Gemini shows the key field and gives it the keyboard.
func (f *firstRun) pickAgentProvider(gemini bool) {
	f.agentGemini, f.agentErr = gemini, ""
	if gemini {
		f.want = "agentkey"
	}
}

// openKeel is Open Keel: a typed key is stored in the vault (PUT /api/agent/keys/gemini: not checked
// against the provider, so nothing is sent anywhere), the switch is saved either way, and then the first run ends. A
// failed save keeps the step open with the reason. The key is emptied the moment it is sent and is in no log or error.
func (w *workspace) openKeel(f *firstRun) {
	if w.first != f || f.busy {
		return
	}
	key := strings.TrimSpace(f.agentKey)
	if f.agentGemini && key == "" {
		f.agentErr, f.want = "Paste your key, or choose Not now.", "agentkey"
		return
	}
	in := keelclient.AgentKeyInput{Provider: "gemini", Key: key}
	if f.agentGemini {
		f.agentKey = ""
	}
	key = ""
	on := "on"
	if f.agentOff {
		on = "off"
	}
	gemini := f.agentGemini
	f.busy, f.agentErr = true, ""
	callErrIf(w, func() bool { return w.first == f }, func(c context.Context) (err error) {
		if gemini {
			err = w.be.SetAgentKey(c, in)
			in = keelclient.AgentKeyInput{}
		}
		if err == nil {
			_, err = w.be.SaveAgentSettings(c, keelclient.AgentSettingsPatch{Suggestions: &on})
		}
		return err
	}, func(err error) {
		f.busy = false
		if err != nil {
			f.agentErr = keyError(err)
			return
		}
		w.refreshSuggestSettings()
		w.finishFirstRun(f)
	})
}

func (w *workspace) finishFirstRun(f *firstRun) {
	if w.first != f || f.busy {
		return
	}
	f.busy = true
	callErrIf(w, func() bool { return w.first == f }, w.be.FirstRunDone, func(err error) {
		w.closeFirstRun()
		if err != nil {
			w.fail("finish first run", err)
		}
		w.start()
	})
}
