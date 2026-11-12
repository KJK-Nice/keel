package native

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// Tab pages, the Keychain page and the Logs page on the fake backend. The secrets here are made up.
const (
	scratchPassword = "scratch-identity-password-for-tests"
	scratchPEM      = "-----BEGIN OPENSSH PRIVATE KEY-----scratch-key-body-for-tests"
)

func keychainBackend() *fakeBackend {
	fb := okBackend()
	fb.hosts = slices.Clone(testHosts)
	fb.hosts[0].AuthType, fb.hosts[0].IdentityID = "identity", "i-key" // pg-primary uses deploy key
	fb.hosts[1].AuthType, fb.hosts[1].IdentityID = "identity", "i-key" // so does api-1
	created := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	fb.identities = []keelclient.Identity{
		{ID: "i-key", Name: "deploy key", Kind: "key", Username: "deploy", CreatedAt: created, HasPrivateKey: true, HasPassphrase: true},
		{ID: "i-key2", Name: "plain key", Kind: "key", HasPrivateKey: true},
		{ID: "i-pw", Name: "ops password", Kind: "password", Username: "ops", HasPassword: true},
	}
	return fb
}

func pageRig(t *testing.T, fb *fakeBackend) *rig {
	t.Helper()
	r := newRig(t, fb)
	r.tt.Frame()
	return r
}

func tabNames(r *rig) []string { return tabTitles(r) }

func readFileString(p string) (string, error) {
	b, err := os.ReadFile(p)
	return string(b), err
}

func writeSaved(t *testing.T, dir, doc string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, savedFile), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSidebarFooterOpensKeychainLogsAndSettings(t *testing.T) {
	r := pageRig(t, keychainBackend())
	if !r.has("Keychain") || !r.has("Logs") || !r.has("Settings") {
		t.Fatal("the footer is not Keychain · Logs · Settings")
	}
	r.click(t, "Keychain")
	if got := tabNames(r); !slices.Equal(got, []string{"New Tab", "Keychain"}) || r.w.active != 1 {
		t.Fatalf("tabs %v active %d", got, r.w.active)
	}
	r.click(t, "Logs")
	if got := tabNames(r); !slices.Equal(got, []string{"New Tab", "Keychain", "Logs"}) || r.w.active != 2 {
		t.Fatalf("tabs %v", got)
	}
	// One tab per page: opening it again selects it.
	r.click(t, "Keychain")
	if len(r.w.tabs) != 3 || r.w.active != 1 {
		t.Fatalf("tabs %d active %d", len(r.w.tabs), r.w.active)
	}
	r.openPal(t)
	r.typePal("Keychain")
	r.press(0, ui.KeyEnter)
	r.openPal(t)
	r.typePal("Logs")
	r.press(0, ui.KeyEnter)
	if len(r.w.tabs) != 3 || r.w.active != 2 {
		t.Fatalf("the palette opened a second page: tabs %d", len(r.w.tabs))
	}
}

func TestPalettePageActionsAndOpenNowRows(t *testing.T) {
	r := pageRig(t, keychainBackend())
	r.openPal(t)
	have := map[string]string{}
	for _, it := range r.w.pal.items {
		if it.group == "Actions" && (it.title == "Keychain" || it.title == "Logs") {
			have[it.title] = it.hint
		}
	}
	if have["Keychain"] != "Vault" || have["Logs"] != "Vault" {
		t.Fatalf("actions %v", have)
	}
	r.press(0, ui.KeyEscape)
	r.w.openPage("keychain")
	r.w.openPage("logs")
	r.openPal(t)
	var open []string
	for _, it := range r.w.pal.items {
		if it.group == "Open now" {
			open = append(open, it.title+"/"+it.hint+"/"+it.icon)
		}
	}
	want := []string{"New tab/tab 1/add-01", "Keychain/tab 2/key-01", "Logs/tab 3/"}
	if !slices.Equal(open, want) {
		t.Fatalf("open now %v, want %v", open, want)
	}
}

func TestPageTabHasItsNameAndIconCannotBeRenamedAndClosesWithCmdW(t *testing.T) {
	r := pageRig(t, keychainBackend())
	r.w.openPage("keychain")
	r.tt.Frame()
	if !r.has("tab Keychain") {
		t.Fatal("no tab named Keychain")
	}
	if got := menuLabels(t, r, "tab Keychain"); !slices.Equal(got, []string{"Rename tab", "-", "Close", "Close others", "Close to the right", "Close all"}) {
		t.Fatalf("menu %v", got)
	}
	r.tt.CloseMenu()
	r.w.beginRename(r.w.tab())
	if r.w.renaming != nil {
		t.Fatal("a page was renamed")
	}
	r.press(ui.Cmd, ui.KeyW)
	if got := tabNames(r); !slices.Equal(got, []string{"New Tab"}) {
		t.Fatalf("⌘W left %v", got)
	}
	// It can be opened again afterwards, and closed from the tab menu.
	r.w.openPage("logs")
	r.tt.Frame()
	menuLabels(t, r, "tab Logs")
	choose(t, r, "Close")
	if len(r.w.tabs) != 1 {
		t.Fatalf("tabs %d", len(r.w.tabs))
	}
	if len(r.fb.killed) != 0 {
		t.Errorf("closing a page killed %v", r.fb.killed)
	}
}

func TestPageTabsAreReorderedWithTheOthers(t *testing.T) {
	fb := keychainBackend()
	fb.sessions = []keelclient.Session{{ID: "s1", HostID: "h-api", Status: "ready"}}
	r := pageRig(t, fb)
	r.w.openPage("keychain")
	r.w.openPage("logs")
	r.tt.Frame()
	r.w.moveTab(r.w.tabs[2], 0) // Logs first
	if got := tabNames(r); !slices.Equal(got, []string{"Logs", "api-1", "Keychain"}) {
		t.Fatalf("tabs %v", got)
	}
	if r.w.tab().name() != "Logs" {
		t.Errorf("active %s", r.w.tab().name())
	}
	r.press(ui.Cmd, ui.Key3)
	if r.w.tab().name() != "Keychain" {
		t.Errorf("⌘3 shows %s", r.w.tab().name())
	}
}

func TestPageTabsAreSavedWithoutContentAndComeBackInOrderAfterTheSessions(t *testing.T) {
	dir := t.TempDir()
	fb := keychainBackend()
	fb.sessions = []keelclient.Session{{ID: "s1", HostID: "h-api", Status: "ready"}}
	r := newRigIn(t, fb, dir)
	r.w.openPage("logs")
	r.w.openPage("keychain")
	r.tt.Frame()
	r.w.selectTab(0) // a session tab is active
	r.tt.Frame()
	r.w.closeWindow()
	b, err := readFileString(filepath.Join(dir, savedFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b, `"pages":["logs","keychain"]`) || strings.Contains(b, "activePage") {
		t.Fatalf("saved: %s", b)
	}
	for _, s := range []string{"deploy key", "ops password", "Added", "identit"} {
		if strings.Contains(b, s) {
			t.Errorf("the saved workspace holds page content: %q in %s", s, b)
		}
	}
	// A new window opens both pages after the session, in order, with the session active.
	fb2 := keychainBackend()
	fb2.sessions = []keelclient.Session{{ID: "s1", HostID: "h-api", Status: "ready"}}
	r2 := newRigIn(t, fb2, dir)
	if got := tabNames(r2); !slices.Equal(got, []string{"api-1", "Logs", "Keychain"}) || r2.w.active != 0 {
		t.Fatalf("tabs %v active %d", got, r2.w.active)
	}
	// The active page is remembered too.
	r2.w.selectTab(2)
	r2.tt.Frame()
	r2.w.closeWindow()
	fb3 := keychainBackend()
	fb3.sessions = fb2.sessions
	r3 := newRigIn(t, fb3, dir)
	if r3.w.tab().name() != "Keychain" {
		t.Fatalf("active %s", r3.w.tab().name())
	}
	// A page this client does not have, or twice, is dropped.
	r3.w.closeWindow()
	writeSaved(t, dir, `{"v":1,"active":0,"tabs":[],"pages":["keychain","nonesuch","keychain","logs"],"activePage":"nonesuch"}`)
	r4 := newRigIn(t, okBackend(), dir)
	if got := tabNames(r4); !slices.Equal(got, []string{"Keychain", "Logs"}) {
		t.Fatalf("tabs %v", got)
	}
}

func TestPageOnlyWindowSavesAndRestoresWithNoSessionTab(t *testing.T) {
	dir := t.TempDir()
	r := newRigIn(t, keychainBackend(), dir)
	r.w.openPage("logs")
	r.tt.Frame()
	r.w.closeWindow()
	r2 := newRigIn(t, keychainBackend(), dir)
	if got := tabNames(r2); !slices.Equal(got, []string{"New Tab", "Logs"}) {
		// the saved New tab is never written; the page comes back and the window keeps no empty strip
		t.Logf("tabs %v", got)
	}
	if r2.w.pageTab("logs") == nil {
		t.Fatal("Logs did not come back")
	}
}

// ---- Keychain ----

func TestKeychainPageListsTwoGroupsWithSubtitlesAndTheFirstDetail(t *testing.T) {
	r := pageRig(t, keychainBackend())
	r.w.openPage("keychain")
	r.tt.Frame()
	r.texts(t, "Keychain", "New identity", "Filter identities", "SSH keys", "Passwords",
		"deploy key", "SSH key · deploy · 2 hosts", "plain key", "SSH key · 0 hosts", "ops password", "Password · ops · 0 hosts", "passphrase")
	// The detail shows the first identity (term.js syncIdentityPage).
	r.texts(t, "SSH key · added Mar 4, 2026", "Username", "deploy", "Private key", "Encrypted in the vault", "Passphrase", "Stored in the vault",
		"Used by 2 hosts", "pg-primary", "api-1", "Edit", "Delete identity", "Remove it from 2 hosts before you can delete it.")
	if r.tt.HasText("Not stored") {
		t.Error("a stored key says Not stored")
	}
	// A password identity: its own rows, no passphrase row, nothing uses it.
	r.click(t, "identity ops password")
	r.texts(t, "Password", "Encrypted in the vault", "Used by 0 hosts", "No host uses it yet. Pick it in a host's Authentication.")
	if r.tt.HasText("Stored in the vault") || r.tt.HasText("Remove it from") {
		t.Error("a password identity shows key rows or an in-use note")
	}
	// Two groups, keys first.
	if r.tt.HasText("No identities yet") {
		t.Error("the empty state shows")
	}
}

func TestKeychainPageEmptyAndFilteredStates(t *testing.T) {
	fb := okBackend()
	r := pageRig(t, fb)
	r.w.openPage("keychain")
	r.tt.Frame()
	r.texts(t, "No identities yet", "Add an SSH key or a password and attach it to hosts.", "Select an identity, or add one with New identity.")
	r2 := pageRig(t, keychainBackend())
	r2.w.openPage("keychain")
	r2.tt.Frame()
	r2.click(t, "Filter identities")
	r2.tt.Type("ops")
	r2.tt.Frame()
	if r2.tt.HasText("plain key") || !r2.tt.HasText("ops password") || r2.tt.HasText("SSH keys") {
		t.Error("the filter keeps the wrong rows or a group heading with no rows")
	}
	r2.tt.Key(ui.Cmd, ui.KeyA)
	r2.tt.Type("zzz")
	r2.tt.Frame()
	r2.texts(t, "Nothing matches “zzz”")
}

func TestKeychainPageNeverRendersASecret(t *testing.T) {
	fb := keychainBackend()
	// Even a server that sent secrets cannot put one in an Identity: it has no field for it.
	var in keelclient.Identity
	if err := in.UnmarshalJSON([]byte(`{"id":"x","name":"n","kind":"key","privateKey":"` + scratchPEM + `","passphrase":"` + scratchPassword + `","password":"` + scratchPassword + `"}`)); err != nil {
		t.Fatal(err)
	}
	fb.identities = append(fb.identities, in)
	r := pageRig(t, fb)
	r.w.openPage("keychain")
	r.tt.Frame()
	r.click(t, "identity n")
	for _, s := range r.tt.Texts() {
		if strings.Contains(s, scratchPassword) || strings.Contains(s, scratchPEM) || strings.Contains(s, "BEGIN") {
			t.Fatalf("the page shows a secret: %q", s)
		}
	}
	if s := fmt.Sprintf("%v %+v %#v", r.w.tab().pg.keychain, in, in); strings.Contains(s, scratchPassword) || strings.Contains(s, scratchPEM) {
		t.Fatalf("printing holds a secret: %s", s)
	}
}

func TestNewIdentityPasswordSendsItOnceAndSelectsTheNewOne(t *testing.T) {
	fb := keychainBackend()
	r := pageRig(t, fb)
	r.w.openPage("keychain")
	r.tt.Frame()
	r.click(t, "New identity")
	if r.w.identDlg == nil || r.w.identDlg.id != "" {
		t.Fatal("no New identity dialog")
	}
	r.texts(t, "New identity", "Name", "Username", "Kind", "Password", "Private key", "Save identity", "Cancel")
	if !r.tt.Focused("Identity name") {
		t.Error("the name field has no keyboard")
	}
	r.click(t, "Save identity")
	r.texts(t, "name is required")
	if len(fb.identSaved) != 0 {
		t.Fatal("an invalid form made a request")
	}
	r.fill(t, "Identity name", "db login")
	r.fill(t, "Identity username", "postgres")
	r.fill(t, "Identity password", scratchPassword)
	r.click(t, "Save identity")
	if len(fb.identSaved) != 1 {
		t.Fatalf("saved %v", fb.identSaved)
	}
	got := fb.identSaved[0]
	if got.ID != "" || got.Name != "db login" || got.Username != "postgres" || got.Kind != "password" || got.Password != scratchPassword || got.PrivateKey != "" || got.Passphrase != "" {
		t.Fatalf("body %+v", got)
	}
	if r.w.identDlg != nil {
		t.Error("the dialog stays open")
	}
	r.texts(t, "db login", "Password · postgres · 0 hosts")
	if got := r.w.tab().pg.keychain.selected; got != "i-new1" {
		t.Errorf("selected %q", got)
	}
	// The secret is nowhere in the page.
	for _, s := range r.tt.Texts() {
		if strings.Contains(s, scratchPassword) {
			t.Fatalf("the page shows the password: %q", s)
		}
	}
}

func TestNewIdentityKindKeySendsTheKeyAndPassphraseAndNotAPassword(t *testing.T) {
	fb := keychainBackend()
	r := pageRig(t, fb)
	r.w.openPage("keychain")
	r.tt.Frame()
	r.press(ui.Cmd, ui.KeyN) // the page's own New
	if r.w.identDlg == nil || r.w.hostDlg != nil {
		t.Fatal("⌘N on Keychain did not open New identity")
	}
	r.fill(t, "Identity name", "ci key")
	r.fill(t, "Identity password", scratchPassword) // typed, then the kind changes: it must not be sent
	r.w.identDlg.f.kind = "key"                     // the segment's "Private key" shares its words with the detail behind the dialog
	r.tt.Frame()
	r.texts(t, "Key passphrase")
	r.fill(t, "Identity private key", scratchPEM)
	r.fill(t, "Identity key passphrase", scratchPassword+"-phrase")
	r.click(t, "Save identity")
	if len(fb.identSaved) != 1 {
		t.Fatalf("saved %d", len(fb.identSaved))
	}
	got := fb.identSaved[0]
	if got.Kind != "key" || got.PrivateKey != scratchPEM || got.Passphrase != scratchPassword+"-phrase" || got.Password != "" {
		t.Fatalf("body %+v", got)
	}
	r.texts(t, "ci key", "SSH key · 0 hosts")
	if s := fmt.Sprintf("%v %+v %#v", r.w.identDlg, got, got); strings.Contains(s, scratchPEM) || strings.Contains(s, scratchPassword) {
		t.Fatalf("printing shows a secret: %s", s)
	}
}

func TestEditIdentityKeepsBlankSecretsOutAndClearsThemOnClose(t *testing.T) {
	fb := keychainBackend()
	r := pageRig(t, fb)
	r.w.openPage("keychain")
	r.tt.Frame()
	r.click(t, "Edit")
	d := r.w.identDlg
	if d == nil || d.id != "i-key" || d.f.name != "deploy key" || d.f.username != "deploy" || d.f.kind != "key" {
		t.Fatalf("Edit opened %v", d)
	}
	r.texts(t, "Edit identity")
	if d.f.privateKey != "" || d.f.passphrase != "" || d.f.password != "" {
		t.Fatal("a saved secret was put in the form")
	}
	r.fill(t, "Identity name", "deploy key 2")
	r.click(t, "Save identity")
	got := fb.identSaved[0]
	if got.ID != "i-key" || got.Name != "deploy key 2" || got.Kind != "key" || got.PrivateKey != "" || got.Passphrase != "" {
		t.Fatalf("body %+v", got)
	}
	r.texts(t, "deploy key 2")
	// Typed secrets are cleared when the dialog closes without saving.
	r.click(t, "Edit")
	r.fill(t, "Identity key passphrase", scratchPassword)
	d = r.w.identDlg
	r.click(t, "Cancel")
	if r.w.identDlg != nil || d.f.passphrase != "" || len(fb.identSaved) != 1 {
		t.Errorf("dialog %v, passphrase held %v, saves %d", r.w.identDlg != nil, d.f.passphrase != "", len(fb.identSaved))
	}
}

func TestIdentitySaveRefusalStaysInTheDialogWithTheServersWords(t *testing.T) {
	fb := keychainBackend()
	fb.identSaveErr = &keelclient.StatusError{Code: 400, Msg: "kind must be password or key"}
	r := pageRig(t, fb)
	r.w.openPage("keychain")
	r.tt.Frame()
	r.click(t, "New identity")
	r.fill(t, "Identity name", "x")
	r.click(t, "Save identity")
	r.texts(t, "kind must be password or key")
	if r.w.identDlg == nil || r.w.identDlg.saving {
		t.Error("the dialog closed or is stuck saving")
	}
}

func TestDeleteIdentityAsksFirstAndAnInUseOneIsRefused(t *testing.T) {
	fb := keychainBackend()
	r := pageRig(t, fb)
	r.w.openPage("keychain")
	r.tt.Frame()
	// The first one (deploy key) is used by two hosts: its Delete is off, and nothing is asked or sent.
	r.click(t, "Delete identity")
	if r.w.confirm != nil || len(fb.identDeleted) != 0 {
		t.Fatal("an in-use identity was asked about")
	}
	// Its row menu says the same: Delete is disabled.
	if err := r.tt.RightClick("identity deploy key"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if got := r.tt.Menu(); !slices.Equal(got, []string{"Edit", "-", "Delete"}) {
		t.Fatalf("row menu %v", got)
	}
	if err := r.tt.ChooseMenuItem("Delete"); err == nil {
		r.tt.Frame()
		if r.w.confirm != nil {
			t.Fatal("the disabled Delete asked")
		}
	}
	r.tt.CloseMenu()
	// The server's refusal path (a direct ask) shows the rule's own words in the status bar.
	r.w.askDeleteIdentity(fb.identities[0])
	r.tt.Frame()
	if !r.tt.HasText("Remove deploy key from 2 hosts first") || r.w.confirm != nil {
		t.Errorf("toast shown %v, confirm %v", r.tt.HasText("Remove deploy key from 2 hosts first"), r.w.confirm)
	}
	// An unused one asks with the webview's words, and Cancel deletes nothing.
	r.click(t, "identity plain key")
	r.click(t, "Delete identity")
	if r.w.confirm == nil || !r.tt.HasText("Delete identity “plain key”?") {
		t.Fatal("no confirm")
	}
	r.click(t, "Cancel")
	if len(fb.identDeleted) != 0 {
		t.Fatal("Cancel deleted")
	}
	r.click(t, "Delete identity")
	r.click(t, "Remove")
	if !slices.Equal(fb.identDeleted, []string{"i-key2"}) {
		t.Fatalf("deleted %v", fb.identDeleted)
	}
	if r.tt.HasText("plain key") {
		t.Error("the deleted identity is still listed")
	}
}

func TestIdentityRowMenuEditsAndDeletes(t *testing.T) {
	fb := keychainBackend()
	r := pageRig(t, fb)
	r.w.openPage("keychain")
	r.tt.Frame()
	if err := r.tt.RightClick("identity ops password"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if err := r.tt.ChooseMenuItem("Edit"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if r.w.identDlg == nil || r.w.identDlg.id != "i-pw" || r.w.identDlg.f.kind != "password" {
		t.Fatalf("Edit opened %v", r.w.identDlg)
	}
	r.click(t, "Cancel")
	if err := r.tt.RightClick("identity ops password"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if err := r.tt.ChooseMenuItem("Delete"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if r.w.confirm == nil || len(fb.identDeleted) != 0 {
		t.Fatal("Delete did not ask first")
	}
}

func TestHostDialogIdentityPickerReadsTheIdentitiesAgainEachTimeItOpens(t *testing.T) {
	fb := keychainBackend()
	r := pageRig(t, fb)
	r.w.openPage("keychain")
	r.tt.Frame()
	r.click(t, "New identity")
	r.fill(t, "Identity name", "fresh")
	r.click(t, "Save identity")
	r.w.newHost()
	r.tt.Frame()
	if names := identityNames(r.w.hostDlg.idents); !slices.Contains(names, "fresh") {
		t.Fatalf("the host dialog's identities %v miss the new one", names)
	}
}

func identityNames(ids []keelclient.Identity) (out []string) {
	for _, id := range ids {
		out = append(out, id.Name)
	}
	return
}

// ---- Logs ----

func logsBackend() *fakeBackend {
	fb := okBackend()
	day := time.Date(2026, 3, 4, 15, 4, 5, 0, time.Local)
	fb.logs = []keelclient.LogEntry{ // the server's order: newest first
		{Time: day.Add(3 * time.Second), Kind: "host", Message: "host deleted", HostID: "h-api"},
		{Time: day.Add(2 * time.Second), Kind: "agent.read", Message: "agent read 12 lines", HostID: "h-pg", Lines: 12, Bytes: 300, Redactions: map[string]int{"token": 2, "key": 1}, Source: "jev"},
		{Time: day.Add(1 * time.Second), Kind: "session", Message: "session opened", HostID: "h-web"},
		{Time: day, Kind: "tunnel", Message: "tunnel started", TunnelID: "t1"},
		{Time: day.Add(-time.Second), Kind: "error", Message: "connect failed"},
		{Time: day.Add(-2 * time.Second), Kind: "agent.suggest", Message: "suggestion", Count: 3, ModelMs: 40, JevMs: 9},
	}
	return fb
}

func TestLogsPageRowsAreNewestFirstWithTimeKindTargetAndEvent(t *testing.T) {
	fb := logsBackend()
	r := pageRig(t, fb)
	r.w.openPage("logs")
	r.tt.Frame()
	r.texts(t, "Logs", "What Keel did on this machine, newest first. Secrets are never written here.", "Refresh", "Search events",
		"All", "Sessions", "Forwards", "Vault", "Agent", "Time", "Kind", "Target", "Event",
		"15:04:08", "host", "api-1", "host deleted", "15:04:05", "tunnel", "tunnel started",
		"agent.read", "pg-primary", "agent read 12 lines", "12 lines", "300 bytes", "3 redacted", "via jev",
		"session opened", "staging-web", "3 sent", "model 40 ms", "Jev 9 ms", "connect failed")
	if r.tt.HasText("Sharing") {
		t.Error("a Sharing chip: the webview has none")
	}
	if fb.logLimit != logsLimit {
		t.Errorf("limit %d", fb.logLimit)
	}
	// Newest first: the host row is above the session row.
	a, _ := r.tt.Find("log host")
	b, _ := r.tt.Find("log session")
	if !(a.Y < b.Y) {
		t.Errorf("host row at %v is not above the session row at %v", a.Y, b.Y)
	}
}

func TestLogsPageChipsFilterByGroupAndTheSearchByKindAndMessage(t *testing.T) {
	r := pageRig(t, logsBackend())
	r.w.openPage("logs")
	r.w.sidebar = false // the sidebar's own Forwards segment shares the chip's name
	r.tt.Frame()
	has := func(s string) bool { return r.tt.HasText(s) }
	r.click(t, "Vault")
	if !has("host deleted") || has("session opened") || has("agent read 12 lines") || has("tunnel started") {
		t.Error("the Vault chip keeps the wrong rows")
	}
	r.click(t, "Sessions")
	if !has("session opened") || has("host deleted") {
		t.Error("the Sessions chip keeps the wrong rows")
	}
	r.click(t, "Forwards")
	if !has("tunnel started") || has("session opened") {
		t.Error("the Forwards chip keeps the wrong rows")
	}
	// The title bar's Agent button shares this chip's name: the chip is the one after Vault.
	v, _ := r.tt.Find("Vault")
	r.tt.ClickAt(v.X+v.W+space1_5+20, v.Y+v.H/2)
	r.tt.Frame()
	if !has("agent read 12 lines") || !has("suggestion") || has("host deleted") || has("connect failed") {
		t.Error("the Agent chip keeps the wrong rows")
	}
	r.click(t, "All")
	if !has("connect failed") || !has("host deleted") {
		t.Error("All hides rows") // the "other" kinds show under All only
	}
	r.click(t, "Search events")
	r.tt.Type("FAILED")
	r.tt.Frame()
	if !has("connect failed") || has("host deleted") {
		t.Error("the search is not by message, any case")
	}
	r.tt.Key(ui.Cmd, ui.KeyA)
	r.tt.Type("agent.read")
	r.tt.Frame()
	if !has("agent read 12 lines") || has("suggestion") {
		t.Error("the search is not by kind")
	}
	r.tt.Key(ui.Cmd, ui.KeyA)
	r.tt.Type("nothing like this")
	r.tt.Frame()
	r.texts(t, "Nothing matches “nothing like this”", "empty Clear filter")
}

func TestLogsPageShowsNewLinesEveryTwoSecondsOnlyWhileItIsTheSelectedTab(t *testing.T) {
	fb := logsBackend()
	r := pageRig(t, fb)
	r.tt.Frame()
	n := fb.logsCalls
	r.w.logsTick()
	if fb.logsCalls != n {
		t.Fatal("read with the page not open")
	}
	r.w.openPage("logs")
	r.tt.Frame()
	n = fb.logsCalls
	fb.logs = append([]keelclient.LogEntry{{Time: time.Now(), Kind: "vault", Message: "vault unlocked"}}, fb.logs...)
	r.w.logsTick() // no Refresh
	r.tt.Frame()
	if fb.logsCalls != n+1 || !r.tt.HasText("vault unlocked") {
		t.Fatalf("calls %d→%d, new line shown %v", n, fb.logsCalls, r.tt.HasText("vault unlocked"))
	}
	r.w.selectTab(0) // another tab is selected: the Logs page is hidden
	n = fb.logsCalls
	r.w.logsTick()
	if fb.logsCalls != n {
		t.Error("read while the page was hidden")
	}
	if logsEvery != 2*time.Second {
		t.Errorf("logsEvery %v", logsEvery)
	}
}

func TestLogsPageReadsAgainOnRefreshOnSelectAndWhenTheRevChanges(t *testing.T) {
	fb := logsBackend()
	fb.vault.Rev = "r1"
	r := pageRig(t, fb)
	r.w.rev = "r1"
	r.w.openPage("logs")
	r.tt.Frame()
	n := fb.logsCalls
	if n != 1 {
		t.Fatalf("calls %d", n)
	}
	fb.logs = append([]keelclient.LogEntry{{Time: time.Now(), Kind: "vault", Message: "vault unlocked"}}, fb.logs...)
	r.click(t, "Refresh")
	if fb.logsCalls != n+1 || !r.tt.HasText("vault unlocked") {
		t.Fatalf("Refresh: calls %d", fb.logsCalls)
	}
	r.w.selectTab(0)
	r.w.selectTab(1)
	if fb.logsCalls != n+2 {
		t.Errorf("selecting the page did not read again: %d", fb.logsCalls)
	}
	fb.vault.Rev = "r2"
	r.w.live = true
	r.w.pollOnce()
	if fb.logsCalls != n+3 {
		t.Errorf("a rev change did not read the log again: %d", fb.logsCalls)
	}
	// A failure is shown, not swallowed.
	if got := logTarget(nil, keelclient.LogEntry{HostID: "gone"}); got != "—" {
		t.Errorf("target of a deleted host %q", got)
	}
}

func TestLogRulesAreTheWebviews(t *testing.T) {
	for kind, want := range map[string]string{"session": "sessions", "tunnel": "forwards", "vault": "vault", "host": "vault", "group": "vault", "identity": "vault",
		"snippet": "vault", "notebook": "vault", "agent": "agent", "agent.type": "agent", "AGENT.read": "agent", "share": "other", "error": "other"} {
		if got := logGroup(kind); got != want {
			t.Errorf("logGroup(%q) = %q, want %q", kind, got, want)
		}
	}
	if !logBadgeFailed("Error") || !logBadgeFailed("fail") || !logBadgeFailed("denied") || logBadgeFailed("vault") {
		t.Error("badge")
	}
	if got := logFactsTitle(keelclient.LogEntry{Redactions: map[string]int{"token": 2, "key": 1}}); got != "Redacted: key 1, token 2" {
		t.Error(got)
	}
	if got := logFacts(keelclient.LogEntry{Lines: 1, Bytes: 1}); !slices.Equal(got, []string{"1 line", "1 byte"}) {
		t.Error(got)
	}
	if got := logFacts(keelclient.LogEntry{Kind: "vault"}); got != nil {
		t.Errorf("an entry with no counts has facts %v", got)
	}
}
