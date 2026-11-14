package native

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
	"keel/internal/native/sshparse"
)

// Made-up secrets for the fake backend: they open nothing.
const (
	typedPass = "typed-pass-not-real"
	typedKey  = "-----BEGIN KEY-----\nnot-a-real-key\n-----END KEY-----"
	typedPhr  = "typed-phrase-not-real"
)

func hostRig(t *testing.T) *rig {
	t.Helper()
	fb := okBackend()
	fb.hosts = slices.Clone(testHosts)
	fb.groups = []keelclient.Group{{ID: "g1", Name: "production"}, {ID: "g2", Name: "web", ParentID: "g1"}, {ID: "g3", Name: "Alpha"}}
	fb.identities = []keelclient.Identity{{ID: "i1", Name: "deploy key", Kind: "key", Username: "deploy"}, {ID: "i2", Name: "ops", Kind: "password"}}
	r := newRig(t, fb)
	r.tt.Frame()
	return r
}

// fill clicks a field by its name, replaces what it holds and types s.
func (r *rig) fill(t *testing.T, field, s string) {
	t.Helper()
	r.click(t, field)
	r.tt.Key(ui.Cmd, ui.KeyA)
	r.tt.Type(s)
	r.tt.Frame()
}

func (r *rig) dlg(t *testing.T) *hostDialog {
	t.Helper()
	if r.w.hostDlg == nil {
		t.Fatal("the host dialog is not open")
	}
	return r.w.hostDlg
}

func TestNewHostDialogHasEveryGroupAndLabel(t *testing.T) {
	r := hostRig(t)
	r.click(t, "New host") // the sidebar's + (the page's own action has the same name: the first one found)
	r.dlg(t)
	for _, s := range []string{
		"New host", "General", "Label", "Folder", "Color", "Tags", "Connection", "Hostname / IP", "Port", "Username", "Jump host",
		"Connects to this saved host first, with its own key or password from the vault. One hop.",
		"Authentication", "Password", "Private key", "Identity", "Local shell",
		"Host key", "Not saved", "Trusted on first connect; Keel asks again if it changes.",
		"Session", "Startup command",
		"Typed once with Enter when a session opens. Not typed again on reconnect, nor for people who join. Do not put secrets here.",
		"Keepalive", "Keepalive interval (sec)", "Auto reconnect",
		"Agent", "Agent can read this host's panes", "You still choose panes per conversation.",
		"Treat as production", "Let the agent propose notes", "It asks first. Nothing is saved until you press Save.",
		"Test connection", "Cancel", "Save host",
	} {
		if !r.tt.HasText(s) {
			t.Errorf("New host: no %q", s)
		}
	}
	// The notes block is there, and waits for the host to be saved.
	for _, s := range []string{"Notes for the agent", "Save the host first, then add notes for the agent."} {
		if !r.tt.HasText(s) {
			t.Errorf("New host: no %q", s)
		}
	}
	if r.tt.HasText("Delete host") || r.has("Add a note") {
		t.Error("New host shows Delete host, or a field to add a note to a host that is not saved")
	}
	d := r.dlg(t)
	if d.f.port != "22" || d.f.color != "var(--ansi-10)" || d.f.auth != "password" || !d.f.keepAlive || d.f.keepSec != "30" ||
		!d.f.autoReconnect || !d.f.agentReadable || d.f.production || !d.f.agentNotes {
		t.Errorf("defaults %v", d.f)
	}
	if !r.tt.Focused("Label") {
		t.Error("the first field does not have the keyboard")
	}
}

func TestEditHostDialogHasDeleteAndTheSavedValues(t *testing.T) {
	r := hostRig(t)
	h := keelclient.Host{ID: "h-pg", Name: "pg-primary", GroupID: "g1", Address: "10.0.0.3", Port: 2222, Username: "dba", AuthType: "key",
		Color: "var(--ansi-12)", Tags: []string{"db", "prod"}, StartupCommand: "tmux", KeepAliveSec: -1, NoAutoReconnect: true, AgentReadable: boolp(false)}
	r.fb.savedKey = keelclient.KnownKey{Hostname: "10.0.0.3:2222", Fingerprint: "SHA256:xyz", KeyType: "ssh-ed25519", Known: true}
	r.w.editHost(h)
	r.tt.Frame()
	for _, s := range []string{"Edit host", "Delete host", "Trusted", "ED25519", "SHA256:xyz", "Forget host key",
		"Saved for 10.0.0.3:2222. Keel refuses a different key until you replace it."} {
		if !r.tt.HasText(s) {
			t.Errorf("Edit host: no %q", s)
		}
	}
	f := r.dlg(t).f
	if f.name != "pg-primary" || f.port != "2222" || f.auth != "key" || f.tags != "db, prod" || f.keepAlive || f.autoReconnect || f.agentReadable ||
		f.startup != "tmux" || f.color != "var(--ansi-12)" || f.groupID != "g1" {
		t.Errorf("fields %v", f)
	}
	if f.password != "" || f.privateKey != "" || f.passphrase != "" {
		t.Error("a saved secret is in the form")
	}
	// The local shell has no Delete host, no Test connection and no ssh-only block.
	r.w.closeHostDialog()
	r.w.editHost(testHosts[3])
	r.tt.Frame()
	for _, s := range []string{"Delete host", "Test connection", "Startup command", "Jump host", "Host key", "Auto reconnect"} {
		if r.tt.HasText(s) {
			t.Errorf("a local shell shows %q", s)
		}
	}
}

func TestSaveAsHostFromAPastedCommandPrefillsAndSendsNothing(t *testing.T) {
	r := startRig(t)
	r.tt.Type("ssh -p 2222 -o StrictHostKeyChecking=no deploy@10.0.0.5")
	r.tt.Frame()
	r.click(t, "Save as host")
	d := r.dlg(t)
	if d.f.name != "10.0.0.5" || d.f.address != "10.0.0.5" || d.f.port != "2222" || d.f.username != "deploy" || d.f.auth != "password" || d.id != "" {
		t.Fatalf("prefill %v", d.f)
	}
	if !r.tt.HasText("Left out (1), not applied") || !r.tt.HasText("-o StrictHostKeyChecking=no") {
		t.Error("the dialog does not carry the command's notes")
	}
	r.click(t, "Cancel")
	if r.w.hostDlg != nil || len(r.fb.saved) != 0 || len(r.fb.created) != 0 || len(r.fb.spawned) != 0 {
		t.Fatal("Cancel sent a request or kept the dialog")
	}
	// ↵ in the page's input opens it too.
	r.tt.Frame()
	r.press(0, ui.KeyEnter)
	if r.w.hostDlg == nil || r.w.hostDlg.f.username != "deploy" {
		t.Fatal("↵ on a read command does not open New host")
	}
	// A command with no user prefills the rest.
	if got := hostPrefill(sshparse.ParseConnect("ssh prod-db")); got.Name != "prod-db" || got.Username != "" {
		t.Errorf("%+v", got)
	}
}

func TestEntryPointsOpenNewHost(t *testing.T) {
	for name, open := range map[string]func(r *rig, t *testing.T){
		"the sidebar +": func(r *rig, t *testing.T) { // a session tab, so the New tab page's own action is not on screen
			r.click(t, "host Local Shell")
			r.click(t, "New host")
		},
		"⌘N": func(r *rig, t *testing.T) { r.press(ui.Cmd, ui.KeyN) },
		"the Start action": func(r *rig, t *testing.T) {
			r.press(ui.Cmd, ui.KeyB) // sidebar hidden: the page's action is the only New host
			r.click(t, "New host")
		},
		"the palette": func(r *rig, t *testing.T) {
			for _, it := range r.w.paletteItems() {
				if it.group == "Actions" && it.title == "New host" && it.keys == "⌘N" {
					it.run(false)
					return
				}
			}
			t.Fatal("no New host action in the palette")
		},
	} {
		r := hostRig(t)
		open(r, t)
		if r.w.hostDlg == nil || r.w.hostDlg.id != "" {
			t.Errorf("%s did not open New host", name)
		}
	}
	// The failure banner's Edit host.
	r := hostRig(t)
	r.click(t, "host api-1")
	r.fb.conns[0].st = &keelclient.State{Status: "error", Message: "refused"}
	r.tt.Frame()
	r.click(t, "Edit host")
	if d := r.dlg(t); d.id != "h-api" || d.f.name != "api-1" {
		t.Errorf("Edit host from the banner opened %v", d)
	}
}

func TestHostValidationMessages(t *testing.T) {
	r := hostRig(t)
	r.w.newHost()
	r.tt.Frame()
	for _, c := range []struct {
		set  func(f *hostForm)
		want string
	}{
		{func(f *hostForm) {}, "name is required"},
		{func(f *hostForm) { f.name = "x" }, "address is required"},
		{func(f *hostForm) { f.address = "a" }, "username is required"},
		{func(f *hostForm) { f.username = "u"; f.auth = "identity"; f.identityID = "" }, "identity is required"},
		{func(f *hostForm) { f.auth = "password"; f.startup = "a\tb" }, "startup command must be one line with no control characters"},
		{func(f *hostForm) { f.startup = strings.Repeat("a", 1025) }, "startup command is longer than 1024 bytes"},
	} {
		c.set(&r.dlg(t).f)
		r.click(t, "Save host")
		if r.w.hostDlg == nil || r.dlg(t).err != c.want || !r.tt.HasText(c.want) {
			t.Fatalf("want %q, got %q", c.want, r.w.hostDlg.err)
		}
	}
	if len(r.fb.saved) != 0 {
		t.Error("a refused form was sent")
	}
	// A local shell needs only a name.
	r.dlg(t).f = hostForm{name: "x", auth: "local"}
	if m := r.dlg(t).f.validate(); m != "" {
		t.Errorf("local: %q", m)
	}
}

func TestServerRefusalStaysInTheDialog(t *testing.T) {
	r := hostRig(t)
	r.w.newHost()
	r.tt.Frame()
	d := r.dlg(t)
	d.f.name, d.f.address, d.f.username, d.f.password = "x", "a", "u", typedPass
	r.fb.saveErr = &keelclient.StatusError{Code: 400, Msg: "one hop only"}
	r.click(t, "Save host")
	if r.w.hostDlg == nil || !r.tt.HasText("one hop only") {
		t.Fatal("the server's reason is not shown in the dialog")
	}
	if d.f.password != "" {
		t.Error("the password stayed in the form after the request was sent")
	}
}

func TestSaveSendsTheExpectedBodies(t *testing.T) {
	r := hostRig(t)
	r.click(t, "New host")
	r.fill(t, "Label", "web-1")
	r.fill(t, "Tags", "prod, gpu ,")
	r.fill(t, "Hostname / IP", "10.9.0.1")
	r.fill(t, "Port", "2200")
	r.fill(t, "Username", "ubuntu")
	r.fill(t, "Host password", typedPass)
	d := r.dlg(t)
	d.f.groupID, d.f.jump = "g2", "h-api"
	r.press(ui.Cmd, ui.KeyEnter) // ⌘↵ saves
	if len(r.fb.saved) != 1 {
		t.Fatalf("%d saves", len(r.fb.saved))
	}
	got := r.fb.saved[0]
	want := keelclient.HostDetail{
		Name: "web-1", GroupID: "g2", Tags: []string{"prod", "gpu"}, Address: "10.9.0.1", Port: 2200, Username: "ubuntu", AuthType: "password",
		Password: typedPass, Color: "var(--ansi-10)", JumpHost: "h-api", KeepAliveSec: 30, AgentReadable: boolp(true), Production: boolp(false), AgentNotes: boolp(true),
	}
	if fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) || got.Password != typedPass || got.ID != "" || !slices.Equal(got.Tags, want.Tags) ||
		got.Port != 2200 || got.KeepAliveSec != 30 || got.NoAutoReconnect || got.JumpHost != "h-api" || got.GroupID != "g2" {
		t.Fatalf("body %v", got)
	}
	if r.w.hostDlg != nil || !r.tt.HasText("web-1") {
		t.Error("the dialog stayed open, or the new host is not in the tree")
	}
	if !slices.Contains(r.fb.calls, "hosts") {
		t.Error("the hosts were not read again")
	}
	if r.dlgSecretsLeft() {
		t.Error("a secret is left on the window")
	}

	// Edit with the password left blank: no password in the body (the server keeps the saved one).
	h := r.fb.hosts[len(r.fb.hosts)-1]
	r.w.editHost(h)
	r.tt.Frame()
	r.fill(t, "Label", "web-2")
	r.click(t, "Save host")
	got = r.fb.saved[1]
	if got.ID != h.ID || got.Name != "web-2" || got.Password != "" || got.PrivateKey != "" || got.Passphrase != "" || got.AuthType != "password" {
		t.Fatalf("edit body %v (password %q)", got, got.Password)
	}
	// Edit with a changed password: it is sent.
	r.w.editHost(r.fb.hosts[len(r.fb.hosts)-1])
	r.tt.Frame()
	r.fill(t, "Host password", typedPass+"2")
	r.click(t, "Save host")
	if got = r.fb.saved[2]; got.Password != typedPass+"2" || got.ID != h.ID {
		t.Fatalf("changed password not sent: %v", got)
	}
	// A key host sends its key and passphrase, and no password.
	r.w.newHost()
	r.tt.Frame()
	d = r.dlg(t)
	d.f.name, d.f.address, d.f.username, d.f.password = "k", "a", "u", typedPass
	d.setAuth("key")
	d.f.privateKey, d.f.passphrase, d.f.keepAlive, d.f.autoReconnect = typedKey, typedPhr, false, false
	r.tt.Frame()
	r.click(t, "Save host")
	got = r.fb.saved[3]
	if got.AuthType != "key" || got.PrivateKey != typedKey || got.Passphrase != typedPhr || got.Password != "" || got.KeepAliveSec != -1 || !got.NoAutoReconnect {
		t.Fatalf("key body %v", got)
	}
	// A local shell has no keepalive, jump host or startup command.
	r.w.newHost()
	r.tt.Frame()
	d = r.dlg(t)
	d.f.name, d.f.jump, d.f.startup = "l", "h-api", "x"
	d.setAuth("local")
	r.tt.Frame()
	r.click(t, "Save host")
	if len(r.fb.saved) < 5 {
		t.Fatalf("the local shell was not saved: %q", d.err)
	}
	if got = r.fb.saved[4]; got.AuthType != "local" || got.KeepAliveSec != 0 || got.JumpHost != "" || got.StartupCommand != "" || got.NoAutoReconnect {
		t.Fatalf("local body %v", got)
	}
}

// dlgSecretsLeft reports a typed secret anywhere in the workspace's dialog state.
func (r *rig) dlgSecretsLeft() bool {
	if d := r.w.hostDlg; d != nil {
		return d.f.password != "" || d.f.privateKey != "" || d.f.passphrase != ""
	}
	return false
}

func TestEscapeAndCancelSendNothingAndClearSecrets(t *testing.T) {
	for _, closer := range []func(r *rig, t *testing.T){
		func(r *rig, t *testing.T) { r.press(0, ui.KeyEscape) },
		func(r *rig, t *testing.T) { r.click(t, "Cancel") },
	} {
		r := hostRig(t)
		r.w.newHost()
		r.tt.Frame()
		d := r.dlg(t)
		d.f.name, d.f.address, d.f.username = "x", "a", "u"
		d.f.password, d.f.privateKey, d.f.passphrase = typedPass, typedKey, typedPhr
		closer(r, t)
		if r.w.hostDlg != nil {
			t.Fatal("the dialog is still open")
		}
		if d.f.password != "" || d.f.privateKey != "" || d.f.passphrase != "" {
			t.Error("the secrets were not cleared")
		}
		if len(r.fb.saved) != 0 || len(r.fb.tests) != 0 || len(r.fb.trusted) != 0 || len(r.fb.deleted) != 0 {
			t.Error("closing sent a request")
		}
	}
}

func TestEachTestOutcomeRendersItsText(t *testing.T) {
	cases := []struct {
		res          keelclient.TestResult
		short, extra string
	}{
		{keelclient.TestResult{OK: true, Kind: "ok", LatencyMs: 12}, "Connected in 12 ms", ""},
		{keelclient.TestResult{Kind: "auth", Message: "no more methods"}, "Authentication failed", "the server refused the password or key. no more methods"},
		{keelclient.TestResult{Kind: "unknown-host", Hostname: "10.0.0.9:22", Fingerprint: "SHA256:fp", KeyType: "ssh-ed25519", TrustToken: "tt"}, "Host key unknown",
			"10.0.0.9:22 presented a key Keel has not seen. Compare it with the server's own fingerprint before you trust it."},
		{keelclient.TestResult{Kind: "host-key-mismatch", Hostname: "10.0.0.9:22", Fingerprint: "SHA256:new", Expected: "SHA256:old", KeyType: "ecdsa-sha2-nistp256", TrustToken: "tt"}, "Host key changed",
			"10.0.0.9:22 presented a different key than the one saved. Only replace it if you know the server was reinstalled."},
		{keelclient.TestResult{Kind: "timeout", Message: "i/o timeout"}, "Timed out", "no answer within 10 seconds. i/o timeout"},
		{keelclient.TestResult{Kind: "dns", Message: "no such host"}, "Host not found", "the name does not resolve. no such host"},
		{keelclient.TestResult{Kind: "refused", Message: "connection refused"}, "Connection refused", "nothing accepts SSH on that port. connection refused"},
		{keelclient.TestResult{Kind: "weird", Message: "boom"}, "Could not connect", "boom"},
		{keelclient.TestResult{Kind: "auth", Hop: "jump", HopName: "bastion", Message: "m"}, "Authentication failed", "Jump host bastion: the server refused the password or key. m"},
		{keelclient.TestResult{Kind: "refused", Hop: "target", Message: "m"}, "Connection refused", "Through the jump host: nothing accepts SSH on that port. m"},
	}
	for _, c := range cases {
		r := hostRig(t)
		r.fb.testReply = []keelclient.TestResult{c.res}
		r.w.newHost()
		r.tt.Frame()
		d := r.dlg(t)
		d.f.name, d.f.address, d.f.username, d.f.password = "x", "a", "u", typedPass
		r.click(t, "Test connection")
		r.tt.Frame()
		if !r.tt.HasText(c.short) || (c.extra != "" && !r.tt.HasText(c.extra)) {
			t.Errorf("%s: short %q shown %v, detail shown %v", c.res.Kind, c.short, r.tt.HasText(c.short), r.tt.HasText(c.extra))
		}
		if len(r.fb.tests) != 1 || r.fb.tests[0].Password != typedPass || r.fb.tests[0].Address != "a" {
			t.Errorf("%s: the test did not send the dialog's values", c.res.Kind)
		}
		if len(r.fb.trusted) != 0 || len(r.fb.saved) != 0 {
			t.Errorf("%s: a test trusted a key or saved a host", c.res.Kind)
		}
		_, offered := c.res.HostKeyOffer()
		if got := r.has("Trust and save") || r.has("Replace saved key"); got != offered {
			t.Errorf("%s: trust button shown %v, want %v", c.res.Kind, got, offered)
		}
		if offered && !r.tt.HasText(fingerprintText(c.res)) {
			t.Errorf("%s: no fingerprint line %q", c.res.Kind, fingerprintText(c.res))
		}
	}
	if got := fingerprintText(cases[3].res); got != "ECDSA SHA256:new (saved: SHA256:old)" {
		t.Errorf("fingerprint text %q", got)
	}
}

func TestTrustFromATestResultSendsTheOfferAndTestsAgain(t *testing.T) {
	r := hostRig(t)
	offer := keelclient.TestResult{Kind: "unknown-host", Hostname: "10.0.0.9:22", Fingerprint: "SHA256:fp", KeyType: "ssh-ed25519", TrustToken: "tt-1"}
	r.fb.testReply = []keelclient.TestResult{offer, {OK: true, Kind: "ok", LatencyMs: 7}}
	r.w.newHost()
	r.tt.Frame()
	d := r.dlg(t)
	d.f.name, d.f.address, d.f.username, d.f.password = "x", "a", "u", typedPass
	r.click(t, "Test connection")
	r.tt.Frame()
	if len(r.fb.trusted) != 0 {
		t.Fatal("the test trusted the key on its own")
	}
	r.tt.Frame()
	r.click(t, "Trust and save")
	if len(r.fb.trusted) != 1 {
		t.Fatalf("%d trusts", len(r.fb.trusted))
	}
	k := r.fb.trusted[0]
	if k.Token != "tt-1" || k.Hostname != "10.0.0.9:22" || k.Fingerprint != "SHA256:fp" || r.fb.replaced[0] {
		t.Fatalf("offer %+v replace %v", k, r.fb.replaced[0])
	}
	if len(r.fb.tests) != 2 || !r.tt.HasText("Connected in 7 ms") {
		t.Fatalf("tests %d, ok shown %v", len(r.fb.tests), r.tt.HasText("Connected in 7 ms"))
	}
	if len(r.fb.saved) != 0 {
		t.Error("Trust and save saved the host")
	}
}

func TestReplacingAChangedKeyAsksFirst(t *testing.T) {
	r := hostRig(t)
	r.fb.testReply = []keelclient.TestResult{{Kind: "host-key-mismatch", Hostname: "h:22", Fingerprint: "SHA256:new", Expected: "SHA256:old", TrustToken: "tt"}, {OK: true, Kind: "ok"}}
	r.w.newHost()
	r.tt.Frame()
	d := r.dlg(t)
	d.f.name, d.f.address, d.f.username, d.f.password = "x", "a", "u", typedPass
	r.click(t, "Test connection")
	r.tt.Frame()
	r.click(t, "Replace saved key")
	if len(r.fb.trusted) != 0 || !r.tt.HasText("Replace host key?") ||
		!r.tt.HasText("Replace the saved key for h:22 with SHA256:new? Only do this if you know the server was reinstalled.") {
		t.Fatalf("no confirm, or it already trusted (%d)", len(r.fb.trusted))
	}
	r.click(t, "Cancel")
	if len(r.fb.trusted) != 0 || r.w.hostDlg == nil {
		t.Fatal("Cancel trusted the key, or lost the dialog")
	}
	r.click(t, "Replace saved key")
	r.click(t, "Replace key")
	if len(r.fb.trusted) != 1 || !r.fb.replaced[0] {
		t.Fatalf("trusted %d replaced %v", len(r.fb.trusted), r.fb.replaced)
	}
}

func TestEditingDropsTheTestResult(t *testing.T) {
	r := hostRig(t)
	r.fb.testReply = []keelclient.TestResult{{Kind: "unknown-host", Hostname: "h:22", Fingerprint: "SHA256:fp", TrustToken: "tt"}}
	r.w.newHost()
	r.tt.Frame()
	d := r.dlg(t)
	d.f.name, d.f.address, d.f.username, d.f.password = "x", "a", "u", typedPass
	r.click(t, "Test connection")
	r.tt.Frame()
	if !r.has("Trust and save") {
		t.Fatal("no offer")
	}
	// A change the test did not use (the folder) keeps it; one it used (the address) drops it.
	d.f.groupID = "g1"
	r.tt.Frame()
	if !r.has("Trust and save") {
		t.Error("an unrelated edit dropped the result")
	}
	d.f.address = "other"
	r.tt.Frame()
	if r.has("Trust and save") || r.tt.HasText("Host key unknown") {
		t.Error("the result stayed after the address changed")
	}
}

func TestTestConnectionCanBeCancelled(t *testing.T) {
	r := hostRig(t)
	r.fb.testWait = make(chan struct{})
	posts := make(chan func(), 16)
	r.w.work = func(fn func()) { go fn() }
	r.w.post = func(fn func()) { posts <- fn }
	// run posts what the background work has finished, until cond holds.
	run := func(cond func() bool, what string) {
		t.Helper()
		for i := 0; i < 200 && !cond(); i++ {
			select {
			case fn := <-posts:
				fn()
			case <-time.After(20 * time.Millisecond):
			}
		}
		if !cond() {
			t.Fatalf("timed out: %s", what)
		}
	}
	r.w.newHost()
	r.tt.Frame()
	d := r.dlg(t)
	d.f.name, d.f.address, d.f.username, d.f.password = "x", "a", "u", typedPass
	r.click(t, "Test connection")
	if !d.test.running || !r.has("Cancel test") || !r.tt.HasText("Testing…") {
		t.Fatal("not running")
	}
	r.click(t, "Cancel test")
	run(func() bool { return !d.test.running }, "the test to stop")
	r.tt.Frame()
	if !r.tt.HasText("Test cancelled") || !r.has("Test connection") {
		t.Errorf("cancelled text shown %v", r.tt.HasText("Test cancelled"))
	}
	// Closing the dialog while a test runs cancels it and drops the result.
	r.fb.testWait = make(chan struct{})
	r.click(t, "Test connection")
	r.w.closeHostDialog()
	run(func() bool { return len(posts) == 0 && !d.test.running }, "the late result")
	for len(posts) > 0 {
		(<-posts)()
	}
	if r.w.hostDlg != nil {
		t.Error("the late result reopened the dialog")
	}
}

func TestDeleteHostAsksFirst(t *testing.T) {
	r := hostRig(t)
	h := testHosts[1]
	r.w.editHost(h)
	r.tt.Frame()
	r.click(t, "Delete host")
	if !r.tt.HasText("Remove api-1?") || !r.tt.HasText("Confirm") || len(r.fb.deleted) != 0 {
		t.Fatal("no confirm, or it deleted already")
	}
	r.click(t, "Cancel")
	if len(r.fb.deleted) != 0 || r.w.hostDlg == nil || !r.tt.HasText("Edit host") {
		t.Fatal("Cancel in the confirm sent a request or lost the host dialog")
	}
	r.click(t, "Delete host")
	r.click(t, "Remove")
	if !slices.Equal(r.fb.deleted, []string{"h-api"}) || r.w.hostDlg != nil || r.tt.HasText("api-1") {
		t.Fatalf("deleted %v, dialog open %v, still in the tree %v", r.fb.deleted, r.w.hostDlg != nil, r.tt.HasText("api-1"))
	}
}

func TestDeletedHostKeepsItsOpenSession(t *testing.T) {
	r := hostRig(t)
	r.click(t, "host api-1")
	if r.w.tabCount() != 1 || r.w.activeTab().isNew() {
		t.Fatal("no session")
	}
	r.w.askDeleteHost(testHosts[1])
	r.tt.Frame()
	r.click(t, "Remove")
	if r.w.tabCount() != 1 || len(r.fb.killed) != 0 || r.w.focusedPane().host.Name != "api-1" || r.fb.conns[0].closed {
		t.Error("deleting the host touched its open session")
	}
	// Edit host from that pane's banner says the host is gone.
	r.w.editPaneHost(r.w.focusedPane())
	r.tt.Frame()
	if !r.tt.HasText("Host not found") || r.w.hostDlg != nil {
		t.Errorf("toast shown %v, dialog %v", r.tt.HasText("Host not found"), r.w.hostDlg)
	}
}

func TestForgetHostKeyAsksFirst(t *testing.T) {
	r := hostRig(t)
	r.fb.savedKey = keelclient.KnownKey{Hostname: "10.0.0.1:22", Fingerprint: "SHA256:s", KeyType: "ssh-rsa", Known: true}
	r.w.editHost(testHosts[1])
	r.tt.Frame()
	r.click(t, "Forget host key")
	if len(r.fb.forgot) != 0 || !r.tt.HasText("Forget host key?") ||
		!r.tt.HasText("Keel forgets the saved key for api-1 (10.0.0.1:22). The next connect shows the server's key and asks you to trust it again.") {
		t.Fatal("no confirm, or it forgot already")
	}
	r.click(t, "Cancel")
	if len(r.fb.forgot) != 0 {
		t.Fatal("Cancel forgot the key")
	}
	r.click(t, "Forget host key")
	r.click(t, "Forget key")
	r.tt.Frame()
	if !slices.Equal(r.fb.forgot, []string{"h-api"}) || !r.tt.HasText("Not saved") || r.has("Forget host key") {
		t.Errorf("forgot %v", r.fb.forgot)
	}
}

func TestPickersListWhatTheWebviewLists(t *testing.T) {
	groups := []keelclient.Group{{ID: "g1", Name: "production"}, {ID: "g2", Name: "web", ParentID: "g1"}, {ID: "g3", Name: "alpha"}}
	var labels []string
	for _, e := range folderEntries(groups) {
		labels = append(labels, e.label)
	}
	if want := []string{"No group", "alpha", "production", "production / web"}; !slices.Equal(labels, want) {
		t.Errorf("folders %v", labels)
	}
	hosts := []keelclient.Host{
		{ID: "a", Name: "a", AuthType: "key", Address: "a", Username: "u"}, {ID: "b", Name: "b", AuthType: "key", JumpHost: "a"},
		{ID: "l", Name: "Local", AuthType: "local"}, {ID: "c", Name: "c", AuthType: "password", Address: "c", Username: "u", Port: 2222},
	}
	var got []string
	for _, e := range jumpEntries(hosts, "c", "") { // excludes itself, hosts with a jump host and the local shell
		got = append(got, e.label)
	}
	if want := []string{"None", "a"}; !slices.Equal(got, want) {
		t.Errorf("jump entries %v", got)
	}
	if e := jumpEntries(hosts, "x", "gone"); e[len(e)-1].label != "Missing host — pick another" {
		t.Error("a missing jump host is not kept listed")
	}
	if e := identityEntries([]keelclient.Identity{{ID: "i", Name: "n", Kind: "key", Username: "u"}}, "i"); e[0].desc != "Private key · u" {
		t.Errorf("%+v", e)
	}
	if e := identityEntries(nil, "gone"); e[0].label != "Missing identity — pick another" {
		t.Error("a missing identity is not kept listed")
	}
}

func TestPickingFromTheMenus(t *testing.T) {
	r := hostRig(t)
	r.w.newHost()
	r.tt.Frame()
	r.click(t, "Folder picker")
	if err := r.tt.ChooseMenuItem("production / web"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if r.dlg(t).f.groupID != "g2" {
		t.Errorf("folder %q", r.dlg(t).f.groupID)
	}
	r.click(t, "Jump host picker")
	if m := r.tt.Menu(); !slices.Contains(m, "api-1") || slices.Contains(m, "Local Shell") {
		t.Errorf("jump menu %v", m)
	}
	r.tt.ChooseMenuItem("api-1")
	r.tt.Frame()
	if r.dlg(t).f.jump != "h-api" {
		t.Errorf("jump %q", r.dlg(t).f.jump)
	}
	r.click(t, "Identity") // the segment of the Authentication control
	if r.dlg(t).f.auth != "identity" || r.dlg(t).f.identityID != "i1" {
		t.Errorf("auth %q identity %q: a new Identity host starts with the first identity", r.dlg(t).f.auth, r.dlg(t).f.identityID)
	}
	// Choosing a folder named production turns Treat as production on.
	r.dlg(t).setFolder("g1")
	if !r.dlg(t).f.production {
		t.Error("production was not turned on")
	}
	r.dlg(t).setFolder("")
	if !r.dlg(t).f.production {
		t.Error("leaving the folder turned production off")
	}
}

func TestNoSecretInPrintedDialogState(t *testing.T) {
	r := hostRig(t)
	r.w.newHost()
	d := r.dlg(t)
	d.f.name, d.f.password, d.f.privateKey, d.f.passphrase = "x", typedPass, typedKey, typedPhr
	for _, v := range []any{d, *d, d.f, &d.f} {
		for _, f := range []string{"%v", "%+v", "%#v", "%s"} {
			out := fmt.Sprintf(f, v)
			if strings.Contains(out, typedPass) || strings.Contains(out, "not-a-real-key") || strings.Contains(out, typedPhr) {
				t.Errorf("%s of %T prints a secret", f, v)
			}
		}
	}
	g := &unlockGate{pass: testPass}
	if out := fmt.Sprintf("%v %+v %#v", g, g, g); strings.Contains(out, testPass) {
		t.Error("the unlock state prints the passphrase")
	}
}

func TestAgentSwitchesAndKeepaliveDefaults(t *testing.T) {
	if got := newHostForm(&keelclient.Host{KeepAliveSec: 0}); !got.keepAlive || got.keepSec != "30" {
		t.Errorf("%v", got)
	}
	if got := newHostForm(&keelclient.Host{KeepAliveSec: 45}); !got.keepAlive || got.keepSec != "45" {
		t.Errorf("%v", got)
	}
	if got := newHostForm(&keelclient.Host{KeepAliveSec: -1}); got.keepAlive || got.keepSec != "30" {
		t.Errorf("%v", got)
	}
	f := newHostForm(nil)
	f.keepSec, f.port = "9999", "99999"
	if d := f.detail(""); d.KeepAliveSec != 600 || d.Port != 65535 {
		t.Errorf("clamp: keepalive %d port %d", d.KeepAliveSec, d.Port)
	}
	f.keepSec, f.port = "-3", "x"
	if d := f.detail(""); d.KeepAliveSec != 30 || d.Port != 22 {
		t.Errorf("fallback: keepalive %d port %d", d.KeepAliveSec, d.Port)
	}
}
