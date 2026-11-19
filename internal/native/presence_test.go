package native

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"slime/internal/native/keelclient"
)

func TestPresenceOfAPane(t *testing.T) {
	me := keelclient.Peer{ID: 1, Name: "me", Kind: keelclient.KindWindow}
	ann := keelclient.Peer{ID: 2, Name: "Ann", Kind: keelclient.KindJoined}
	bo := keelclient.Peer{ID: 3, Name: "Bo", Kind: keelclient.KindJoined}
	cy := keelclient.Peer{ID: 4, Name: "", Kind: keelclient.KindJoined}
	kim := keelclient.Peer{ID: 5, Name: "Kim", Kind: keelclient.KindWindow}
	for _, tc := range []struct {
		name string
		st   keelclient.State
		kind presenceKind
		text string
	}{
		{"alone", keelclient.State{Status: "ready", Role: "driver", Attached: []keelclient.Peer{me}}, presenceNone, ""},
		{"not ready", keelclient.State{Status: "connecting", Role: "follower", Attached: []keelclient.Peer{me, ann}}, presenceNone, ""},
		{"a window follows, nobody by link", keelclient.State{Status: "ready", Role: "driver", Attached: []keelclient.Peer{me, kim}}, presenceNone, ""},
		{"one watcher", keelclient.State{Status: "ready", Role: "driver", Attached: []keelclient.Peer{me, ann}}, presenceWatched, "Ann watching"},
		{"two watchers", keelclient.State{Status: "ready", Role: "driver", Attached: []keelclient.Peer{me, ann, bo}}, presenceWatched, "Ann, Bo watching"},
		{"three watchers", keelclient.State{Status: "ready", Role: "driver", Attached: []keelclient.Peer{me, ann, bo, cy}}, presenceWatched, "Ann +2 watching"},
		{"following", keelclient.State{Status: "ready", Role: "follower", DriverID: 2, Attached: []keelclient.Peer{me, ann}}, presenceFollowing, "Ann is driving"},
		{"nobody drives", keelclient.State{Status: "ready", Role: "follower", Attached: []keelclient.Peer{me, ann}}, presenceFollowing, "Nobody is driving"},
	} {
		pr := presenceOf(tc.st)
		if pr.kind != tc.kind || pr.text() != tc.text {
			t.Errorf("%s: kind %d %q, want %d %q", tc.name, pr.kind, pr.text(), tc.kind, tc.text)
		}
	}
	owner := presenceOf(keelclient.State{Status: "ready", Role: "owner", OwnerID: 1, YouID: 1, DriverID: 2, Attached: []keelclient.Peer{me, ann}})
	if !owner.owner {
		t.Error("the owner who follows may take control")
	}
}

// A driver's colour is one of the normal ANSI colours, the same for the same name; watchers and nobody driving are haze;
// only someone else at the keys colours the cursor.
func TestPresenceColours(t *testing.T) {
	palette := map[ui.Color]bool{}
	for i := presenceFirst; i < presenceFirst+presenceCount; i++ {
		palette[ansi[i]] = true
	}
	for _, n := range []string{"Ann", "Bo", "kjkusap", ""} {
		if c := personColor(n); !palette[c] || c != personColor(n) {
			t.Errorf("%q: colour %v is not a stable ANSI colour of ansi-1 … ansi-6", n, c)
		}
	}
	following := presence{kind: presenceFollowing, who: []string{"Ann"}}
	if following.color() != personColor("Ann") || following.cursorColor() != personColor("Ann") {
		t.Error("someone else driving: ring and cursor take their colour")
	}
	for _, pr := range []presence{{kind: presenceWatched, who: []string{"Ann"}}, {kind: presenceFollowing, who: []string{""}}} {
		if pr.color() != haze || pr.cursorColor() != (ui.Color{}) {
			t.Errorf("%+v: haze ring, the theme's cursor", pr)
		}
	}
}

// The terminal's theme is rebuilt only when the cursor's colour changes.
func TestKeepCursorRebuildsTheThemeOnlyOnAChange(t *testing.T) {
	n := 0
	p := &pane{setTheme: func() { n++ }}
	ann := presence{kind: presenceFollowing, who: []string{"Ann"}}
	p.keepCursor(ann)
	p.keepCursor(ann)
	if n != 1 || p.cursorCol != personColor("Ann") {
		t.Fatalf("themes %d, cursor %v", n, p.cursorCol)
	}
	p.keepCursor(presence{})
	if n != 2 || p.cursorCol != (ui.Color{}) {
		t.Errorf("back at the keys: themes %d, cursor %v", n, p.cursorCol)
	}
}

// In a pane you follow the notch says who drives and carries Ask for control; the ring is the driver's colour.
func TestFollowerNotchAndRing(t *testing.T) {
	two := []keelclient.Peer{{ID: 1, Name: "ann", Kind: keelclient.KindWindow, Role: "driver"}, {ID: 2, Name: "me", Kind: keelclient.KindWindow, Role: "follower"}}
	r := failedRig(t, keelclient.State{Status: "ready", Role: "follower", Attached: two, OwnerID: 1, YouID: 2, DriverID: 1}, "h-api")
	notch, ok := r.tt.Find("presence following")
	body, ok2 := r.tt.Find("pane 1")
	if !ok || !ok2 || !r.tt.HasText("ann is driving") || !r.has("Ask for control") {
		t.Fatalf("notch %v (%v), body %v: %v", notch, ok, body, r.tt.Texts())
	}
	if notch.X+notch.W > body.X+body.W || notch.Y < body.Y+sizePanebar {
		t.Errorf("notch %v is not on the terminal's top edge inside the pane %v", notch, body)
	}
	// The ring's left edge, half way down the terminal.
	if got := r.pixel(body.X+1, body.Y+body.H/2); !is(got, personColor("ann")) {
		t.Errorf("ring %v, want ann's colour %v", got, personColor("ann"))
	}
	if p := r.w.activeTab().panes["1"]; p.cursorCol != personColor("ann") {
		t.Errorf("cursor %v, want ann's colour", p.cursorCol)
	}
}
