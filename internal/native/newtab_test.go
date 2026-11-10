package native

import (
	"encoding/json"
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

// ---- the rules of lib/newtab.js (cases of test/newtab.test.mjs) ----

func hostsNamed(ids ...string) (out []keelclient.Host) {
	for _, id := range ids {
		out = append(out, keelclient.Host{ID: id, Name: "host-" + id, Address: id + ".example", Port: 22, Username: "u"})
	}
	return
}

func idsOf(hs []keelclient.Host) (out []string) {
	for _, h := range hs {
		out = append(out, h.ID)
	}
	return
}

// "recent cards are the recent hosts plus the local shell, at most six"
func TestRecentCardsAreTheRecentHostsPlusTheLocalShell(t *testing.T) {
	hosts := hostsNamed("a", "b", "c", "d", "e", "f", "g", "h")
	local := keelclient.Host{ID: "loc", Name: "Local Shell", AuthType: "local"}
	all := append(slices.Clone(hosts), local)
	got := pickCards(all, []string{"c", "a", "gone", "b", "d", "e", "f", "g"}, "", &local)
	if want := []string{"loc", "c", "a", "b", "d", "e"}; !slices.Equal(idsOf(got), want) {
		t.Errorf("%v, want %v", idsOf(got), want)
	}
	// "with no recent hosts the local shell is the only card"
	if got := pickCards(all, nil, "", &local); !slices.Equal(idsOf(got), []string{"loc"}) {
		t.Errorf("no recents: %v", idsOf(got))
	}
	if got := pickCards(nil, nil, "", nil); len(got) != 0 {
		t.Errorf("nothing: %v", idsOf(got))
	}
}

// "a query filters every host, recent ones first"
func TestRecentCardsForAQuery(t *testing.T) {
	hosts := hostsNamed("a", "b", "c", "d", "e", "f", "g", "h")
	local := keelclient.Host{ID: "loc", Name: "Local Shell", AuthType: "local"}
	got := pickCards(append(slices.Clone(hosts), local), []string{"c"}, "host-", &local)
	if got[0].ID != "c" || len(got) != recentCards {
		t.Errorf("%v", idsOf(got))
	}
	if got := pickCards(hosts, nil, "host-b", nil); !slices.Equal(idsOf(got), []string{"b"}) {
		t.Errorf("%v", idsOf(got))
	}
	if got := pickCards(hosts, nil, "nothing", nil); len(got) != 0 {
		t.Errorf("%v", idsOf(got))
	}
}

// "ageLabel"
func TestAgeLabel(t *testing.T) {
	now := time.Unix(10_000_000, 0)
	for _, c := range []struct {
		ago  time.Duration
		want string
	}{
		{5 * time.Second, "just now"}, {12 * time.Minute, "12 min ago"}, {3 * time.Hour, "3 h ago"},
		{30 * time.Hour, "yesterday"}, {5 * 24 * time.Hour, "5 days ago"}, {-5 * time.Second, "just now"},
	} {
		if got := ageLabel(now.Add(-c.ago), now); got != c.want {
			t.Errorf("%v ago: %q, want %q", c.ago, got, c.want)
		}
	}
}

// ---- the page in a window ----

// startRig is a window on nine hosts with the local shell; the page is the empty window's.
func startRig(t *testing.T) *rig {
	hosts := []keelclient.Host{{ID: "h-local", Name: "Local Shell", Address: "localhost", Username: "me", AuthType: "local", Color: "var(--ansi-10)"}}
	for i := 1; i <= 8; i++ {
		hosts = append(hosts, keelclient.Host{ID: fmt.Sprintf("h%d", i), Name: fmt.Sprintf("srv-%d", i), Address: fmt.Sprintf("10.0.0.%d", i), Username: "ops", AuthType: "key", Group: "fleet"})
	}
	r := newRig(t, &fakeBackend{hosts: hosts})
	r.tt.Frame()
	return r
}

func (r *rig) onPage(t *testing.T) {
	t.Helper()
	if tb := r.w.tab(); tb == nil || !tb.isNew() || !r.tt.HasText("Where to?") {
		t.Fatalf("the New tab page is not showing (tabs %d)", len(r.w.tabs))
	}
}

func cardNames(r *rig) (out []string) {
	for _, h := range r.w.cards(r.w.tab().page) {
		out = append(out, h.Name)
	}
	return
}

func TestEmptyWindowShowsTheNewTabPage(t *testing.T) {
	r := startRig(t)
	r.onPage(t)
	if len(r.w.tabs) != 1 || !r.tt.Focused("Host to connect to") {
		t.Fatalf("tabs %d, the input has the keyboard: %v", len(r.w.tabs), r.tt.Focused("Host to connect to"))
	}
	for _, s := range []string{"Where to?", "Recent", "Local Shell", "Start", "Quick connect", "New host", "New tab"} {
		if !r.tt.HasText(s) {
			t.Errorf("no %q on the page", s)
		}
	}
	// Join session works in a later phase: it is not offered.
	if r.tt.HasText("Join session") {
		t.Error("an action that does not work is offered")
	}
}

func TestNewTabFromShortcutPlusLastClosedAndRestore(t *testing.T) {
	r := startRig(t)
	r.click(t, "start card Local Shell") // the page's tab becomes a session
	if r.w.tab().isNew() || len(r.w.tabs) != 1 {
		t.Fatalf("the card did not fill the page's tab: tabs %d", len(r.w.tabs))
	}
	r.press(ui.Cmd, ui.KeyT)
	r.onPage(t)
	if len(r.w.tabs) != 2 {
		t.Fatalf("⌘T: tabs %d", len(r.w.tabs))
	}
	r.click(t, "New tab") // the + button, which the ⌘T lock does not hold back
	if len(r.w.tabs) != 3 || !r.w.tabs[2].isNew() {
		t.Fatalf("+: tabs %d", len(r.w.tabs))
	}
	r.press(ui.Cmd, ui.KeyW) // closes the New tab
	r.press(ui.Cmd, ui.KeyW)
	r.press(ui.Cmd, ui.KeyW) // the session's tab: the window keeps a tab
	r.onPage(t)
	if len(r.w.tabs) != 1 {
		t.Fatalf("after closing everything: tabs %d", len(r.w.tabs))
	}
	r.press(ui.Cmd, ui.KeyW) // the lone New tab stays
	if len(r.w.tabs) != 1 {
		t.Fatalf("⌘W closed the lone New tab")
	}
}

func TestHoldingCmdTMakesOneTab(t *testing.T) {
	r := startRig(t)
	r.press(ui.Cmd, ui.KeyT)
	r.press(ui.Cmd, ui.KeyT) // a key repeat inside the lock
	if len(r.w.tabs) != 2 {
		t.Fatalf("tabs %d, want 2", len(r.w.tabs))
	}
}

func TestCardsForZeroThreeAndEightRecents(t *testing.T) {
	r := startRig(t)
	if got := cardNames(r); !slices.Equal(got, []string{"Local Shell"}) {
		t.Fatalf("no recents: %q", got)
	}
	r.w.recentIDs = []string{"h3", "h1", "h2"}
	r.tt.Frame()
	if got := cardNames(r); !slices.Equal(got, []string{"Local Shell", "srv-3", "srv-1", "srv-2"}) {
		t.Fatalf("three recents: %q", got)
	}
	for _, s := range []string{"⌥1", "⌥4", "10.0.0.3"} {
		if !r.tt.HasText(s) {
			t.Errorf("no %q on the cards", s)
		}
	}
	r.w.recentIDs = []string{"h8", "h7", "h6", "h5", "h4", "h3", "h2", "h1"}
	r.tt.Frame()
	if got := cardNames(r); !slices.Equal(got, []string{"Local Shell", "srv-8", "srv-7", "srv-6", "srv-5", "srv-4"}) {
		t.Fatalf("eight recents: %q", got)
	}
	if r.tt.HasText("⌥7") || !r.tt.HasText("⌥6") {
		t.Error("only the first six cards have a number")
	}
}

func TestAltNumberOpensThatCardsHost(t *testing.T) {
	r := startRig(t)
	r.w.recentIDs = []string{"h3", "h1"}
	r.tt.Frame()
	r.press(ui.Alt, ui.Key2) // the second card: Local Shell is first
	if !slices.Equal(r.fb.spawned, []string{"h3"}) || len(r.w.tabs) != 1 || r.w.tab().isNew() {
		t.Fatalf("spawned %v tabs %d", r.fb.spawned, len(r.w.tabs))
	}
	// A session tab has no cards: ⌥1 does nothing there.
	r.press(ui.Alt, ui.Key1)
	if len(r.fb.spawned) != 1 {
		t.Errorf("⌥1 outside the page spawned %v", r.fb.spawned)
	}
}

func TestTypingFiltersTheCardsAndEnterOpensTheHighlightedOne(t *testing.T) {
	r := startRig(t)
	r.tt.Type("srv-")
	r.tt.Frame()
	if got := cardNames(r); len(got) != recentCards || got[0] != "srv-1" || !r.tt.HasText("Hosts") {
		t.Fatalf("cards %q", got)
	}
	r.tt.Type("7")
	r.tt.Frame()
	if got := cardNames(r); !slices.Equal(got, []string{"srv-7"}) {
		t.Fatalf("cards %q", got)
	}
	r.tt.Type("9")
	r.tt.Frame()
	if len(cardNames(r)) != 0 || !r.tt.HasText("No saved host matches “srv-79”.") {
		t.Fatal("no empty message")
	}
	r.press(0, ui.KeyBackspace)
	r.press(0, ui.KeyBackspace)
	r.press(0, ui.KeyDown) // srv-1, srv-2, ...: ↓ highlights the next card
	if r.w.tab().page.pick != "h2" {
		t.Fatalf("pick %q", r.w.tab().page.pick)
	}
	r.press(0, ui.KeyEnter)
	if !slices.Equal(r.fb.spawned, []string{"h2"}) {
		t.Fatalf("spawned %v", r.fb.spawned)
	}
}

func TestAPastedTargetIsOfferedNotConnected(t *testing.T) {
	r := startRig(t)
	r.tt.Type("deploy@10.0.0.5:2222")
	r.tt.Frame()
	for _, s := range []string{"Read as", "deploy@10.0.0.5:2222", "Nothing has run.", "Connect once"} {
		if !r.tt.HasText(s) {
			t.Errorf("the offer lacks %q", s)
		}
	}
	r.press(0, ui.KeyEnter)
	if len(r.fb.spawned) != 0 || len(r.fb.created) != 0 {
		t.Fatalf("↵ on a read command connected or saved: spawned %v created %d", r.fb.spawned, len(r.fb.created))
	}
}

func TestAPastedCommandListsWhatWasLeftOut(t *testing.T) {
	r := startRig(t)
	r.tt.Type("ssh -p 2222 -o StrictHostKeyChecking=no -L 1:x:2 -i ~/.ssh/k deploy@10.0.0.5 uptime")
	r.tt.Frame()
	for _, s := range []string{
		"Read as", "deploy@10.0.0.5:2222", "Left out (3), not applied", "-o StrictHostKeyChecking=no", "changes host key trust",
		"-L 1:x:2", "opens a port forward; add it in Forwards", "uptime", "remote command; Keel opens a shell and does not run it",
		"Key file ~/.ssh/k was not read.",
	} {
		if !r.tt.HasText(s) {
			t.Errorf("the offer lacks %q", s)
		}
	}
	if len(r.fb.spawned) != 0 {
		t.Error("a pasted command started a session")
	}
	// Without a user there is no Connect once.
	r.tt.Frame()
	for i := 0; i < len("ssh -p 2222 -o StrictHostKeyChecking=no -L 1:x:2 -i ~/.ssh/k deploy@10.0.0.5 uptime"); i++ {
		r.tt.Key(0, ui.KeyBackspace)
	}
	r.tt.Type("ssh prod-db")
	r.tt.Frame()
	if r.tt.HasText("Connect once") || !r.tt.HasText("Add a user (user@host) to connect once.") {
		t.Error("an offer without a user shows Connect once")
	}
}

func TestAnInvalidInputShowsTheError(t *testing.T) {
	r := startRig(t)
	r.tt.Type("ssh host; rm -rf /")
	r.tt.Frame()
	if !r.tt.HasText("Shell operators are not allowed") || r.tt.HasText("Read as") || r.tt.HasText("Connect once") {
		t.Error("the refused command is not shown as an error")
	}
	// A half-typed user@host is not an error until ↵.
	r = startRig(t)
	r.tt.Type("deploy@-bad")
	r.tt.Frame()
	if r.tt.HasText("Host name has characters Keel does not accept") {
		t.Fatal("a half-typed target is already an error")
	}
	r.press(0, ui.KeyEnter)
	if !r.tt.HasText("Host name has characters Keel does not accept") {
		t.Error("↵ does not show the error")
	}
	if len(r.fb.spawned) != 0 {
		t.Error("a refused input started a session")
	}
}

func TestConnectOnceAsksForThePasswordAndSavesAQuickHost(t *testing.T) {
	r := startRig(t)
	dir := t.TempDir()
	r.w.dataDir = dir
	r.tt.Type("ssh -p 2222 -i ~/.ssh/k deploy@10.0.0.5")
	r.tt.Frame()
	r.click(t, "Connect once")
	if r.w.quick == nil || !r.hasText("dialog Quick connect") || !r.tt.HasText("Key file ~/.ssh/k was not read.") {
		t.Fatal("Connect once did not open the Quick connect dialog with the note")
	}
	if r.w.quick.target != "deploy@10.0.0.5:2222" || r.w.quick.password != "" || !r.tt.Focused("Password") {
		t.Fatalf("target %q, password %q, password focused %v", r.w.quick.target, r.w.quick.password, r.tt.Focused("Password"))
	}
	if len(r.fb.created) != 0 || len(r.fb.spawned) != 0 {
		t.Fatal("something was saved or started before the user pressed Connect")
	}
	const secret = "correct-horse-battery-staple"
	r.tt.Type(secret)
	r.tt.Frame()
	r.press(0, ui.KeyEnter)
	want := keelclient.NewHost{
		Name: "deploy@10.0.0.5", Group: "Quick connect", Address: "10.0.0.5", Port: 2222, Username: "deploy",
		AuthType: "password", Password: secret, Color: "var(--ansi-12)",
	}
	if len(r.fb.created) != 1 || r.fb.created[0] != want {
		t.Fatalf("created %v", r.fb.created)
	}
	if strings.Contains(r.fb.created[0].String(), secret) || strings.Contains(fmt.Sprintf("%v %+v", r.fb.created[0], r.fb.created[0]), secret) {
		t.Error("a NewHost prints its password")
	}
	if !slices.Equal(r.fb.spawned, []string{"h-new1"}) || r.w.quick != nil || r.w.tab().isNew() {
		t.Fatalf("spawned %v, dialog open %v, still a page %v", r.fb.spawned, r.w.quick != nil, r.w.tab().isNew())
	}
	// The password lives in the request only: not in the window, the prefs or the saved workspace.
	r.w.saveNow()
	r.w.work(func() {})
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	for _, f := range files {
		if b, _ := os.ReadFile(f); strings.Contains(string(b), secret) {
			t.Errorf("%s holds the password", filepath.Base(f))
		}
	}
	if r.hasText(secret) {
		t.Error("the password is drawn")
	}
}

func TestQuickConnectRefusesWhatIsNotUserAtHost(t *testing.T) {
	r := startRig(t)
	r.press(ui.Cmd|ui.Shift, ui.KeyO)
	if r.w.quick == nil || !r.tt.Focused("Target") {
		t.Fatal("⌘⇧O did not open Quick connect with the Target field focused")
	}
	for _, bad := range []string{"just-a-host", "ssh host; id", "a@b c", "ssh -J bastion user@host"} {
		r.w.quick.target = bad
		r.click(t, "Connect")
		if r.w.quick == nil || !r.tt.HasText("Could not parse user@host") || len(r.fb.created) != 0 {
			t.Errorf("%q was accepted", bad)
		}
	}
	r.press(0, ui.KeyEscape)
	if r.w.quick != nil {
		t.Error("Esc did not close the dialog")
	}
}

func (r *rig) hasText(s string) bool { _, ok := r.tt.Find(s); return r.tt.HasText(s) || ok }

func TestRecentlyClosedRowsReopenAsANewSession(t *testing.T) {
	r := startRig(t)
	r.click(t, "start card Local Shell")
	r.press(ui.Cmd, ui.KeyT)
	r.press(ui.Cmd, ui.KeyW)
	r.click(t, "host srv-2")
	r.press(ui.Cmd, ui.KeyW) // the srv-2 tab closes; Local Shell's tab is left
	r.press(ui.Cmd, ui.KeyW)
	r.onPage(t)
	for _, s := range []string{"Recently closed", "reopens as a new session", "srv-2", "Local Shell", "just now", "⌘⇧T"} {
		if !r.tt.HasText(s) {
			t.Errorf("no %q in Recently closed", s)
		}
	}
	spawned := len(r.fb.spawned)
	r.click(t, "reopen srv-2")
	if len(r.fb.spawned) != spawned+1 || r.fb.spawned[spawned] != "h2" || r.w.tab().isNew() {
		t.Fatalf("spawned %v", r.fb.spawned)
	}
	if len(r.w.closedList()) != 1 {
		t.Errorf("the reopened entry is still listed: %d", len(r.w.closedList()))
	}
}

func TestANewTabIsNotSaved(t *testing.T) {
	r := startRig(t)
	dir := t.TempDir()
	r.w.dataDir = dir
	r.click(t, "start card Local Shell")
	r.press(ui.Cmd, ui.KeyT)
	r.onPage(t)
	r.w.saveNow()
	doc := loadSaved(dir)
	if doc == nil || len(doc.Tabs) != 1 {
		t.Fatalf("saved %+v: the session tab only", doc)
	}
	// Only the New tab: nothing to restore, and the file says so.
	r.press(ui.Cmd, ui.KeyW)
	r.press(ui.Cmd, ui.KeyW)
	r.w.saveNow()
	if doc := loadSaved(dir); doc == nil || len(doc.Tabs) != 0 {
		t.Fatalf("saved %+v", doc)
	}
}

func TestRecentHostsSurviveAPrefsRoundTripAndHoldOnlyIDs(t *testing.T) {
	dir := t.TempDir()
	if err := savePrefs(dir, map[string]any{"termSize": 14, "extra": map[string]int{"keep": 1}}); err != nil {
		t.Fatal(err)
	}
	r := startRig(t)
	r.w.dataDir = dir
	for _, id := range []string{"h1", "h2", "h3", "h1"} {
		r.w.rememberRecent(id)
	}
	if got := loadRecentHosts(dir); !slices.Equal(got, []string{"h1", "h3", "h2"}) {
		t.Fatalf("recent %v", got)
	}
	var f map[string]json.RawMessage
	b, _ := os.ReadFile(filepath.Join(dir, "keel-native.json"))
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if string(f["recentHosts"]) == "" || f["extra"] == nil || loadPrefs(dir).TermSize != 14 {
		t.Errorf("another field was lost: %s", b)
	}
	var ids []string
	if json.Unmarshal(f["recentHosts"], &ids) != nil || strings.Contains(string(b), "srv-") || strings.Contains(string(b), "10.0.0") {
		t.Errorf("recentHosts is not a list of ids: %s", b)
	}
	// A second window reads them; only strings count, and at most recentMax.
	os.WriteFile(filepath.Join(dir, "keel-native.json"), []byte(`{"recentHosts":["a",3,"","b",null,{"id":"x"}]}`), 0o600)
	if got := loadRecentHosts(dir); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("read %v", got)
	}
	for i := 0; i < 20; i++ {
		r.w.rememberRecent(fmt.Sprintf("id%d", i))
	}
	if len(r.w.recentIDs) != recentMax || len(loadRecentHosts(dir)) != recentMax {
		t.Errorf("recent list has %d", len(r.w.recentIDs))
	}
}

func TestOpeningAHostRemembersIt(t *testing.T) {
	r := startRig(t)
	r.click(t, "start card Local Shell")
	r.press(ui.Cmd, ui.KeyT)
	if got := cardNames(r); got[0] != "Local Shell" || !slices.Contains(r.w.recentIDs, "h-local") {
		t.Fatalf("cards %q recents %v", got, r.w.recentIDs)
	}
}
