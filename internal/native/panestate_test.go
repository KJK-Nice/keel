package native

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// failedRig is a window whose only session was already down when it opened.
func failedRig(t *testing.T, st keelclient.State, hostID string) *rig {
	t.Helper()
	fb := okBackend()
	fb.sessions = []keelclient.Session{{ID: "s-x", HostID: hostID, Status: string(st.Status)}}
	r := newRig(t, fb)
	r.fb.conns[0].st = &st
	r.tt.Frame()
	return r
}

// has reports whether an element is labelled label; r.tt.HasText is for what is drawn as text.
func (r *rig) has(label string) bool { _, ok := r.tt.Find(label); return ok }

func TestFailedSessionFromStartShowsABannerWithCloseAndReconnect(t *testing.T) {
	r := failedRig(t, keelclient.State{Status: "error", Message: "dial tcp: connection refused"}, "h-api")
	if r.w.tabCount() != 1 {
		t.Fatalf("tabs %d: a failed session is hidden", r.w.tabCount())
	}
	for _, s := range []string{"Connection failed", "SSH 10.0.0.1:22", "dial tcp: connection refused"} {
		if !r.tt.HasText(s) {
			t.Errorf("no text %q", s)
		}
	}
	if !r.has("banner failed") || !r.has("Close") || !r.has("Reconnect") {
		t.Fatal("the failure banner lacks its buttons")
	}
	r.click(t, "Reconnect")
	if !slices.Equal(r.fb.reconnected, []string{"s-x"}) || len(r.fb.killed) != 0 {
		t.Fatalf("reconnected %v killed %v", r.fb.reconnected, r.fb.killed)
	}
	r.click(t, "Close")
	if !slices.Equal(r.fb.killed, []string{"s-x"}) || r.w.tabCount() != 1 || !r.w.tabAt(0).isNew() {
		t.Fatalf("killed %v tabs %d: closing the last tab leaves a New tab", r.fb.killed, r.w.tabCount())
	}
}

// The failure banner has Copy logs between nothing and
// Close, the pane lists the connection log under it, and Copy logs puts exactly those lines on the clipboard.
func TestAFailedPaneListsItsConnectionLogAndCopyLogsCopiesIt(t *testing.T) {
	st := keelclient.State{Status: "error", Message: "dial tcp: connection refused", Log: []keelclient.LogLine{
		{Kind: "work", Text: "Connecting…"}, {Kind: "fail", Text: "dial tcp: connection refused"}}}
	r := failedRig(t, st, "h-api")
	lines := []string{
		`Starting a new connection to: "10.0.0.1" port "22"`, `Starting address resolution of "10.0.0.1"`, "Address resolution finished",
		`Connecting to "10.0.0.1" port "22"`, "Connecting…", "dial tcp: connection refused",
	}
	for _, l := range lines {
		if !r.tt.HasText(l) {
			t.Errorf("the connection log lacks %q", l)
		}
	}
	if !r.has("connection log 1") || !r.has("Copy logs") {
		t.Fatal("no log or no Copy logs")
	}
	r.click(t, "Copy logs")
	if got := r.tt.Clipboard(); got != strings.Join(lines, "\n") {
		t.Fatalf("clipboard %q", got)
	}
	if !r.tt.HasText("Copied connection log") {
		t.Error("no toast")
	}
	// A local shell is seeded with its own line; a connecting SSH pane has the log and Copy logs, and no Reconnect.
	r2 := failedRig(t, keelclient.State{Status: "exited"}, "h-local")
	if !r2.tt.HasText("Starting a local shell") {
		t.Error("the local shell's seed line is missing")
	}
	r3 := failedRig(t, keelclient.State{Status: "connecting"}, "h-api")
	if !r3.has("Copy logs") || r3.has("Reconnect") || !r3.tt.HasText(`Connecting to "10.0.0.1" port "22"`) {
		t.Error("the connecting pane lacks its log or Copy logs, or offers Reconnect")
	}
	// A ready session, a local shell that is connecting and a joined tab show no log.
	for _, st := range []keelclient.State{{Status: "ready"}} {
		if failedRig(t, st, "h-api").has("connection log 1") {
			t.Errorf("%+v: a log shows", st)
		}
	}
}

func TestLogTextIsTheLinesOrTheEmptyWords(t *testing.T) {
	if got := logText(nil); got != "No connection log yet." {
		t.Error(got)
	}
	if got := logText([]keelclient.LogLine{{Text: "a"}, {Text: ""}, {Text: "b"}}); got != "a\nb" {
		t.Errorf("%q", got)
	}
}

func TestBannerHeadlinesAndEndpoints(t *testing.T) {
	for _, tc := range []struct {
		name     string
		st       keelclient.State
		host     string
		words    []string
		banner   string
		reconect bool
	}{
		{"exited", keelclient.State{Status: "exited"}, "h-api", []string{"Session ended", "SSH 10.0.0.1:22"}, "banner failed", true},
		{"lost", keelclient.State{Status: "error", WasReady: true}, "h-web", []string{"Connection lost", "SSH 10.1.0.1:22"}, "banner failed", true},
		{"local failed", keelclient.State{Status: "error", Subtitle: "sh in /tmp"}, "h-local", []string{"Connection failed", "sh in /tmp"}, "banner failed", true},
		{"local no subtitle", keelclient.State{Status: "exited"}, "h-local", []string{"Session ended", "Local shell"}, "banner failed", true},
		{"connecting", keelclient.State{Status: "connecting"}, "h-api", []string{"Connecting…", "SSH 10.0.0.1:22"}, "banner connecting", false},
	} {
		r := failedRig(t, tc.st, tc.host)
		for _, s := range tc.words {
			if !r.tt.HasText(s) {
				t.Errorf("%s: no text %q", tc.name, s)
			}
		}
		if !r.has(tc.banner) || r.has("Reconnect") != tc.reconect {
			t.Errorf("%s: banner %v, Reconnect %v", tc.name, r.has(tc.banner), r.has("Reconnect"))
		}
	}
	// A local shell that is still connecting, or a ready session, has no banner.
	for _, st := range []keelclient.State{{Status: "ready"}, {Status: "connecting"}} {
		host := "h-api"
		if st.Status == "connecting" {
			host = "h-local"
		}
		r := failedRig(t, st, host)
		if r.has("banner failed") || r.has("banner connecting") || r.has("banner following") {
			t.Errorf("%+v: unexpected banner", st)
		}
	}
}

var unknownKey = keelclient.HostKey{Kind: "unknown-host", Hostname: "10.0.0.1:22", Fingerprint: "SHA256:fp-offered", KeyType: "ssh-ed25519", Token: "t-offer"}

// hostKeyRig has two tabs: s-a refused for its host key, s-b fine and focused.
func hostKeyRig(t *testing.T, k keelclient.HostKey) *rig {
	t.Helper()
	fb := okBackend()
	fb.sessions = []keelclient.Session{{ID: "s-a", HostID: "h-api", Status: "error"}, {ID: "s-b", HostID: "h-pg", Status: "ready"}}
	r := newRig(t, fb)
	r.fb.conns[0].st = &keelclient.State{Status: "error", HostKey: k}
	r.tt.Frame()
	return r
}

func TestHostKeyDialogOnlyForTheFocusedPaneThenTrustReconnects(t *testing.T) {
	r := hostKeyRig(t, unknownKey)
	if r.has("dialog Unknown host key") {
		t.Fatal("a pane that is not on screen opened the dialog")
	}
	// Two panes of one tab: the unfocused one shows its failure banner only.
	r2 := newRig(t, okBackend())
	r2.tt.Frame()
	r2.click(t, "host api-1")
	r2.click(t, "Split right") // pane 2 is focused
	r2.fb.conns[0].st = &keelclient.State{Status: "error", HostKey: unknownKey}
	r2.tt.Frame()
	if r2.has("dialog Unknown host key") || !r2.has("banner failed") {
		t.Fatal("an unfocused pane must show its failure banner only")
	}
	r.click(t, "tab api-1") // focus the refused pane
	for _, s := range []string{"Unknown host key", "10.0.0.1:22", "SHA256:fp-offered", "ssh-ed25519", "Abort", "Trust and connect"} {
		if !r.tt.HasText(s) {
			t.Errorf("dialog lacks %q", s)
		}
	}
	if len(r.fb.trusted) != 0 {
		t.Fatal("nothing may be trusted before the click")
	}
	r.click(t, "Trust and connect")
	if len(r.fb.trusted) != 1 || r.fb.trusted[0] != unknownKey || r.fb.replaced[0] {
		t.Fatalf("trusted %v replace %v: not the offer", r.fb.trusted, r.fb.replaced)
	}
	if !slices.Equal(r.fb.reconnected, []string{"s-a"}) {
		t.Errorf("reconnected %v", r.fb.reconnected)
	}
	if !slices.Equal(r.fb.calls[len(r.fb.calls)-2:], []string{"trust", "reconnect s-a"}) {
		t.Errorf("order %v", r.fb.calls)
	}
	if r.has("dialog Unknown host key") {
		t.Error("the dialog stays after Trust")
	}
}

func TestHostKeyDialogEscapeCancelsAndLeavesTheBanner(t *testing.T) {
	r := hostKeyRig(t, unknownKey)
	r.click(t, "tab api-1")
	if !r.has("dialog Unknown host key") {
		t.Fatal("no dialog")
	}
	r.tt.Key(0, ui.KeyEscape)
	r.tt.Frame()
	if r.has("dialog Unknown host key") || !r.has("banner failed") || !r.has("Reconnect") {
		t.Error("Escape must close the dialog and leave the failure banner")
	}
	if len(r.fb.trusted) != 0 || len(r.fb.reconnected) != 0 {
		t.Errorf("Escape trusted %v reconnected %v", r.fb.trusted, r.fb.reconnected)
	}
	r.tt.Frame()
	if r.has("dialog Unknown host key") {
		t.Error("the dialog came back for the same offer")
	}
	// A new offer (a different fingerprint) asks again.
	k2 := unknownKey
	k2.Fingerprint = "SHA256:other"
	r.fb.conns[0].st.HostKey = k2
	r.tt.Frame()
	if !r.has("dialog Unknown host key") {
		t.Error("a new offer did not open the dialog")
	}
}

func TestHostKeyDialogAbortButtonCancels(t *testing.T) {
	r := hostKeyRig(t, unknownKey)
	r.click(t, "tab api-1")
	r.click(t, "Abort")
	if r.has("dialog Unknown host key") || len(r.fb.trusted) != 0 || len(r.fb.reconnected) != 0 {
		t.Error("Abort must only close the dialog")
	}
}

func TestChangedHostKeyDialogReplaces(t *testing.T) {
	k := unknownKey
	k.Kind, k.Expected = "host-key-mismatch", "SHA256:saved"
	r := hostKeyRig(t, k)
	r.click(t, "tab api-1")
	for _, s := range []string{"Host key changed", "saved: SHA256:saved", "SHA256:fp-offered"} {
		if !r.tt.HasText(s) {
			t.Errorf("dialog lacks %q", s)
		}
	}
	r.click(t, "Trust and connect")
	if len(r.fb.trusted) != 1 || !r.fb.replaced[0] {
		t.Fatalf("trusted %v replace %v", r.fb.trusted, r.fb.replaced)
	}
}

// A role chip has a glyph before its words, steering for who drives and the owner, an eye for a
// follower. The glyph is 0.923rem, before the words, inside the pill.
func TestARoleChipHasItsGlyph(t *testing.T) {
	two := []keelclient.Peer{{ID: 1, Name: "ann", Kind: keelclient.KindWindow, Role: "driver"}, {ID: 2, Name: "me", Kind: keelclient.KindWindow, Role: "follower"}}
	for _, tc := range []struct {
		label, glyph string
		st           keelclient.State
	}{
		{"role Driving", "steering", keelclient.State{Status: "ready", Role: "driver", Attached: two, OwnerID: 1, YouID: 2, DriverID: 2}},
		{"role Following", "eye", keelclient.State{Status: "ready", Role: "follower", Attached: two, OwnerID: 1, YouID: 2, DriverID: 1}},
		{"role Owner", "steering", keelclient.State{Status: "ready", Role: "owner", Attached: two, OwnerID: 2, YouID: 2, DriverID: 1}},
	} {
		r := failedRig(t, tc.st, "h-api")
		chip, ok := r.tt.Find(tc.label)
		g, ok2 := r.tt.Find("role glyph " + tc.glyph)
		if !ok || !ok2 {
			t.Fatalf("%s: chip %v, glyph %q %v", tc.label, ok, tc.glyph, ok2)
		}
		// 12 px (0.923rem), 6 px in from the chip's left edge (7 with the owner's 1 px ring), centred in it, before the words.
		if d := g.X - (chip.X + space1_5); d < -0.6 || d > 1.6 || g.W < iconSizeSm-0.1 || g.W > iconSizeSm+0.1 || g.Y < chip.Y || g.Y+g.H > chip.Y+chip.H {
			t.Errorf("%s: glyph %v in chip %v", tc.label, g, chip)
		}
	}
}

func TestRoleChipAndFollowerBannerOnlyWhenShared(t *testing.T) {
	two := []keelclient.Peer{{ID: 1, Name: "ann", Kind: keelclient.KindWindow, Role: "driver"}, {ID: 2, Name: "me", Kind: keelclient.KindWindow, Role: "follower"}}
	for _, tc := range []struct {
		name      string
		st        keelclient.State
		chipLabel string // "" when no chip
		button    string // "" when no follower banner
		notice    string
	}{
		{"alone", keelclient.State{Status: "ready", Role: "driver", Attached: two[:1], OwnerID: 1, YouID: 1, DriverID: 1}, "", "", ""},
		{"driving", keelclient.State{Status: "ready", Role: "driver", Attached: two, OwnerID: 1, YouID: 2, DriverID: 2}, "role Driving", "", ""},
		{"following", keelclient.State{Status: "ready", Role: "follower", Attached: two, OwnerID: 1, YouID: 2, DriverID: 1}, "role Following", "Ask for control", "Following · ann is driving · read-only"},
		{"owner", keelclient.State{Status: "ready", Role: "owner", Attached: two, OwnerID: 2, YouID: 2, DriverID: 1}, "role Owner", "Take control", "Following · ann is driving · read-only"},
		{"nobody", keelclient.State{Status: "ready", Role: "follower", Attached: two, OwnerID: 1, YouID: 2}, "role Following", "Ask for control", "Following · Nobody is driving · read-only"},
	} {
		r := failedRig(t, tc.st, "h-api")
		for _, l := range []string{"role Driving", "role Following", "role Owner"} {
			if r.has(l) != (l == tc.chipLabel) {
				t.Errorf("%s: chip %q present=%v", tc.name, l, r.has(l))
			}
		}
		for _, b := range []string{"Take control", "Ask for control"} {
			if r.has(b) != (b == tc.button) {
				t.Errorf("%s: button %q present=%v", tc.name, b, r.has(b))
			}
		}
		if r.has("banner following") != (tc.button != "") || tc.notice != "" && !r.tt.HasText(tc.notice) {
			t.Errorf("%s: follower banner present=%v, notice %q", tc.name, r.has("banner following"), tc.notice)
		}
	}
	r := failedRig(t, keelclient.State{Status: "ready", Role: "follower", Attached: two, OwnerID: 1, YouID: 2, DriverID: 1}, "h-api")
	r.click(t, "Ask for control")
	if c := r.fb.conns[0]; c.asked != 1 || c.took != 0 {
		t.Errorf("Ask for control: asked %d took %d", c.asked, c.took)
	}
	r = failedRig(t, keelclient.State{Status: "ready", Role: "owner", Attached: two, OwnerID: 2, YouID: 2, DriverID: 1}, "h-api")
	r.click(t, "Take control")
	if c := r.fb.conns[0]; c.took != 1 || c.asked != 0 {
		t.Errorf("Take control: asked %d took %d", c.asked, c.took)
	}
}
