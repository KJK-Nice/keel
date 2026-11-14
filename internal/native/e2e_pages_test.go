//go:build nativee2e

package native

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"golang.org/x/crypto/ssh"

	"keel/internal/native/keelclient"
)

// First run, Keychain and Logs against a scratch keel serve. Every daemon has KEEL_KEYRING=off, its own
// KEEL_DATA under .scratch-native/e2e and, for the import, KEEL_SSH_DIR at a copy of tests/fixtures/ssh-home/.ssh
// with two keys made here: the real ~/.ssh and the OS keychain are never touched. The passphrases and the identity
// password below are scratch values; no test prints them and the checks at the end of each scenario look for them
// in what the window drew.

const (
	e2eFirstPass  = "native-e2e-first-run-scratch"
	e2eKeyPass    = "native-e2e-locked-key-scratch"
	e2eIdentPass  = "native-e2e-identity-scratch"
	fixtureLocked = "id_e2e_locked"
	fixturePlain  = "id_e2e_plain"
)

// copyTree copies a directory of plain files.
func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.Walk(from, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		dst := filepath.Join(to, rel)
		if info.IsDir() {
			return os.MkdirAll(dst, 0o700)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// writeKey makes a fresh ed25519 key in OpenSSH's private key format.
func writeKey(t *testing.T, path, comment, passphrase string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var blk *pem.Block
	if passphrase == "" {
		blk, err = ssh.MarshalPrivateKey(priv, comment)
	} else {
		blk, err = ssh.MarshalPrivateKeyWithPassphrase(priv, comment, []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(blk), 0o600); err != nil {
		t.Fatal(err)
	}
}

// sshFixtureDir is the fixture ~/.ssh under dir: tests/fixtures/ssh-home/.ssh
// copied, plus one key without a passphrase and one with. A config's ~ is the folder above it.
func sshFixtureDir(t *testing.T, dir string) string {
	t.Helper()
	dst := filepath.Join(dir, "ssh-home", ".ssh")
	copyTree(t, filepath.Join(repoRoot(t), "tests", "fixtures", "ssh-home", ".ssh"), dst)
	writeKey(t, filepath.Join(dst, fixturePlain), "e2e-plain", "")
	writeKey(t, filepath.Join(dst, fixtureLocked), "e2e-locked", e2eKeyPass)
	return dst
}

// startFirstRunServe starts a daemon with no vault and no passphrase in its environment, so that it needs setup,
// with KEEL_SSH_DIR at the fixture.
func startFirstRunServe(t *testing.T) (addr, dir, dataDir string) {
	t.Helper()
	dir = scratchDir(t)
	ssh := sshFixtureDir(t, dir)
	addr, _ = startServeAt(t, dir, "serve", false, "KEEL_SSH_DIR="+ssh)
	return addr, dir, filepath.Join(dir, "data")
}

func (e *win) vault() keelclient.Vault {
	e.t.Helper()
	c, cancel := ctx()
	defer cancel()
	v, err := e.cl.Vault(c)
	if err != nil {
		e.t.Fatal(err)
	}
	return v
}

// hostNames are the saved hosts' names, sorted.
func (e *win) hostNames() []string {
	e.t.Helper()
	c, cancel := ctx()
	defer cancel()
	hosts, err := e.cl.Hosts(c)
	if err != nil {
		e.t.Fatal(err)
	}
	var out []string
	for _, h := range hosts {
		out = append(out, h.Name)
	}
	sort.Strings(out)
	return out
}

func (e *win) identities() []keelclient.Identity {
	e.t.Helper()
	c, cancel := ctx()
	defer cancel()
	ids, err := e.cl.Identities(c)
	if err != nil {
		e.t.Fatal(err)
	}
	return ids
}

// logEntries is GET /api/logs.
func (e *win) logEntries() []keelclient.LogEntry {
	e.t.Helper()
	c, cancel := ctx()
	defer cancel()
	l, err := e.cl.Logs(c, 0)
	if err != nil {
		e.t.Fatal(err)
	}
	return l
}

// noSecretDrawn fails when the window drew one of the scratch secrets.
func (e *win) noSecretDrawn(secrets ...string) {
	e.t.Helper()
	all := e.texts()
	for _, s := range secrets {
		if strings.Contains(all, s) {
			e.t.Fatal("the window drew a scratch secret")
		}
	}
}

// createVault types the scratch passphrase twice on step 1 and creates the vault; it waits for step 2.
func (e *win) createVault() {
	e.t.Helper()
	e.pump(func() bool { return e.tt.HasText("Welcome to Keel") && e.tt.Focused("Passphrase") }, "First run step 1 with the passphrase field focused")
	e.fillField("Passphrase", e2eFirstPass)
	e.fillField("Repeat passphrase", e2eFirstPass)
	e.click("Create vault")
	e.pump(func() bool { return e.w.first != nil && e.w.first.step == stepHosts && e.w.first.preview != nil }, "step 2 with the ssh config read")
}

func TestE2EFirstRun(t *testing.T) {
	addr, _, dir := startFirstRunServe(t)
	e := newWin(t, addr, dir, nil)
	e.pump(func() bool { return e.tt.HasText("Welcome to Keel") }, "First run step 1")
	if v := e.vault(); !v.NeedsSetup || !v.Locked {
		t.Fatalf("the daemon does not need setup: %+v", v)
	}
	if e.tt.HasText("Filter hosts") {
		t.Fatal("the workspace shows under First run")
	}
	for _, s := range []string{"Vault", "Hosts", "Agent", "Passphrase", "Repeat passphrase", "I already have a keel.vault…", "Create vault",
		"Keel was started with KEEL_KEYRING=off, so it does not use the keychain."} {
		if !e.tt.HasText(s) {
			t.Errorf("step 1 has no %q", s)
		}
	}
	e.settle(300 * time.Millisecond)
	e.shot("TestE2EFirstRun-vault")

	// A mismatch says so and sends nothing; the same passphrase twice creates the vault.
	e.pump(func() bool { return e.tt.Focused("Passphrase") }, "the passphrase field to have the keyboard")
	e.fillField("Passphrase", e2eFirstPass)
	e.fillField("Repeat passphrase", e2eFirstPass+"-different")
	e.click("Create vault")
	e.pump(func() bool { return e.tt.HasText("The passphrases don't match.") }, "the mismatch message")
	if !e.vault().NeedsSetup {
		t.Fatal("a mismatch created the vault")
	}
	e.fillField("Repeat passphrase", e2eFirstPass)
	e.click("Create vault")
	e.pump(func() bool { return e.w.first != nil && e.w.first.step == stepHosts && e.w.first.preview != nil }, "step 2 with the ssh config read")
	if v := e.vault(); v.Locked || v.NeedsSetup || !v.FirstRun {
		t.Fatalf("after Create vault: %+v", v)
	}
	if e.w.first.pass != "" || e.w.first.pass2 != "" || strings.Contains(fmt.Sprintf("%+v", e.w.first), e2eFirstPass) {
		t.Error("the passphrase is held by the screen")
	}
	e.noSecretDrawn(e2eFirstPass)

	// Step 2 lists the fixture's hosts, patterns apart, and keeps a ProxyCommand host off.
	for _, s := range []string{"Bring your hosts", "e2e-web", "e2e-api", "e2e-db", "e2e-legacy", "e2e-jump", "ProxyCommand, not used"} {
		if !e.tt.HasText(s) {
			t.Errorf("step 2 has no %q", s)
		}
	}
	if !e.tt.HasText("Keel found " + e.w.first.sshConfig() + " with 5 hosts. Pick the ones to add. Your config file is not changed.") {
		t.Errorf("the lead is %q", e.w.first.lead)
	}
	if got := sortedHosts(e.w.first.sel); !slices.Equal(got, []string{"e2e-api", "e2e-db", "e2e-web"}) {
		t.Fatalf("ticked at first: %v", got)
	}
	e.settle(300 * time.Millisecond)
	e.shot("TestE2EFirstRun-hosts")

	// Tick two (untick e2e-db), leave every key's copy off, add.
	e.click("Add e2e-db")
	e.pump(func() bool { return e.tt.HasText("2 of 5 selected") && e.hasLabel("Add 2 hosts") }, "two hosts ticked")
	before := len(e.identities())
	e.click("Add 2 hosts")
	e.pump(func() bool { return e.w.first != nil && e.w.first.step == stepAgent }, "step 3")
	want := []string{"Local Shell", "e2e-api", "e2e-web"}
	if got := e.hostNames(); !slices.Equal(got, want) {
		t.Fatalf("hosts %v, want %v", got, want)
	}
	if got := len(e.identities()); got != before {
		t.Fatalf("identities %d → %d: a key was copied with its switch off", before, got)
	}
	if !e.tt.HasText("Added 2 hosts") {
		t.Error("the result text is not shown")
	}
	e.settle(300 * time.Millisecond)
	e.shot("TestE2EFirstRun-agent")

	// Choose Google Gemini and type a made-up key: nothing is saved until Open Keel, and the key is never drawn.
	e.click("Google Gemini")
	e.pump(func() bool { return e.hasLabel("Gemini API key input") }, "the key field")
	e.tt.Type(fakeAgentKey)
	e.step()
	if e.tt.HasText(fakeAgentKey) {
		t.Fatal("the key is drawn in the clear")
	}
	if code, body := e.raw("GET", "/api/agent/settings", nil); code != 200 || strings.Contains(body, `"configured":true`) {
		t.Fatalf("a key was saved before Open Keel: %d", code)
	}
	e.shot("TestE2EFirstRun-agent-gemini")

	// Open Keel ends the first run and opens the workspace.
	if v := e.vault(); !v.FirstRun {
		t.Fatal("the first run ended before Open Keel")
	}
	e.click("Open Keel")
	e.pump(func() bool { return e.w.first == nil }, "Open Keel to end the first run")
	if code, body := e.raw("GET", "/api/agent/settings", nil); code != 200 || !strings.Contains(body, `"configured":true`) || !strings.Contains(body, `"suggestions":"on"`) {
		t.Fatalf("the key and the switch were not saved: %d", code)
	}
	if e.w.first != nil || strings.Contains(e.texts(), fakeAgentKey) {
		t.Fatal("the key is on screen")
	}
	e.pump(func() bool { return e.w.first == nil && e.hasLabel("host e2e-web") && e.tt.HasText("Filter hosts") }, "the workspace with the imported hosts")
	if v := e.vault(); v.FirstRun || v.Locked || v.NeedsSetup {
		t.Fatalf("after Open Keel: %+v", v)
	}
	// A second window on the same daemon opens straight to the workspace.
	e2 := newWin(t, addr, dir, nil)
	e2.pump(func() bool { return e2.hasLabel("host e2e-api") && e2.tt.HasText("Filter hosts") }, "the second window's workspace")
	if e2.w.first != nil || e2.tt.HasText("Welcome to Keel") {
		t.Fatal("the second window shows First run")
	}
	e2.noSecretDrawn(e2eFirstPass)
	e.noSecretDrawn(e2eFirstPass)
}

func TestE2EFirstRunSkipAndKeyCopy(t *testing.T) {
	addr, _, dir := startFirstRunServe(t)
	e := newWinH(t, addr, dir, nil, 1500)
	e.createVault()
	// Skip adds nothing.
	e.click("Skip, add hosts later")
	e.pump(func() bool { return e.w.first != nil && e.w.first.step == stepAgent }, "step 3")
	if got := e.hostNames(); !slices.Equal(got, []string{"Local Shell"}) {
		t.Fatalf("Skip added hosts: %v", got)
	}
	e.click("Set up later")
	e.pump(func() bool { return e.w.first == nil && e.tt.HasText("Filter hosts") }, "the workspace")
	if v := e.vault(); v.FirstRun {
		t.Fatal("Set up later left the first run open")
	}

	// The import comes back from the palette ("Import hosts from ~/.ssh/config").
	e.key(ui.Cmd, ui.KeyK)
	e.pump(func() bool { return e.w.pal != nil }, "the palette")
	e.tt.Type("Import hosts")
	e.step()
	e.key(0, ui.KeyEnter)
	e.pump(func() bool { return e.w.first != nil && e.w.first.flow == flowImport && e.w.first.preview != nil }, "the import's table")
	for _, s := range []string{"Import hosts", "Cancel", "e2e-web", "e2e-db"} {
		if !e.tt.HasText(s) {
			t.Errorf("the import shows no %q", s)
		}
	}
	if e.hasLabel("Setup progress") || e.hasLabel("Back") {
		t.Error("First run's step row or Back shows in the palette's import")
	}
	// Only e2e-web, with the plain key copied.
	e.click("Select all hosts")
	e.click("Select all hosts")
	if n := e.w.first.addCount(); n != 0 {
		t.Fatalf("%d hosts still ticked", n)
	}
	e.click("Add e2e-web")
	e.click("Copy ~/.ssh/" + fixturePlain + " into the vault")
	if !anyKeyOn(e.w.first) {
		t.Fatal("the key's switch did not turn on")
	}
	e.shot("TestE2EFirstRunSkipAndKeyCopy")
	e.click("Add 1 host")
	e.pump(func() bool { return e.w.first == nil && e.hasLabel("host e2e-web") }, "the imported host in the tree")
	if got := e.hostNames(); !slices.Equal(got, []string{"Local Shell", "e2e-web"}) {
		t.Fatalf("hosts %v", got)
	}
	ids := e.identities()
	if len(ids) != 1 || ids[0].Kind != "key" || !strings.Contains(ids[0].Name, fixturePlain) || !ids[0].HasPrivateKey {
		t.Fatalf("identities %+v", ids)
	}
	h, _ := e.hostNamed("e2e-web")
	if h.AuthType != "identity" || h.IdentityID != ids[0].ID {
		t.Fatalf("the host does not use the copied key: %+v", h)
	}
	if !e.tt.HasText("Added 1 host and 1 key") {
		t.Error("the result text is not in the status bar")
	}
	// Nothing of the key is on the page, and the API returns none either.
	_, body := e.raw("GET", "/api/identities", nil)
	if strings.Contains(body, "BEGIN OPENSSH") {
		t.Fatal("GET /api/identities returns a private key")
	}
	e.noSecretDrawn(e2eFirstPass, e2eKeyPass)
}

func anyKeyOn(f *firstRun) bool {
	for _, k := range f.sel.keys {
		if k.on {
			return true
		}
	}
	return false
}

func TestE2EKeychainPage(t *testing.T) {
	addr, dir := startServe(t, true)
	f := startSSHOnly(t, dir)
	e := newWin(t, addr, dir, nil)
	e.pump(e.onStartPage, "the New tab page")
	e.click("Keychain") // the sidebar footer
	e.pump(func() bool { return e.w.activeTab().pg != nil && e.tt.HasText("No identities yet") }, "the Keychain page")
	e.click("New identity")
	e.pump(func() bool { return e.hasLabel("dialog New identity") && e.tt.Focused("Identity name") }, "the New identity dialog")
	e.fillField("Identity name", "e2e-password")
	e.fillField("Identity username", f.user)
	e.fillField("Identity password", f.password)
	e.noSecretDrawn(f.password)
	e.click("Save identity")
	e.pump(func() bool { return e.w.identDlg == nil && e.hasLabel("identity e2e-password") }, "the new identity in the list")
	if !e.tt.HasText("Passwords") || !e.tt.HasText("Password · "+f.user+" · 0 hosts") {
		t.Error("the identity is not listed under Passwords with its subtitle")
	}
	ids := e.identities()
	if len(ids) != 1 || ids[0].Name != "e2e-password" || ids[0].Kind != "password" || !ids[0].HasPassword {
		t.Fatalf("identities %+v", ids)
	}
	if _, body := e.raw("GET", "/api/identities", nil); strings.Contains(body, f.password) {
		t.Fatal("GET /api/identities returns the password")
	}
	e.noSecretDrawn(f.password)

	// A host that signs in with it, made in the host dialog on the New tab page, connects to the SSH fixture.
	e.click("tab New Tab")
	e.pump(e.onStartPage, "the New tab page")
	e.click("New host")
	e.pump(func() bool { return e.hasLabel("dialog New host") }, "the New host dialog")
	e.fillField("Label", "e2e-ident-host")
	e.fillField("Hostname / IP", f.host)
	e.fillField("Port", fmt.Sprint(f.port))
	e.fillField("Username", f.user)
	e.click("Identity") // the Authentication control
	e.pump(func() bool { return e.w.hostDlg.f.auth == "identity" && e.w.hostDlg.f.identityID == ids[0].ID }, "the identity to be picked")
	e.click("Save host")
	e.pump(func() bool { return e.w.hostDlg == nil && e.hasLabel("host e2e-ident-host") }, "the saved host")
	h, ok := e.hostNamed("e2e-ident-host")
	if !ok || h.AuthType != "identity" || h.IdentityID != ids[0].ID {
		t.Fatalf("host %+v", h)
	}
	e.click("host e2e-ident-host")
	e.pump(func() bool { return e.hasLabel("dialog Unknown host key") }, "the unknown host key dialog")
	e.click("Trust and connect")
	e.pump(func() bool { return e.snap().Status == "ready" }, "the session to be ready")

	// Back on the page: "Used by 1 host", and Delete does nothing while a host uses it.
	e.click("tab Keychain")
	e.pump(func() bool { return e.tt.HasText("Used by 1 host") && e.tt.HasText("e2e-ident-host") }, "the detail to say Used by 1 host")
	if !e.tt.HasText("Password · "+f.user+" · 1 host") || !e.tt.HasText("Remove it from 1 host before you can delete it.") {
		t.Error("the list line or the note does not say the host uses it")
	}
	e.click("Delete identity")
	e.settle(300 * time.Millisecond)
	if e.w.confirmOpen() || len(e.identities()) != 1 {
		t.Fatal("an in-use identity was asked about or deleted")
	}
	e.noSecretDrawn(f.password)
	e.shot("TestE2EKeychainPage-detail")

	// Delete the host (its menu on the New tab's sidebar), then the identity, with the confirms.
	if err := e.tt.RightClick("host e2e-ident-host"); err != nil {
		t.Fatal(err)
	}
	e.step()
	if err := e.tt.ChooseMenuItem("Delete"); err != nil {
		t.Fatal(err)
	}
	e.pump(func() bool { return e.tt.HasText("Remove e2e-ident-host?") }, "the host's confirm")
	e.click("Remove")
	e.pump(func() bool { _, ok := e.hostNamed("e2e-ident-host"); return !ok }, "the host to go")
	e.pump(func() bool { return e.tt.HasText("Used by 0 hosts") }, "the detail to say Used by 0 hosts")
	e.click("Delete identity")
	e.pump(func() bool { return e.tt.HasText("Delete identity “e2e-password”?") }, "the identity's confirm")
	if len(e.identities()) != 1 {
		t.Fatal("deleted before the confirm")
	}
	e.click("Remove")
	e.pump(func() bool { return len(e.identities()) == 0 && !e.hasLabel("identity e2e-password") }, "the identity gone")
	e.pump(func() bool { return e.tt.HasText("No identities yet") }, "the empty state")
	e.noSecretDrawn(f.password)
}

func TestE2ELogsPage(t *testing.T) {
	addr, dir := startServe(t, true)
	f := startSSH(t, addr, dir)
	e := newWin(t, addr, dir, nil)
	// Three things that happen: a session, a trusted host key, a deleted host. keel serve logs the first two and the
	// host's creation, but not a host's deletion (httpapi writes no entry for it), so no row names the delete.
	e.openLocal()
	e.click("host e2e-ssh")
	e.pump(func() bool { return e.hasLabel("dialog Unknown host key") }, "the unknown host key dialog")
	e.click("Trust and connect")
	e.pump(func() bool { return e.snap().Status == "ready" }, "the ssh session")
	if code, _ := e.raw("POST", "/api/hosts", map[string]any{"name": "e2e-temp", "address": "10.9.9.9", "port": 22, "username": "u", "authType": "password", "password": "x"}); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("create: %d", code)
	}
	e.pump(func() bool { return e.w.paneCount() == 2 }, "both sessions")
	e.w.reloadHosts()
	e.pump(func() bool { return e.hasLabel("host e2e-temp") }, "the temporary host")
	if err := e.tt.RightClick("host e2e-temp"); err != nil {
		t.Fatal(err)
	}
	e.step()
	if err := e.tt.ChooseMenuItem("Delete"); err != nil {
		t.Fatal(err)
	}
	e.pump(func() bool { return e.tt.HasText("Remove e2e-temp?") }, "the confirm")
	e.click("Remove")
	e.pump(func() bool { _, ok := e.hostNamed("e2e-temp"); return !ok }, "the host deleted")

	// What the server logged, newest first: the row texts the page must show.
	var trusted, sessionMsg, created string
	for _, en := range e.logEntries() {
		switch {
		case trusted == "" && en.Kind == "host" && strings.Contains(en.Message, "host key for"):
			trusted = en.Message
		case sessionMsg == "" && en.Kind == "session":
			sessionMsg = en.Message
		case created == "" && en.Kind == "host" && en.Message == "host created: e2e-temp":
			created = en.Message
		}
	}
	if trusted == "" || sessionMsg == "" || created == "" {
		t.Fatalf("the server logged no host key trust (%q), session (%q) or host creation (%q)", trusted, sessionMsg, created)
	}

	// Logs from the palette.
	e.key(ui.Cmd, ui.KeyK)
	e.pump(func() bool { return e.w.pal != nil }, "the palette")
	e.tt.Type("Logs")
	e.step()
	e.key(0, ui.KeyEnter)
	e.pump(func() bool {
		return e.w.activeTab().pg != nil && pageAs[*logsPage](e.w.activeTab()) != nil && pageAs[*logsPage](e.w.activeTab()).loaded
	}, "the Logs page")
	for _, s := range []string{"Logs", "Refresh", "All", "Sessions", "Forwards", "Vault", "Agent", "Time", "Kind", "Target", "Event"} {
		if !e.tt.HasText(s) {
			t.Errorf("no %q", s)
		}
	}
	if !e.tt.HasText(trusted) || !e.tt.HasText(sessionMsg) || !e.tt.HasText(created) {
		t.Fatalf("the page does not show the trust (%q), session (%q) and creation (%q) rows", trusted, sessionMsg, created)
	}
	e.noSecretDrawn(f.password)
	// Newest first: the log's own order on screen.
	p := pageAs[*logsPage](e.w.activeTab())
	rows := p.rows()
	all := e.logEntries()
	if len(rows) == 0 || rows[0].Message != all[0].Message || rows[0].Time.Before(rows[len(rows)-1].Time) {
		t.Errorf("the page is not newest first")
	}
	// The Vault chip hides the session row and keeps the host rows.
	e.click("Vault")
	e.pump(func() bool { return !e.tt.HasText(sessionMsg) && e.tt.HasText(trusted) }, "the Vault chip to hide the session row")
	e.click("Sessions")
	e.pump(func() bool { return e.tt.HasText(sessionMsg) && !e.tt.HasText(trusted) }, "the Sessions chip to hide the host rows")
	e.click("Vault")
	e.pump(func() bool { return !e.tt.HasText(sessionMsg) && e.tt.HasText(trusted) }, "the Vault chip again")
	e.settle(300 * time.Millisecond)
	e.shot("TestE2ELogsPage")
}

func TestE2EPagesComeBack(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	sid := e.openLocal()
	e.click("Keychain")
	e.pump(func() bool { return e.w.pageTab("keychain") != nil && e.w.activeTab().name() == "Keychain" }, "the Keychain tab")
	e.click("Logs")
	e.pump(func() bool { return e.w.pageTab("logs") != nil && e.w.activeTab().name() == "Logs" }, "the Logs tab")
	if got := tabTitles(&rig{w: e.w}); !slices.Equal(got, []string{"Local Shell", "Keychain", "Logs"}) {
		t.Fatalf("tabs %v", got)
	}
	e.settle(600 * time.Millisecond)
	e.w.closeWindow()
	if b, err := os.ReadFile(filepath.Join(dir, savedFile)); err != nil || !strings.Contains(string(b), `"pages":["keychain","logs"]`) {
		t.Fatalf("the saved workspace: %v %s", err, b)
	}

	e2 := newWin(t, addr, dir, nil)
	e2.pump(func() bool { return e2.w.tabCount() == 3 && e2.tt.HasText("Keychain") }, "the new window's tabs")
	if got := tabTitles(&rig{w: e2.w}); !slices.Equal(got, []string{"Local Shell", "Keychain", "Logs"}) {
		t.Fatalf("restored tabs %v", got)
	}
	if e2.w.activeTab().name() != "Logs" {
		t.Errorf("the page that was shown is not shown again: %s", e2.w.activeTab().name())
	}
	if e2.sidOf(0) != sid {
		t.Errorf("the session tab came back as %q, want %q", e2.sidOf(0), sid)
	}
	e2.pump(func() bool {
		return pageAs[*logsPage](e2.w.pageTab("logs")).loaded && pageAs[*keychainPage](e2.w.pageTab("keychain")).loaded
	}, "both pages to read the server")
	e2.click("tab Keychain")
	e2.pump(func() bool { return e2.tt.HasText("No identities yet") }, "the Keychain page")
	e2.shot("TestE2EPagesComeBack")
}
