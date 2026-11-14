package native

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// rowMenu right-clicks a host row and returns the menu's items.
func (r *rig) rowMenu(t *testing.T, host string) []string {
	t.Helper()
	if err := r.tt.RightClick("host " + host); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	return r.tt.Menu()
}

func TestHostMenuItemsAndOrder(t *testing.T) {
	r := hostRig(t)
	want := []string{"Connect", "Open in split", "Edit", "Duplicate", "Copy SSH command", "Move to folder…", "Agent can read", "Delete"}
	got := slices.DeleteFunc(r.rowMenu(t, "api-1"), func(s string) bool { return s == "-" })
	if !slices.Equal(got, want) {
		t.Fatalf("menu %v, want %v", got, want)
	}
	r.tt.CloseMenu()
	// The local shell can be neither duplicated nor removed.
	got = slices.DeleteFunc(r.rowMenu(t, "Local Shell"), func(s string) bool { return s == "-" })
	if want := []string{"Connect", "Open in split", "Edit", "Move to folder…", "Agent can read"}; !slices.Equal(got, want) {
		t.Fatalf("local menu %v, want %v", got, want)
	}
}

// "Copy SSH command" copies ssh, the port, the jump host and user@address, built from the address fields alone: the menu
// writes it to the clipboard and to no session, and says "Copied SSH command".
func TestCopySSHCommandPutsTheCommandOnTheClipboardOnly(t *testing.T) {
	r := hostRig(t)
	var addr string
	for _, h := range r.w.hosts {
		if h.Name == "api-1" {
			addr = h.Username + "@" + h.Address
		}
	}
	r.rowMenu(t, "api-1")
	if err := r.tt.ChooseMenuItem("Copy SSH command"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	r.tt.Frame()
	if got := r.tt.Clipboard(); got != "ssh "+addr || !r.tt.HasText("Copied SSH command") {
		t.Fatalf("clipboard %q (want %q), toast %v", got, "ssh "+addr, r.tt.HasText("Copied SSH command"))
	}
	if len(r.fb.spawned) != 0 {
		t.Errorf("copying opened a session: %v", r.fb.spawned)
	}
}

func TestSSHCommandCases(t *testing.T) {
	hosts := []keelclient.Host{
		{ID: "j", Name: "bastion", Address: "bastion.example", Port: 2200, Username: "ed", AuthType: "password"},
		{ID: "jd", Address: "bastion", Username: "-oProxyCommand=id", AuthType: "password"},
		{ID: "ja", Address: "-oProxyCommand=id", Username: "ed", AuthType: "password"},
		{ID: "js", Address: "bastion x", Username: "ed", AuthType: "password"},
	}
	for _, c := range []struct {
		name string
		h    keelclient.Host
		want string
	}{
		{"plain", keelclient.Host{Address: "10.0.0.5", Port: 22, Username: "ubuntu", AuthType: "password"}, "ssh ubuntu@10.0.0.5"},
		{"port", keelclient.Host{Address: "db.lan", Port: 2222, Username: "pg", AuthType: "key"}, "ssh -p 2222 pg@db.lan"},
		{"no user", keelclient.Host{Address: "db.lan", AuthType: "password"}, "ssh db.lan"},
		{"jump", keelclient.Host{Address: "10.1.0.9", Username: "app", AuthType: "password", JumpHost: "j"}, "ssh -J ed@bastion.example:2200 app@10.1.0.9"},
		{"ipv6", keelclient.Host{Address: "fe80::1", Username: "root", AuthType: "password"}, "ssh root@[fe80::1]"},
		{"quoted", keelclient.Host{Address: "web", Username: "o'neil", AuthType: "password"}, `ssh 'o'\''neil@web'`},
		{"dash user", keelclient.Host{Address: "web", Username: "-oProxyCommand=touch /tmp/x", AuthType: "password"}, ""},
		{"dash user no space", keelclient.Host{Address: "web", Username: "-oProxyCommand=id", AuthType: "password"}, ""},
		{"dash address", keelclient.Host{Address: "-oProxyCommand=id", Username: "ops", AuthType: "password"}, ""},
		{"dash address no user", keelclient.Host{Address: "-oProxyCommand=id", AuthType: "password"}, ""},
		{"space user", keelclient.Host{Address: "web", Username: "o neil", AuthType: "password"}, ""},
		{"newline address", keelclient.Host{Address: "web\nid", Username: "ops", AuthType: "password"}, ""},
		{"control user", keelclient.Host{Address: "web", Username: "ops\x1b", AuthType: "password"}, ""},
		//lint:ignore ST1018 the zero-width space in the address is the test input
		{"format char address", keelclient.Host{Address: "web​", Username: "ops", AuthType: "password"}, ""},
		{"dash jump user", keelclient.Host{Address: "10.1.0.9", Username: "app", AuthType: "password", JumpHost: "jd"}, ""},
		{"dash jump address", keelclient.Host{Address: "10.1.0.9", Username: "app", AuthType: "password", JumpHost: "ja"}, ""},
		{"space jump address", keelclient.Host{Address: "10.1.0.9", Username: "app", AuthType: "password", JumpHost: "js"}, ""},
		{"local", keelclient.Host{Name: "Local Shell", AuthType: "local"}, ""},
	} {
		if got := sshCommand(c.h, hosts); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestHostMenuActions(t *testing.T) {
	r := hostRig(t)
	choose := func(host, item string) {
		t.Helper()
		r.rowMenu(t, host)
		if err := r.tt.ChooseMenuItem(item); err != nil {
			t.Fatal(err)
		}
		r.tt.Frame()
	}
	choose("api-1", "Connect")
	if !slices.Equal(r.fb.spawned, []string{"h-api"}) {
		t.Fatalf("spawned %v", r.fb.spawned)
	}
	choose("staging-web", "Open in split")
	if !slices.Equal(r.fb.spawned, []string{"h-api", "h-web"}) || r.w.tabCount() != 1 || len(r.w.tabAt(0).panes) != 2 {
		t.Fatalf("spawned %v, panes %d", r.fb.spawned, r.w.paneCount())
	}
	choose("api-1", "Edit")
	if d := r.dlg(t); d.id != "h-api" || d.f.name != "api-1" {
		t.Fatalf("Edit opened %v", d)
	}
	r.press(0, ui.KeyEscape)
	choose("pg-primary", "Duplicate")
	if !slices.Equal(r.fb.duplicated, []string{"h-pg"}) || !r.tt.HasText("pg-primary (copy)") {
		t.Fatalf("duplicated %v", r.fb.duplicated)
	}
	choose("pg-primary", "Agent can read") // on by default: this turns it off
	if v, ok := r.fb.readable["h-pg"]; !ok || v {
		t.Fatalf("agent readable %v", r.fb.readable)
	}
	choose("pg-primary", "Delete")
	if len(r.fb.deleted) != 0 || !r.tt.HasText("Remove pg-primary?") {
		t.Fatal("Delete did not ask first")
	}
	r.click(t, "Cancel")
	if len(r.fb.deleted) != 0 {
		t.Fatal("Cancel deleted")
	}
	choose("pg-primary", "Delete")
	r.click(t, "Remove")
	if !slices.Equal(r.fb.deleted, []string{"h-pg"}) || r.has("host pg-primary") {
		t.Fatalf("deleted %v", r.fb.deleted)
	}
}

func TestAltEnterOnAFocusedHostRowOpensASplit(t *testing.T) {
	r := hostRig(t)
	r.click(t, "host api-1") // a session tab to split
	r.click(t, "Filter hosts")
	for i := 0; i < 16 && !r.tt.Focused("host staging-web"); i++ { // Tab walks from the filter to the rows (MyGo's buttons are stops of their own)
		r.press(0, ui.KeyTab)
	}
	if !r.tt.Focused("host staging-web") {
		t.Fatal("could not focus the row with the keyboard")
	}
	spawned := len(r.fb.spawned)
	r.press(ui.Alt, ui.KeyEnter)
	if len(r.fb.spawned) != spawned+1 || r.fb.spawned[spawned] != "h-web" {
		t.Fatalf("spawned %v", r.fb.spawned)
	}
	if r.w.tabCount() != 1 || len(r.w.tabAt(0).panes) != 2 {
		t.Fatalf("not opened as a split: tabs %d", r.w.tabCount())
	}
	// ↵ alone opens the focused row's host in a tab of its own.
	r.click(t, "Filter hosts")
	for i := 0; i < 16 && !r.tt.Focused("host pg-primary"); i++ { // more tab stops
		r.press(0, ui.KeyTab)
	}
	r.press(0, ui.KeyEnter)
	if got := r.fb.spawned[len(r.fb.spawned)-1]; got != "h-pg" || r.w.tabCount() != 2 {
		t.Errorf("↵ spawned %q, tabs %d", got, r.w.tabCount())
	}
}

func TestHostsReloadAfterAWriteKeepFilterGroupsAndTabs(t *testing.T) {
	r := hostRig(t)
	r.click(t, "host api-1")
	r.w.filter = "pg"
	r.w.prefs.Collapsed["staging"] = true
	r.tt.Frame()
	before := r.fb.hostsCalls
	r.fb.hosts = append(r.fb.hosts, keelclient.Host{ID: "h-new", Name: "pg-replica", Group: "production", AuthType: "key", Address: "10.0.0.9", Username: "dba"})
	r.w.reloadHosts()
	r.tt.Frame()
	if r.fb.hostsCalls != before+1 || !r.tt.HasText("pg-replica") {
		t.Fatalf("hosts calls %d→%d, new host shown %v", before, r.fb.hostsCalls, r.tt.HasText("pg-replica"))
	}
	if r.w.filter != "pg" || !r.w.prefs.Collapsed["staging"] || r.w.tabCount() != 1 || r.tt.HasText("api-1 ") && false {
		t.Errorf("filter %q collapsed %v tabs %d", r.w.filter, r.w.prefs.Collapsed, r.w.tabCount())
	}
	// A pane takes its host's new name.
	h := r.fb.hosts[0]
	for i := range r.fb.hosts {
		if r.fb.hosts[i].ID == "h-api" {
			r.fb.hosts[i].Name = "api-renamed"
			h = r.fb.hosts[i]
		}
	}
	r.w.reloadHosts()
	if r.w.focusedPane().host.Name != h.Name {
		t.Errorf("pane host %q", r.w.focusedPane().host.Name)
	}
}

func TestARevChangeReloadsOnceAndSameRevNever(t *testing.T) {
	r := hostRig(t)
	r.fb.vault.Rev = "e.1"
	r.w.rev = "e.1"
	base := r.fb.hostsCalls
	r.w.pollTick()
	r.w.pollTick()
	if r.fb.hostsCalls != base {
		t.Fatalf("a poll with the same rev read the hosts (%d)", r.fb.hostsCalls-base)
	}
	r.fb.hosts = append(r.fb.hosts, keelclient.Host{ID: "h-x", Name: "from-elsewhere", AuthType: "key", Address: "a", Username: "u"})
	r.fb.vault.Rev = "e.2"
	r.w.pollTick()
	r.tt.Frame()
	if r.fb.hostsCalls != base+1 || !r.tt.HasText("from-elsewhere") || r.w.rev != "e.2" {
		t.Fatalf("hosts calls %d, shown %v, rev %q", r.fb.hostsCalls-base, r.tt.HasText("from-elsewhere"), r.w.rev)
	}
	r.w.pollTick() // the same rev again
	if r.fb.hostsCalls != base+1 {
		t.Error("a second poll at the same rev read the hosts again")
	}
}

func TestNoPollAfterTheWindowCloses(t *testing.T) {
	r := hostRig(t)
	r.w.pollStop = nil
	r.w.startPoll(5 * 1e6) // 5 ms
	if r.w.pollStop == nil {
		t.Fatal("the poll did not start")
	}
	stop := r.w.pollStop
	r.w.closeWindow()
	select {
	case <-stop:
	default:
		t.Fatal("closing the window did not stop the poll")
	}
	calls := r.fb.vaultCalls
	r.w.pollTick()
	if r.fb.vaultCalls != calls {
		t.Error("a tick after close read the vault")
	}
}

func TestRelockShowsUnlockAndKeepsTheTabs(t *testing.T) {
	r := hostRig(t)
	r.fb.unlockWant = testPass
	r.click(t, "host api-1")
	r.click(t, "Split right")
	panes, conns := r.w.paneCount(), len(r.fb.conns)
	r.fb.vault = keelclient.Vault{Locked: true, Sessions: 2}
	r.w.pollTick()
	r.tt.Frame()
	if r.w.gate == nil || !r.tt.HasText("Unlock your vault") || r.tt.HasText("Filter hosts") {
		t.Fatal("the Unlock screen is not over the window")
	}
	if r.w.paneCount() != panes || r.w.tabCount() != 1 {
		t.Errorf("panes %d→%d", panes, r.w.paneCount())
	}
	for _, c := range r.fb.conns {
		if c.closed {
			t.Error("a session was closed by the relock")
		}
	}
	if len(r.fb.killed) != 0 || len(r.fb.conns) != conns {
		t.Error("the relock killed or opened sessions")
	}
	// The poll keeps the screen's counts current and does not stack a second one.
	g := r.w.gate
	r.w.pollTick()
	if r.w.gate != g {
		t.Error("a second Unlock screen replaced the first")
	}
	// Unlocking brings back the same tabs, with the hosts read again.
	hosts := r.fb.hostsCalls
	r.tt.Type(testPass)
	r.press(0, ui.KeyEnter)
	r.tt.Frame()
	if r.w.gate != nil || r.w.paneCount() != panes || r.w.tabCount() != 1 || r.fb.hostsCalls != hosts+1 || !r.tt.HasText("Filter hosts") {
		t.Fatalf("gate %v panes %d hosts calls %d", r.w.gate != nil, r.w.paneCount(), r.fb.hostsCalls-hosts)
	}
}

func TestUnlockedElsewhereTakesTheScreenAway(t *testing.T) {
	r := newRig(t, lockedBackend())
	r.tt.Frame()
	if r.w.gate == nil {
		t.Fatal("no Unlock screen")
	}
	r.fb.vault = keelclient.Vault{Rev: "e.1"}
	r.w.pollTick()
	r.tt.Frame()
	if r.w.gate != nil || !r.tt.HasText("Filter hosts") || !r.tt.HasText("Local Shell") {
		t.Fatal("the screen stayed after another window unlocked the vault")
	}
}

func TestTheRowsRightClickOpensTheMenuWithoutOpeningTheHost(t *testing.T) {
	r := hostRig(t)
	if err := r.tt.RightClick("host api-1"); err != nil {
		t.Fatal(err)
	}
	if m := slices.DeleteFunc(r.tt.Menu(), func(s string) bool { return s == "-" }); !slices.Contains(m, "Edit") {
		t.Fatalf("menu %v", m)
	}
	if len(r.fb.spawned) != 0 {
		t.Errorf("the right-click opened the host: %v", r.fb.spawned)
	}
}
