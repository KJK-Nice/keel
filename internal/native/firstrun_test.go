package native

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The First run flow on the fake backend. The passphrases here are made up and open nothing.
const (
	scratchPass = "scratch-vault-pass-for-tests"
	scratchKey  = "scratch-key-pass-for-tests"
)

func setupBackend() *fakeBackend {
	fb := &fakeBackend{vault: keelclient.Vault{Locked: true, NeedsSetup: true, KeyringOff: true, Path: "~/keel/keel.vault", SSHConfig: "~/.ssh/config"}, hosts: testHosts}
	fb.preview = testPreview()
	return fb
}

func firstRunRig(t *testing.T, fb *fakeBackend) *rig {
	t.Helper()
	r := &rig{fb: fb}
	r.w = newWorkspace(fb)
	r.w.work = func(fn func()) { fn() }
	r.w.post = func(fn func()) { fn() }
	r.w.mkBody = placeholder(&r.released)
	r.tt = ui.NewTester(r.w.view, 1280, 1500) // tall: a step is a scrolling page, and the tests read all of it
	r.w.start()
	r.tt.Frame()
	if r.w.first == nil {
		t.Fatal("First run is not showing")
	}
	return r
}

func (r *rig) texts(t *testing.T, want ...string) {
	t.Helper()
	for _, s := range want {
		if !r.tt.HasText(s) {
			t.Errorf("no text %q", s)
		}
	}
}

func TestFirstRunStep1WordsWhenTheVaultNeedsSetup(t *testing.T) {
	fb := setupBackend()
	r := firstRunRig(t, fb)
	kc := keychainName(goosForTest)
	r.texts(t, "Vault", "Hosts", "Agent", "Welcome to Keel",
		"Keel keeps your SSH sessions running after you close the window. First, a vault for host passwords and keys. It is encrypted on this machine and never leaves it.",
		"Unlock with "+kc, "Recommended",
		"Keel was started with KEEL_KEYRING=off, so it does not use the keychain.",
		"Use a passphrase", "You type it each time keel serve starts. Pick this on shared machines or when there is no keychain.",
		"Passphrase", "Repeat passphrase", "Keel can't recover the vault if you lose the passphrase.",
		"Vault file", "~/keel/keel.vault", "Encryption", "age, with a key made from your passphrase (scrypt)",
		"Holds", "Host passwords, private keys and key passphrases. Host names, addresses and snippets stay in keel.json.",
		"I already have a keel.vault…", "Create vault")
	if !r.tt.Focused("Passphrase") {
		t.Error("the passphrase field does not have the keyboard")
	}
	if !slices.Equal(fb.calls, []string{"vault"}) {
		t.Errorf("calls %v", fb.calls)
	}
	// Without KEEL_KEYRING=off the keychain card says it could not keep the key there.
	fb2 := setupBackend()
	fb2.vault.KeyringOff = false
	r2 := firstRunRig(t, fb2)
	r2.texts(t, kc+" didn't answer, so Keel can't keep the vault key there. Use a passphrase, or allow Keel in "+kc+" and open Keel again.")
}

func TestFirstRunKeychainCardIsNotChoosableWhenSettingUp(t *testing.T) {
	r := firstRunRig(t, setupBackend())
	if f := r.w.first; f.usesKeychain() {
		t.Fatal("a vault that needs setup uses the keychain")
	}
	r.click(t, "Unlock with "+keychainName(goosForTest))
	if r.w.first.vault.NeedsSetup != true || len(r.fb.setups) != 0 {
		t.Error("the disabled keychain card did something")
	}
}

func TestFirstRunPassphraseValidationMessages(t *testing.T) {
	fb := setupBackend()
	r := firstRunRig(t, fb)
	r.click(t, "Create vault")
	r.texts(t, "Choose a passphrase.")
	r.fill(t, "Passphrase", scratchPass)
	r.fill(t, "Repeat passphrase", scratchPass+"x")
	r.click(t, "Create vault")
	r.texts(t, "The passphrases don't match.")
	if !r.tt.Focused("Repeat passphrase") {
		t.Error("the mismatch does not focus the repeat field")
	}
	if len(fb.setups) != 0 || len(fb.calls) != 1 {
		t.Errorf("a refused form made a request: %v %v", fb.setups, fb.calls)
	}
}

func TestFirstRunCreatesTheVaultThenAsksForHostsAndClearsThePassphrase(t *testing.T) {
	fb := setupBackend()
	r := firstRunRig(t, fb)
	r.fill(t, "Passphrase", scratchPass)
	r.fill(t, "Repeat passphrase", scratchPass)
	r.click(t, "Create vault")
	if !slices.Equal(fb.setups, []string{scratchPass}) {
		t.Fatalf("setups %v", fb.setups)
	}
	f := r.w.first
	if f == nil || f.step != stepHosts || !f.created || f.pass != "" || f.pass2 != "" {
		t.Fatalf("after Create vault: %v", f)
	}
	if want := []string{"vault", "setup vault", "vault", "ssh preview"}; !slices.Equal(fb.calls, want) {
		t.Errorf("calls %v, want %v", fb.calls, want)
	}
	r.texts(t, "Bring your hosts", "Keel found ~/.ssh/config with 3 hosts. Pick the ones to add. Your config file is not changed.", "Back", "Skip, add hosts later", "Add 1 host")
}

func TestFirstRunSetupRefusalShowsTheServersWordsAndKeepsStep1(t *testing.T) {
	fb := setupBackend()
	fb.setupErr = &keelclient.StatusError{Method: "POST", Path: "/api/vault/setup", Status: "400 Bad Request", Code: 400, Msg: "passphrase is required"}
	r := firstRunRig(t, fb)
	r.fill(t, "Passphrase", scratchPass)
	r.fill(t, "Repeat passphrase", scratchPass)
	r.click(t, "Create vault")
	r.texts(t, "passphrase is required")
	if r.w.first.step != stepVault || r.w.first.pass != "" {
		t.Errorf("step %d, passphrase held %v", r.w.first.step, r.w.first.pass != "")
	}
	if !r.tt.Focused("Passphrase") {
		t.Error("focus left the passphrase field")
	}
}

func TestFirstRunExistingVaultIsAnInstructionOnly(t *testing.T) {
	r := firstRunRig(t, setupBackend())
	if r.tt.HasText("To use a keel.vault you already have") {
		t.Fatal("the instruction shows before it is asked for")
	}
	r.click(t, "I already have a keel.vault…")
	r.texts(t, "To use a keel.vault you already have: quit Keel, run keel serve stop, put that keel.vault and its keel.json in ~/keel, then open Keel. It opens with the keychain or passphrase that made it. Keel does not change a vault it cannot open.")
	r.click(t, "I already have a keel.vault…")
	if r.tt.HasText("To use a keel.vault you already have") {
		t.Error("the instruction stays open")
	}
}

// An unlocked vault whose first run is open starts at step 1 with Continue, as gate.js does (it shows the vault
// the keychain already made).
func TestFirstRunOpenOnAnExistingVaultStartsAtStep1WithContinue(t *testing.T) {
	fb := &fakeBackend{vault: keelclient.Vault{FirstRun: true, Mode: "keychain", Path: "~/keel/keel.vault"}, hosts: testHosts, preview: testPreview()}
	r := firstRunRig(t, fb)
	kc := keychainName(goosForTest)
	r.texts(t, "Welcome to Keel", "The vault key lives in your login keychain. Keel opens without asking.",
		"Keel already made this vault with "+kc+". It does not re-key a vault here.",
		"age, with the wrapping key in your keychain", "Continue")
	if r.has("Repeat passphrase") || r.has("I already have a keel.vault…") || r.has("Create vault") {
		t.Error("a vault that exists shows the setup fields")
	}
	r.click(t, "Continue")
	if r.w.first.step != stepHosts || len(fb.setups) != 0 {
		t.Errorf("step %d, setups %v", r.w.first.step, fb.setups)
	}
	// A passphrase vault says so on its passphrase card.
	fb2 := &fakeBackend{vault: keelclient.Vault{FirstRun: true, Mode: "passphrase", KeyringOff: true}, preview: testPreview()}
	r2 := firstRunRig(t, fb2)
	r2.texts(t, "Keel was started with KEEL_KEYRING=off, so it does not use the keychain.", "age, with a key made from your passphrase (scrypt)")
}

func atHosts(t *testing.T, fb *fakeBackend) *rig {
	t.Helper()
	fb.vault = keelclient.Vault{FirstRun: true, Path: "~/keel/keel.vault", SSHConfig: "~/.ssh/config"}
	r := firstRunRig(t, fb)
	r.click(t, "Continue")
	return r
}

func TestFirstRunStep2TableRowsAndRules(t *testing.T) {
	fb := &fakeBackend{hosts: testHosts, preview: testPreview()}
	r := atHosts(t, fb)
	r.texts(t, "Host", "Address", "User", "Key", "Via", "web", "web.example.net", "deploy", "id_web", "db", "10.0.0.5:2222", "id_db",
		"Already saved", "jumpy", "ProxyJump bastion, not used", "Without the jump", "1 of 3 selected",
		"This pattern sets options for the hosts above. It is not a host to add.", "*.prod", "2 notes about this config",
		"Copy private keys into the vault", "~/.ssh/id_web", "~/.ssh/gone", "file not found", "Used by web", "Passphrase",
		"Each key is off until you turn it on. The files in ~/.ssh stay where they are; Keel only reads them. A copied key becomes a Keychain identity, encrypted in the vault with its passphrase. Hosts whose key stays off are added with password sign-in; edit them to pick a key.")
	// A pattern is not a host row.
	if r.has("row *.prod") {
		t.Error("a pattern is a host row")
	}
	// Only the new direct host starts ticked; every key switch is off.
	if got := sortedHosts(r.w.first.sel); !slices.Equal(got, []string{"web"}) {
		t.Errorf("ticked %v", got)
	}
	for path, k := range r.w.first.sel.keys {
		if k.on {
			t.Errorf("key %s starts on", path)
		}
	}
	r.click(t, "2 notes about this config")
	r.texts(t, "~/.ssh/config:14", "Match block skipped: Keel imports Host blocks only", "jumpy")
}

func TestFirstRunStep2ImportSendsExactlyTheTicks(t *testing.T) {
	fb := &fakeBackend{hosts: testHosts, preview: testPreview()}
	fb.importRes = keelclient.ImportResult{Hosts: []keelclient.Host{{ID: "h1"}, {ID: "h2"}}}
	r := atHosts(t, fb)
	r.click(t, "Add db")  // a saved host the user turns on
	r.click(t, "Add web") // and the default one off again, then on
	r.click(t, "Add web")
	r.texts(t, "2 of 3 selected", "Add 2 hosts")
	r.click(t, "Add 2 hosts")
	if len(fb.imports) != 1 {
		t.Fatalf("imports %v", fb.imports)
	}
	if got := fb.imports[0]; !slices.Equal(got.Hosts, []string{"web", "db"}) || len(got.Keys) != 0 || got.WithoutJump != nil {
		t.Errorf("request %+v", got)
	}
	f := r.w.first
	if f == nil || f.step != stepAgent || f.notice != "Added 2 hosts" {
		t.Fatalf("after the import: %v %q", f, f.notice)
	}
	r.texts(t, "An agent that suggests, never types", "Added 2 hosts")
}

func TestFirstRunStep2SelectAllAndASummary(t *testing.T) {
	r := atHosts(t, &fakeBackend{hosts: testHosts, preview: testPreview()})
	r.click(t, "Select all hosts")
	r.texts(t, "3 of 3 selected", "Add 3 hosts")
	r.click(t, "Select all hosts")
	r.texts(t, "0 of 3 selected")
	if !r.has("Add hosts") {
		t.Error("the button does not say Add hosts with nothing ticked")
	}
	r.click(t, "Add hosts") // disabled: nothing is sent
	if len(r.fb.imports) != 0 {
		t.Error("an empty import was sent")
	}
}

func TestFirstRunStep2ProxyJumpNeedsWithoutTheJump(t *testing.T) {
	fb := &fakeBackend{hosts: testHosts, preview: testPreview()}
	r := atHosts(t, fb)
	r.click(t, "Add jumpy")
	r.click(t, "Add 2 hosts")
	r.texts(t, "Add jumpy's jump host too, or tick Without the jump to add it as a direct host.")
	if len(fb.imports) != 0 || r.w.first.step != stepHosts {
		t.Fatal("the import went on")
	}
	r.click(t, "Add jumpy without the jump")
	r.click(t, "Add 2 hosts")
	if len(fb.imports) != 1 || !slices.Equal(fb.imports[0].WithoutJump, []string{"jumpy"}) || !slices.Equal(fb.imports[0].Hosts, []string{"web", "jumpy"}) {
		t.Fatalf("imports %+v", fb.imports)
	}
}

func TestFirstRunKeyCopyIsOffByDefaultAndAsksForItsPassphrase(t *testing.T) {
	fb := &fakeBackend{hosts: testHosts, preview: testPreview()}
	r := atHosts(t, fb)
	r.click(t, "Add 1 host")
	if len(fb.imports) != 1 || len(fb.imports[0].Keys) != 0 {
		t.Fatalf("a key was sent without its switch: %+v", fb.imports)
	}
	// Again, with the encrypted key turned on and no passphrase.
	fb2 := &fakeBackend{hosts: testHosts, preview: testPreview()}
	r2 := atHosts(t, fb2)
	r2.click(t, "Copy ~/.ssh/id_db into the vault")
	if !r2.has("Passphrase for id_db") || !r2.tt.Focused("Passphrase for id_db") {
		t.Fatal("no passphrase field, or it has no keyboard")
	}
	r2.click(t, "Add 1 host")
	r2.texts(t, "Enter the passphrase for id_db, or turn this key off.")
	if len(fb2.imports) != 0 {
		t.Fatal("sent without the passphrase")
	}
	r2.click(t, "Passphrase for id_db")
	r2.tt.Type(scratchKey)
	r2.tt.Frame()
	r2.click(t, "Add 1 host")
	if len(fb2.imports) != 1 || !slices.Equal(fb2.imports[0].Keys, []keelclient.ImportKey{{Path: pathDB, Passphrase: scratchKey}}) {
		t.Fatalf("imports %+v", fb2.imports)
	}
	// The unreadable key's switch is disabled.
	if err := r2.tt.Click("Copy ~/.ssh/gone into the vault"); err == nil && r2.w.first != nil && r2.w.first.sel.keys[pathBad].on {
		t.Error("an unreadable key was turned on")
	}
}

func TestFirstRunKeyPassphraseIsClearedWhenSentAndNeverPrinted(t *testing.T) {
	fb := &fakeBackend{hosts: testHosts, preview: testPreview(), importErr: &keelclient.StatusError{Method: "POST", Path: "/api/ssh-config/import", Status: "400", Code: 400,
		Msg: "wrong", ErrorKind: "wrongPassphrase", Key: pathDB}}
	r := atHosts(t, fb)
	r.click(t, "Copy ~/.ssh/id_db into the vault")
	r.click(t, "Passphrase for id_db")
	r.tt.Type(scratchKey)
	r.tt.Frame()
	f := r.w.first
	for _, s := range []string{fmt.Sprintf("%v", f), fmt.Sprintf("%+v", f), fmt.Sprintf("%#v", f), fmt.Sprintf("%+v", *f.sel.keys[pathDB])} {
		if strings.Contains(s, scratchKey) {
			t.Fatalf("the screen prints the passphrase: %s", s)
		}
	}
	r.click(t, "Add 1 host")
	if f.sel.keys[pathDB].pass != "" {
		t.Error("the passphrase is still held after it was sent")
	}
	if len(fb.imports) != 1 || fb.imports[0].Keys[0].Passphrase != scratchKey {
		t.Fatalf("%+v", fb.imports)
	}
	// The refusal is shown under the key it names, with the webview's words, and stays on step 2.
	r.texts(t, "That passphrase doesn't open id_db.")
	if f.step != stepHosts || !r.tt.Focused("Passphrase for id_db") {
		t.Errorf("step %d", f.step)
	}
}

func TestFirstRunImportRefusalWithoutAKeyShowsTheServersMessage(t *testing.T) {
	fb := &fakeBackend{hosts: testHosts, preview: testPreview(), importErr: &keelclient.StatusError{Code: 400, Msg: "invalid import payload"}}
	r := atHosts(t, fb)
	r.click(t, "Add 1 host")
	r.texts(t, "invalid import payload")
	if r.w.first.step != stepHosts {
		t.Error("left step 2")
	}
}

func TestFirstRunImportNamesHostsThatWereNotAdded(t *testing.T) {
	fb := &fakeBackend{hosts: testHosts, preview: testPreview(), importRes: keelclient.ImportResult{
		Hosts: []keelclient.Host{{ID: "h1"}}, Identities: []keelclient.Identity{{ID: "i1"}},
		Failed: []keelclient.ImportFailure{{Alias: "db", Error: "its jump host bastion was not added"}}}}
	r := atHosts(t, fb)
	r.click(t, "Add 1 host")
	r.texts(t, "Added 1 host and 1 key. Not added: db (its jump host bastion was not added)")
	if !r.w.first.noticeBad {
		t.Error("a partial import is not marked")
	}
}

func TestFirstRunSkipAndBack(t *testing.T) {
	fb := &fakeBackend{hosts: testHosts, preview: testPreview()}
	r := atHosts(t, fb)
	r.click(t, "Back")
	if r.w.first.step != stepVault {
		t.Fatal("Back did not return to step 1")
	}
	r.click(t, "Continue")
	r.click(t, "Skip, add hosts later")
	if len(fb.imports) != 0 || r.w.first.step != stepAgent {
		t.Fatalf("imports %v, step %d", fb.imports, r.w.first.step)
	}
	r.texts(t, "An agent that suggests, never types",
		"Ask it about a session, or type plain language at a prompt and press ⇥. It is optional, and you can set it up later in Settings.",
		"Reads only the panes you pick", `Every pane it can read carries an "Agent reads" mark. Hosts can opt out.`,
		"Suggests commands; you press Type", "The agent never types into a pane. Nothing reaches one until you accept it. Destructive commands need ⇧⇥.",
		"Secrets are redacted, the vault stays closed", "No passwords, keys or snippets leave it. Secret-looking values in a pane are redacted before sending.",
		"Back", "Set up later", "Open Keel")
	r.click(t, "Back")
	if r.w.first.step != stepHosts {
		t.Error("Back from step 3 did not return to step 2")
	}
}

func TestFirstRunNoSSHConfigAndAReadFailure(t *testing.T) {
	fb := &fakeBackend{hosts: testHosts, preview: keelclient.SSHPreview{Path: "~/.ssh/config"}}
	r := atHosts(t, fb)
	r.texts(t, "Keel found no ~/.ssh/config. Add hosts later with New host.")
	fb2 := &fakeBackend{hosts: testHosts, previewErr: &keelclient.StatusError{Code: 500, Msg: "boom"}}
	r2 := atHosts(t, fb2)
	r2.texts(t, "Keel could not read ~/.ssh/config: boom")
	// No table: only Skip, and Add hosts is off.
	r2.click(t, "Add hosts")
	if len(fb2.imports) != 0 {
		t.Error("imported with no preview")
	}
}

func TestFirstRunOpenKeelEndsTheFirstRunAndOpensTheWorkspace(t *testing.T) {
	for _, button := range []string{"Open Keel", "Set up later"} {
		fb := &fakeBackend{hosts: testHosts, preview: testPreview()}
		r := atHosts(t, fb)
		r.click(t, "Skip, add hosts later")
		r.click(t, button)
		if fb.firstRunDone != 1 {
			t.Errorf("%s: first-run/done called %d times", button, fb.firstRunDone)
		}
		if r.w.first != nil || !r.w.live || !r.tt.HasText("Filter hosts") || !r.tt.HasText("Local Shell") {
			t.Errorf("%s: first %v live %v", button, r.w.first != nil, r.w.live)
		}
		if len(r.w.tabs) != 1 || !r.w.tabs[0].isNew() {
			t.Errorf("%s: tabs %d", button, len(r.w.tabs))
		}
	}
}

func TestFirstRunSecondWindowOpensStraightToTheWorkspaceOnceItIsDone(t *testing.T) {
	fb := &fakeBackend{hosts: testHosts, vault: keelclient.Vault{FirstRun: false}}
	r := newRig(t, fb)
	r.tt.Frame()
	if r.w.first != nil || !r.tt.HasText("Filter hosts") {
		t.Error("a finished first run shows again")
	}
}

func TestFirstRunSetupWithNoFirstRunLeftGoesStraightToTheWorkspace(t *testing.T) {
	fb := setupBackend()
	fb.noFirstRunAfterSetup = true // the server made the vault and its first run is not open: the gate hides itself
	r := firstRunRig(t, fb)
	r.fill(t, "Passphrase", scratchPass)
	r.fill(t, "Repeat passphrase", scratchPass)
	r.click(t, "Create vault")
	if r.w.first != nil || !r.w.live || !r.tt.HasText("Filter hosts") {
		t.Errorf("first %v live %v", r.w.first != nil, r.w.live)
	}
	if fb.firstRunDone != 0 {
		t.Error("first-run/done was called with no first run open")
	}
}

// The palette's import is step 2 on its own: no step row, Cancel instead of Skip, no Back, and it ends back in
// the window with a status line.
func TestPaletteImportOpensStep2AloneAndEndsInTheWindow(t *testing.T) {
	fb := &fakeBackend{hosts: testHosts, preview: testPreview(), vault: keelclient.Vault{SSHConfig: "~/.ssh/config"}}
	fb.importRes = keelclient.ImportResult{Hosts: []keelclient.Host{{ID: "hx"}}}
	r := newRig(t, fb)
	r.tt.Frame()
	r.openPal(t)
	r.typePal("Import hosts")
	r.press(0, ui.KeyEnter)
	f := r.w.first
	if f == nil || f.flow != flowImport || f.step != stepHosts {
		t.Fatalf("first %v", f)
	}
	r.texts(t, "Import hosts", "Keel found ~/.ssh/config with 3 hosts. Pick the ones to add. Your config file is not changed.", "Cancel", "Add 1 host")
	if r.has("Back") || r.has("Setup progress") || r.has("Skip, add hosts later") {
		t.Error("first run's own controls show in the import")
	}
	hostsBefore := fb.hostsCalls
	r.click(t, "Add 1 host")
	if r.w.first != nil || !r.tt.HasText("Filter hosts") || !r.tt.HasText("Added 1 host") || fb.hostsCalls != hostsBefore+1 {
		t.Errorf("first %v, hosts calls %d→%d", r.w.first != nil, hostsBefore, fb.hostsCalls)
	}
	if fb.firstRunDone != 0 {
		t.Error("the palette's import ended the first run")
	}
	// Cancel and Esc leave it too.
	r.openPal(t)
	r.typePal("Import hosts")
	r.press(0, ui.KeyEnter)
	r.click(t, "Cancel")
	if r.w.first != nil || len(fb.imports) != 1 {
		t.Error("Cancel did not close the import, or sent something")
	}
	r.openPal(t)
	r.typePal("Import hosts")
	r.press(0, ui.KeyEnter)
	r.press(0, ui.KeyEscape)
	if r.w.first != nil {
		t.Error("Esc did not close the import")
	}
}

func TestFirstRunHidesTheWorkspaceAndTakesNoShortcut(t *testing.T) {
	r := firstRunRig(t, setupBackend())
	r.press(ui.Cmd, ui.KeyT)
	r.press(ui.Cmd, ui.KeyK)
	if len(r.w.tabs) != 0 || r.w.pal != nil || r.tt.HasText("Filter hosts") {
		t.Error("the window behind First run acts")
	}
}

func TestVaultErrorsUseTheServersWords(t *testing.T) {
	if got := vaultError(&keelclient.StatusError{Msg: "passphrase is required", Code: 400}); got != "passphrase is required" {
		t.Error(got)
	}
	if got := vaultError(keelclient.ErrVaultExists); got != "The vault already exists" {
		t.Error(got)
	}
	if got := vaultError(errors.New("dial tcp: refused")); got != "dial tcp: refused" {
		t.Error(got)
	}
	for in, want := range map[string]string{"~/keel/keel.vault": "~/keel", `C:\k\keel.vault`: `C:\k`, "keel.vault": "keel.vault", "": "the Keel data folder"} {
		if got := dataFolder(in); got != want {
			t.Errorf("dataFolder(%q) = %q, want %q", in, got, want)
		}
	}
}
