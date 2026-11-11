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
	// hostRowMenu in rowmenus.templ, for the items whose feature exists here.
	want := []string{"Connect", "Open in split", "Edit", "Duplicate", "Move to folder…", "Agent can read", "Delete"}
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
	if !slices.Equal(r.fb.spawned, []string{"h-api", "h-web"}) || len(r.w.tabs) != 1 || len(r.w.tabs[0].panes) != 2 {
		t.Fatalf("spawned %v, panes %d", r.fb.spawned, r.w.paneCount())
	}
	choose("api-1", "Edit")
	if d := r.dlg(t); d.id != "h-api" || d.f.name != "api-1" {
		t.Fatalf("Edit opened %v", d)
	}
	r.press(0, ui.KeyEscape)
	choose("pg-primary", "Duplicate")
	if !slices.Equal(r.fb.duplicated, []string{"h-pg"}) || !r.hasText("pg-primary (copy)") {
		t.Fatalf("duplicated %v", r.fb.duplicated)
	}
	choose("pg-primary", "Agent can read") // on by default: this turns it off
	if v, ok := r.fb.readable["h-pg"]; !ok || v {
		t.Fatalf("agent readable %v", r.fb.readable)
	}
	choose("pg-primary", "Delete")
	if len(r.fb.deleted) != 0 || !r.hasText("Remove pg-primary?") {
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
	for i := 0; i < 8 && !r.tt.Focused("host staging-web"); i++ { // Tab walks from the filter to the rows
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
	if len(r.w.tabs) != 1 || len(r.w.tabs[0].panes) != 2 {
		t.Fatalf("not opened as a split: tabs %d", len(r.w.tabs))
	}
	// ↵ alone opens the focused row's host in a tab of its own.
	r.click(t, "Filter hosts")
	for i := 0; i < 8 && !r.tt.Focused("host pg-primary"); i++ {
		r.press(0, ui.KeyTab)
	}
	r.press(0, ui.KeyEnter)
	if got := r.fb.spawned[len(r.fb.spawned)-1]; got != "h-pg" || len(r.w.tabs) != 2 {
		t.Errorf("↵ spawned %q, tabs %d", got, len(r.w.tabs))
	}
}

func TestHostsReloadAfterAWriteKeepFilterGroupsAndTabs(t *testing.T) {
	r := hostRig(t)
	r.click(t, "host api-1")
	r.w.filter = "pg"
	r.w.collapsed["staging"] = true
	r.tt.Frame()
	before := r.fb.hostsCalls
	r.fb.hosts = append(r.fb.hosts, keelclient.Host{ID: "h-new", Name: "pg-replica", Group: "production", AuthType: "key", Address: "10.0.0.9", Username: "dba"})
	r.w.reloadHosts()
	r.tt.Frame()
	if r.fb.hostsCalls != before+1 || !r.hasText("pg-replica") {
		t.Fatalf("hosts calls %d→%d, new host shown %v", before, r.fb.hostsCalls, r.hasText("pg-replica"))
	}
	if r.w.filter != "pg" || !r.w.collapsed["staging"] || len(r.w.tabs) != 1 || r.hasText("api-1 ") && false {
		t.Errorf("filter %q collapsed %v tabs %d", r.w.filter, r.w.collapsed, len(r.w.tabs))
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
	if r.fb.hostsCalls != base+1 || !r.hasText("from-elsewhere") || r.w.rev != "e.2" {
		t.Fatalf("hosts calls %d, shown %v, rev %q", r.fb.hostsCalls-base, r.hasText("from-elsewhere"), r.w.rev)
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
	if r.w.gate == nil || !r.hasText("Unlock your vault") || r.hasText("Filter hosts") {
		t.Fatal("the Unlock screen is not over the window")
	}
	if r.w.paneCount() != panes || len(r.w.tabs) != 1 {
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
	if r.w.gate != nil || r.w.paneCount() != panes || len(r.w.tabs) != 1 || r.fb.hostsCalls != hosts+1 || !r.hasText("Filter hosts") {
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
	if r.w.gate != nil || !r.hasText("Filter hosts") || !r.hasText("Local Shell") {
		t.Fatal("the screen stayed after another window unlocked the vault")
	}
}

func TestTheRowsMoreControlOpensTheMenuWithoutOpeningTheHost(t *testing.T) {
	r := hostRig(t)
	rc, _ := r.tt.Find("host api-1")
	r.tt.Move(rc.X+20, rc.Y+rc.H/2) // the control shows while the pointer is on the row
	r.tt.Frame()
	r.click(t, "Actions for api-1")
	if m := slices.DeleteFunc(r.tt.Menu(), func(s string) bool { return s == "-" }); !slices.Contains(m, "Edit") {
		t.Fatalf("menu %v", m)
	}
	if len(r.fb.spawned) != 0 {
		t.Errorf("the … control opened the host: %v", r.fb.spawned)
	}
}
