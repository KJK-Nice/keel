package native

import (
	"slices"
	"strings"
	"testing"
	"time"

	"keel/internal/native/keelclient"
)

// Port forwarding on the fake backend: the page, the dialog, the sidebar section and the status bar.

func forwardBackend() *fakeBackend {
	fb := okBackend()
	fb.tunnels = []keelclient.Tunnel{
		{ID: "t1", Name: "db local", HostID: "h-pg", Kind: "local", BindAddr: "127.0.0.1", BindPort: 5433, DestHost: "127.0.0.1", DestPort: 5432, Status: "ready", ListenAddr: "127.0.0.1:5433"},
		{ID: "t2", Name: "web back", HostID: "h-web", Kind: "remote", BindAddr: "127.0.0.1", BindPort: 8080, DestHost: "localhost", DestPort: 3000, Status: "stopped"},
		{ID: "t3", Name: "socks", HostID: "h-api", Kind: "dynamic", BindAddr: "127.0.0.1", BindPort: 0, Status: "connecting"},
		{ID: "t4", Name: "open bind", HostID: "h-api", Kind: "local", BindAddr: "0.0.0.0", BindPort: 9000, DestHost: "10.0.0.9", DestPort: 80, Status: "error", Error: "connection refused"},
	}
	return fb
}

func forwardRig(t *testing.T, fb *fakeBackend) *rig {
	t.Helper()
	r := newRig(t, fb)
	r.tt.Frame()
	r.w.showPage("forwards")
	r.tt.Frame()
	return r
}

func TestTunnelHelpersAreTheAppsOwn(t *testing.T) {
	ready := tunnelState(keelclient.Tunnel{Status: "ready"})
	if ready.label != "Running" || ready.dot != "live" || !ready.on || ready.filter != "running" {
		t.Errorf("ready %+v", ready)
	}
	if c := tunnelState(keelclient.Tunnel{Status: "connecting"}); c.label != "Connecting" || !c.on || c.filter != "running" {
		t.Errorf("connecting %+v", c)
	}
	if f := tunnelState(keelclient.Tunnel{Status: "error", Error: "refused"}); f.label != "Failed · refused" || f.dot != "dead" || f.on || f.filter != "stopped" {
		t.Errorf("failed %+v", f)
	}
	if f := tunnelState(keelclient.Tunnel{Status: "error"}); f.label != "Failed" {
		t.Errorf("failed with no cause %+v", f)
	}
	if s := tunnelState(keelclient.Tunnel{}); s.label != "Stopped" || s.on {
		t.Errorf("stopped %+v", s)
	}
	if tunnelFlag("local") != "-L" || tunnelFlag("remote") != "-R" || tunnelFlag("dynamic") != "-D" || tunnelFlag("") != "-L" {
		t.Error("flags")
	}
	for addr, want := range map[string]bool{"0.0.0.0": true, "::": true, "[::]": true, "*": true, " 0.0.0.0 ": true, "127.0.0.1": false, "localhost": false, "::1": false} {
		if got := tunnelEveryInterface(keelclient.Tunnel{BindAddr: addr}); got != want {
			t.Errorf("every interface %q = %v", addr, got)
		}
	}
	if got := tunnelListen(keelclient.Tunnel{BindAddr: "127.0.0.1"}); got != "127.0.0.1:auto" {
		t.Errorf("listen %q", got)
	}
	if got := tunnelListen(keelclient.Tunnel{BindAddr: "127.0.0.1", BindPort: 80, ListenAddr: "127.0.0.1:4242"}); got != "127.0.0.1:4242" {
		t.Errorf("listen %q", got)
	}
	if tunnelDest(keelclient.Tunnel{Kind: "dynamic"}) != "SOCKS5" || tunnelDest(keelclient.Tunnel{DestHost: "h", DestPort: 5}) != "h:5" {
		t.Error("dest")
	}
	if tunnelLocalAddress(keelclient.Tunnel{Kind: "remote", DestHost: "h", DestPort: 5}) != "h:5" || tunnelLocalAddress(keelclient.Tunnel{BindAddr: "127.0.0.1"}) != "" ||
		tunnelLocalAddress(keelclient.Tunnel{BindPort: 7}) != "127.0.0.1:7" {
		t.Error("local address")
	}
	if forwardsLabel(1) != "1 forward" || forwardsLabel(2) != "2 forwards" {
		t.Error("count words")
	}
}

func TestForwardsPageRowsForEveryTypeAndState(t *testing.T) {
	r := forwardRig(t, forwardBackend())
	r.texts(t, "Port forwarding", "Each forward opens its own SSH connection. It keeps running when you close the window.", "New forward",
		"Name", "Type", "Route", "Through", "Status", "On",
		"db local", "127.0.0.1:5433 → 127.0.0.1:5432", "pg-primary", "Running",
		"web back", "127.0.0.1:8080 → localhost:3000", "staging-web", "Stopped",
		"socks", "127.0.0.1:auto → SOCKS5", "Connecting",
		"open bind", "0.0.0.0:9000 → 10.0.0.9:80", "Failed · connection refused", "Retry", "Every interface",
		"-L", "-R", "-D")
	if n := strings.Count(strings.Join(r.tt.Texts(), "\n"), "badge Every interface"); n != 1 {
		t.Errorf("%d Every interface badges, want 1 (only the 0.0.0.0 forward)", n)
	}
	r.texts(t, "All 4", "Running 2", "Stopped 2")
	if _, ok := r.tt.Find("tab Port forwarding"); !ok {
		t.Error("the tab is not named Port forwarding")
	}
}

func TestForwardsChipsAndFilterNarrowRows(t *testing.T) {
	r := forwardRig(t, forwardBackend())
	r.click(t, "Running 2")
	if r.has("forward web back") || r.has("forward open bind") || !r.has("forward db local") || !r.has("forward socks") {
		t.Errorf("Running shows %v", r.tt.Texts())
	}
	r.click(t, "Stopped 2")
	if !r.has("forward web back") || !r.has("forward open bind") || r.has("forward db local") {
		t.Errorf("Stopped shows %v", r.tt.Texts())
	}
	r.click(t, "All 4")
	// The filter reads the name, the host's name, the listen address and the target, any case.
	p := pageAs[*forwardsPage](r.w.pageTab("forwards"))
	for q, want := range map[string]string{"STAGING": "web back", "9000": "open bind", "3000": "web back", "socks5": "socks"} {
		r.fill(t, "Filter forwards", q)
		rows := r.w.shownTunnels(p)
		if len(rows) != 1 || rows[0].Name != want || !r.has("forward "+want) {
			t.Errorf("filter %q shows %v", q, rows)
		}
	}
}

func TestForwardSwitchStartsAndStopsAndReadsAgain(t *testing.T) {
	fb := forwardBackend()
	r := forwardRig(t, fb)
	before := fb.tunCalls
	r.click(t, "web back on")
	if !slices.Equal(fb.tunStarted, []string{"t2"}) || fb.tunCalls == before {
		t.Fatalf("started %v, reads %d→%d", fb.tunStarted, before, fb.tunCalls)
	}
	r.texts(t, "Running 3")
	r.click(t, "db local on")
	if !slices.Equal(fb.tunStopped, []string{"t1"}) {
		t.Fatalf("stopped %v", fb.tunStopped)
	}
	// A connecting forward counts as on: its switch stops it.
	r.click(t, "socks on")
	if !slices.Equal(fb.tunStopped, []string{"t1", "t3"}) {
		t.Fatalf("stopped %v", fb.tunStopped)
	}
	// Retry on a failed forward starts it.
	r.click(t, "Retry")
	if got := fb.tunStarted[len(fb.tunStarted)-1]; got != "t4" {
		t.Fatalf("Retry started %q", got)
	}
}

func TestForwardRowMenuItems(t *testing.T) {
	r := forwardRig(t, forwardBackend())
	menu := func(row string) []string {
		if err := r.tt.RightClick(row); err != nil {
			t.Fatal(err)
		}
		r.tt.Frame()
		return r.tt.Menu()
	}
	if got := menu("forward db local"); !slices.Equal(got, []string{"Stop", "Copy local address", "-", "Edit", "-", "Delete"}) {
		t.Errorf("running menu %v", got)
	}
	r.tt.CloseMenu()
	if got := menu("forward web back"); !slices.Equal(got, []string{"Start", "Copy local address", "-", "Edit", "-", "Delete"}) {
		t.Errorf("stopped menu %v", got)
	}
	if err := r.tt.ChooseMenuItem("Copy local address"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if r.tt.Clipboard() != "localhost:3000" { // a remote forward's local address is its target
		t.Errorf("clipboard %q", r.tt.Clipboard())
	}
	if got := menu("forward socks"); slices.Contains(got, "Copy local address") {
		t.Errorf("a forward on a port picked at start has no local address yet: %v", got)
	}
	r.tt.CloseMenu()
}

func TestForwardDialogNewHasTheAppsDefaultsAndSavesTheFormsBody(t *testing.T) {
	fb := forwardBackend()
	r := forwardRig(t, fb)
	r.click(t, "New forward")
	r.texts(t, "New forward", "Name", "Host", "Kind", "Bind address", "Bind port", "Dest host", "Dest port",
		"Bind 0.0.0.0 exposes the port on every interface.", "Cancel", "Save forward", "Local (−L)", "Remote (−R)", "SOCKS5 (−D)")
	d := r.w.fwdDlg
	if d.kind != "local" || d.bindAddr != "127.0.0.1" || d.bindPort != "0" || d.destHost != "127.0.0.1" || d.destPort != "5432" || d.hostID != "" {
		t.Fatalf("defaults %+v", d)
	}
	// No host picked: the form's own refusal, and nothing is sent.
	r.fill(t, "Forward name", "  new db  ")
	r.click(t, "Save forward")
	if len(fb.tunSaved) != 0 || !r.tt.HasText("Pick an SSH host first") {
		t.Fatalf("saved %v", fb.tunSaved)
	}
	// The host list has the SSH hosts, not the local shell.
	if got := r.w.hostPickEntries(""); len(got) != 3 || slices.ContainsFunc(got, func(e pickEntry) bool { return e.label == "Local Shell" }) {
		t.Errorf("the host list %v", got)
	}
	r.click(t, "Forward host picker")
	if err := r.tt.ChooseMenuItem("pg-primary"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	r.fill(t, "Bind address", "  ")
	r.fill(t, "Bind port", "5439")
	r.click(t, "Save forward")
	if len(fb.tunSaved) != 1 {
		t.Fatalf("saved %v", fb.tunSaved)
	}
	want := keelclient.TunnelInput{Name: "new db", HostID: "h-pg", Kind: "local", BindAddr: "127.0.0.1", BindPort: 5439, DestHost: "127.0.0.1", DestPort: 5432}
	if fb.tunSaved[0] != want {
		t.Errorf("body %+v, want %+v", fb.tunSaved[0], want)
	}
	if r.w.fwdDlg != nil || !r.tt.HasText("Forward saved") || !r.tt.HasText("new db") {
		t.Errorf("dialog %v, texts %v", r.w.fwdDlg, r.tt.Texts())
	}
}

func TestForwardDialogKindsShowTheirFieldsAndEditKeepsTheID(t *testing.T) {
	fb := forwardBackend()
	r := forwardRig(t, fb)
	r.click(t, "New forward")
	r.click(t, "SOCKS5 (−D)")
	if r.has("Dest host") || r.has("Dest port") {
		t.Error("SOCKS5 asks for a target")
	}
	r.click(t, "Remote (−R)")
	if !r.has("Dest host") || !r.has("Dest port") {
		t.Error("-R has no target fields")
	}
	r.click(t, "Cancel")
	if r.w.fwdDlg != nil {
		t.Fatal("Cancel left the dialog open")
	}
	if err := r.tt.RightClick("forward web back"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if err := r.tt.ChooseMenuItem("Edit"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	r.texts(t, "Edit forward")
	d := r.w.fwdDlg
	if d.id != "t2" || d.kind != "remote" || d.bindPort != "8080" || d.destHost != "localhost" || d.destPort != "3000" || d.hostID != "h-web" {
		t.Fatalf("edit %+v", d)
	}
	r.fill(t, "Forward name", "web back 2")
	r.click(t, "Save forward")
	if len(fb.tunSaved) != 1 || fb.tunSaved[0].ID != "t2" || fb.tunSaved[0].Name != "web back 2" {
		t.Fatalf("saved %+v", fb.tunSaved)
	}
	// The server's refusal shows in the dialog in its own words.
	fb.tunSaveErr = &keelclient.StatusError{Code: 400, Msg: "destHost and destPort are required"}
	r.w.editForward(fb.tunnels[1])
	r.tt.Frame()
	r.click(t, "Save forward")
	if r.w.fwdDlg == nil || !r.tt.HasText("destHost and destPort are required") {
		t.Errorf("a refused save: dialog %v", r.w.fwdDlg)
	}
}

func TestForwardDialogSaysAMissingHost(t *testing.T) {
	fb := forwardBackend()
	fb.tunnels[1].HostID = "gone"
	r := forwardRig(t, fb)
	entries := r.w.hostPickEntries("gone")
	if entries[0].label != "Missing host — pick another" || len(entries) != 4 {
		t.Errorf("entries %v", entries)
	}
	if got := newRig(t, &fakeBackend{}).w.hostPickEntries(""); len(got) != 0 {
		t.Errorf("no hosts: %v", got)
	}
}

func TestDeletingAForwardAsksFirst(t *testing.T) {
	fb := forwardBackend()
	r := forwardRig(t, fb)
	if err := r.tt.RightClick("forward web back"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if err := r.tt.ChooseMenuItem("Delete"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	r.texts(t, "Confirm", "Delete forward “web back”?")
	r.click(t, "Cancel")
	if len(fb.tunDeleted) != 0 {
		t.Fatal("Cancel deleted")
	}
	r.w.askDeleteTunnel(fb.tunnels[1])
	r.tt.Frame()
	r.click(t, "Remove")
	if !slices.Equal(fb.tunDeleted, []string{"t2"}) || r.has("forward web back") {
		t.Errorf("deleted %v", fb.tunDeleted)
	}
}

func TestFailedStartAsksForTheHostKeyAndTrustRetries(t *testing.T) {
	fb := forwardBackend()
	fb.tunStartErr = &keelclient.StatusError{Code: 409, Msg: "unknown host", ErrorKind: "unknown-host",
		Offer: keelclient.HostKey{Kind: "unknown-host", Hostname: "10.1.0.1:22", Fingerprint: "SHA256:abc", KeyType: "ssh-ed25519", Token: "t-1"}}
	r := forwardRig(t, fb)
	r.click(t, "web back on")
	r.texts(t, "Unknown host key", "10.1.0.1:22", "SHA256:abc", "Abort", "Trust and connect")
	if len(fb.trusted) != 0 {
		t.Fatal("a key was trusted before the click")
	}
	fb.tunStartErr = nil
	r.click(t, "Trust and connect")
	if len(fb.trusted) != 1 || fb.trusted[0].Fingerprint != "SHA256:abc" || fb.replaced[0] || !slices.Equal(fb.tunStarted, []string{"t2", "t2"}) {
		t.Fatalf("trusted %v replaced %v started %v", fb.trusted, fb.replaced, fb.tunStarted)
	}
	r.texts(t, "Host key saved")
	// A changed key says so, and Abort trusts nothing.
	fb.tunStartErr = &keelclient.StatusError{Code: 409, ErrorKind: "host-key-mismatch", Offer: keelclient.HostKey{Kind: "host-key-mismatch", Hostname: "h", Fingerprint: "SHA256:new", Expected: "SHA256:old"}}
	r.click(t, "Retry")
	r.texts(t, "Host key changed", "saved: SHA256:old")
	r.click(t, "Abort")
	if len(fb.trusted) != 1 || r.w.fwdKey != nil {
		t.Errorf("trusted %d, offer %v", len(fb.trusted), r.w.fwdKey)
	}
}

func TestAFailedStartThatIsNotAKeyIsAToast(t *testing.T) {
	fb := forwardBackend()
	fb.tunStartErr = &keelclient.StatusError{Code: 400, Msg: "listen tcp 127.0.0.1:8080: bind: address already in use"}
	r := forwardRig(t, fb)
	r.click(t, "web back on")
	r.texts(t, "listen tcp 127.0.0.1:8080: bind: address already in use")
	if r.w.fwdKey != nil {
		t.Error("a bind failure asked about a key")
	}
}

func TestSidebarForwardsSectionAndStatusBar(t *testing.T) {
	fb := forwardBackend()
	r := newRig(t, fb)
	r.tt.Frame()
	r.texts(t, "keel serve · 0 sessions", "1 forward") // only db local is ready; a connecting one is not counted
	r.click(t, "Forwards")
	r.texts(t, "db local", "127.0.0.1:5433 → 127.0.0.1:5432", "web back", "socks", "Manage forwards", "New forward")
	if !r.has("sidebar forward db local") || !r.has("Filter forwards") {
		t.Errorf("texts %v", r.tt.Texts())
	}
	r.fill(t, "Filter forwards", "staging")
	if !r.has("sidebar forward web back") || r.has("sidebar forward db local") {
		t.Errorf("the filter reads the host's name: %v", r.tt.Texts())
	}
	r.fill(t, "Filter forwards", "zzz")
	r.texts(t, "Nothing matches “zzz”")
	r.fill(t, "Filter forwards", "")
	r.click(t, "web back on") // the section's switch
	if !slices.Equal(fb.tunStarted, []string{"t2"}) {
		t.Fatalf("started %v", fb.tunStarted)
	}
	r.texts(t, "2 forwards")
	r.click(t, "Manage forwards")
	if r.w.pageTab("forwards") == nil {
		t.Fatal("Manage forwards did not open the page")
	}
	for _, t2 := range fb.tunnels {
		fb.setTunnel(t2.ID, "stopped")
	}
	r.w.reloadTunnels()
	r.tt.Frame()
	if r.has("status forwards") {
		t.Error("the status bar shows forwards with none running")
	}
}

func TestSidebarForwardsEmpty(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.click(t, "Forwards")
	r.texts(t, "No forwards yet")
	r.click(t, "New forward")
	r.texts(t, "New forward")
}

func TestForwardsReadEveryTwoSecondsOnlyWhileShown(t *testing.T) {
	fb := forwardBackend()
	r := newRig(t, fb)
	r.tt.Frame()
	n := fb.tunCalls
	r.w.tunnelTick()
	if fb.tunCalls != n {
		t.Fatal("read with nothing showing")
	}
	r.w.showPage("forwards")
	r.tt.Frame()
	n = fb.tunCalls
	fb.setTunnel("t2", "ready")
	r.w.tunnelTick()
	r.tt.Frame()
	if fb.tunCalls != n+1 || !r.tt.HasText("Running 3") {
		t.Errorf("reads %d→%d, texts %v", n, fb.tunCalls, r.tt.Texts())
	}
	if forwardsEvery != 2*time.Second {
		t.Errorf("forwardsEvery %v", forwardsEvery)
	}
}

func TestForwardsAreInThePaletteAndNewIsTheirs(t *testing.T) {
	r := newRig(t, forwardBackend())
	r.tt.Frame()
	r.openPal(t)
	r.typePal("port forwarding")
	rows := r.rows()
	if len(rows) != 1 || rows[0].title != "Port forwarding" || rows[0].group != "Actions" {
		t.Fatalf("rows %+v", rows)
	}
	rows[0].run(false)
	r.w.closePalette()
	r.tt.Frame()
	if r.w.pageTab("forwards") == nil {
		t.Fatal("the palette did not open the page")
	}
	// ⌘N on the page is New forward, not New host.
	r.w.newItem()
	r.tt.Frame()
	if r.w.fwdDlg == nil || r.w.hostDlg != nil {
		t.Errorf("⌘N: forward %v host %v", r.w.fwdDlg, r.w.hostDlg)
	}
}
