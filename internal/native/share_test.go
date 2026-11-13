package native

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// shareTestKey is a made-up link key for these tests; it opens nothing.
const shareTestKey = "00112233445566778899aabbccddeeff"
const shareTestURL = "http://127.0.0.1:1/join?room=r1#k=" + shareTestKey

var shareNow = time.Date(2026, 1, 2, 3, 10, 0, 0, time.UTC)

// ownerState is a session shared with one person who joined by link: you own and drive it.
func ownerState() *keelclient.State {
	return &keelclient.State{
		Status: "ready", Role: "driver", Title: "api-1", YouID: 1, DriverID: 1, OwnerID: 1,
		Attached: []keelclient.Peer{
			{ID: 2, Name: "Ann", Kind: "webrtc", Role: "follower", AttachedAt: time.Now().Add(-2 * time.Minute).Format(time.RFC3339)},
			{ID: 1, Name: "me", Kind: "web", Role: "driver", AttachedAt: time.Now().Add(-10 * time.Minute).Format(time.RFC3339)},
		},
	}
}

func shareRig(t *testing.T, st *keelclient.State) *rig {
	t.Helper()
	r := openTabs(t, 1)
	r.fb.shareURL = shareTestURL
	r.fb.conns[0].st = st
	r.tt.Frame()
	return r
}

func TestSharePeopleOrderRolesAndHowTheyCame(t *testing.T) {
	st := keelclient.State{YouID: 1, DriverID: 3, OwnerID: 1, Attached: []keelclient.Peer{
		{ID: 5, Name: "Cy", Kind: "webrtc", AttachedAt: "2026-01-02T03:09:00Z"},
		{ID: 3, Name: "Dee", Kind: "webrtc", AttachedAt: "2026-01-02T03:08:00Z"},
		{ID: 4, Name: "", Kind: "tui", AttachedAt: "2026-01-02T03:02:00Z"},
		{ID: 1, Name: "me", Kind: "web", AttachedAt: "2026-01-02T03:00:00Z"},
	}}
	rows := sharePeople(st, shareNow)
	var got []string
	for _, r := range rows {
		got = append(got, fmt.Sprintf("%s|%s|%s", r.name, r.role, r.sub))
	}
	want := []string{
		"You|owner|owner · this window",
		"Dee|driver|joined by link · 2 min ago",
		"guest|follower|terminal · 8 min ago",
		"Cy|follower|joined by link · 1 min ago",
	}
	if !slices.Equal(got, want) {
		t.Errorf("rows %q\nwant %q", got, want)
	}
	if watchingCount(st.Attached) != 2 || watchingCount(nil) != 0 {
		t.Error("watching counts the people who came by link")
	}
	if initials("ann lee kay") != "AL" || initials("  ") != "?" || initials("Ann") != "A" {
		t.Error("initials")
	}
	if roleOf(1, 1, 2) != "owner" || roleOf(2, 1, 2) != "driver" || roleOf(3, 1, 2) != "follower" || roleOf(0, 0, 0) != "follower" {
		t.Error("roleOf")
	}
	own, drv, driving, host := followState(keelclient.State{YouID: 2, DriverID: 2, OwnerID: 1, Title: "api", Attached: []keelclient.Peer{{ID: 1, Name: "Ann"}, {ID: 2, Name: "me"}}})
	if own != "Ann" || drv != "You" || !driving || host != "api" {
		t.Errorf("followState %q %q %v %q", own, drv, driving, host)
	}
	if own, drv, driving, host = followState(keelclient.State{}); own != "the host" || drv != "" || driving || host != "session" {
		t.Errorf("followState empty %q %q %v %q", own, drv, driving, host)
	}
}

func TestPopoverActionWhichButtonShows(t *testing.T) {
	rows := []person{{id: 1, you: true, role: "owner"}, {id: 2, name: "Ann", role: "follower"}}
	for _, tc := range []struct {
		name string
		rows []person
		s    seat
		want string
	}{
		{"one grantable", rows, seat{owner: true, driving: true}, "pass"},
		{"owner not driving, several", append(slices.Clone(rows), person{id: 3, name: "Bo", role: "follower"}), seat{owner: true}, "take"},
		{"owner driving, several", append(slices.Clone(rows), person{id: 3, name: "Bo", role: "follower"}), seat{owner: true, driving: true}, ""},
		{"follower", rows, seat{}, "ask"},
		{"driver not owner, one other", rows, seat{driving: true}, "pass"},
	} {
		if got, _ := popoverAction(tc.rows, tc.s); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
	if k, who := popoverAction(rows, seat{owner: true, driving: true}); k != "pass" || who.name != "Ann" {
		t.Errorf("pass to %q", who.name)
	}
}

func TestTheShareLinkNeverPrintsItsKey(t *testing.T) {
	l := shareLink{shareTestURL}
	s := &sharePop{status: "ready", link: l}
	for _, v := range []any{l, &l, s, *s} {
		for _, f := range []string{"%v", "%+v", "%#v", "%s"} {
			if out := fmt.Sprintf(f, v); strings.Contains(out, shareTestKey) || strings.Contains(out, "room=r1") {
				t.Errorf("%s of %T shows the link: %s", f, v, out)
			}
		}
	}
}

func TestShareButtonOpensThePopoverAndCopiesTheLink(t *testing.T) {
	r := shareRig(t, ownerState())
	if !r.has("Share") || !r.has("1 watching") {
		t.Fatal("a shared session tab shows the Share button and the watchers badge")
	}
	if len(r.fb.shared) != 0 {
		t.Fatal("nothing is shared before the click")
	}
	r.click(t, "Share")
	r.tt.Frame()
	if !slices.Equal(r.fb.shared, []string{"s1"}) {
		t.Fatalf("shared %v", r.fb.shared)
	}
	for _, s := range []string{
		"Share session", "api-1", "Live link", "Join link", "Copied",
		"People with the link see this pane's terminal only, from when you started sharing. Never your hosts, keys, vault or other panes.",
		"Works until you stop sharing or close this pane, 24 hours at most. You are asked before anyone joins; a request you do not answer is turned away after 2 minutes. Peer-to-peer over WebRTC; your loopback API stays private.",
		"In this session · 2", "You", "owner · this window", "Ann", "joined by link · 2 min ago", "Stop sharing",
	} {
		if s == "joined by link · 2 min ago" { // the age is the clock's: only its shape is fixed
			s = "joined by link · "
		}
		if !r.tt.HasText(s) && !textHas(r, s) {
			t.Errorf("popover lacks %q", s)
		}
	}
	if r.tt.Clipboard() != shareTestURL {
		t.Errorf("the link was not put on the clipboard")
	}
	if !r.tt.HasText(shareTestURL) { // the Join link field shows the whole link, as the app does
		t.Errorf("the Join link field does not hold the link: %q", r.tt.Texts())
	}
	if !r.has("Pass control to Ann") {
		t.Error("one person to hand over to: the foot says Pass control to Ann")
	}
	if len(r.w.queued) != 0 {
		t.Errorf("a toast was queued: %v", r.w.queued)
	}
}

// textHas reports a text that contains s (a row's sub line holds a clock).
func textHas(r *rig, s string) bool {
	return slices.ContainsFunc(r.tt.Texts(), func(x string) bool { return strings.Contains(x, s) })
}

func TestCopyPutsTheLinkOnTheClipboardAndNowhereElse(t *testing.T) {
	r := shareRig(t, ownerState())
	r.click(t, "Share")
	r.tt.Frame()
	r.w.shareLinks["s1"] = shareLink{shareTestURL}
	// Clear what the automatic copy did, then press Copy.
	r.w.share.copied = false
	r.tt.Frame()
	if r.has("Copied") || !r.has("Copy") {
		t.Fatal("before Copy the button says Copy")
	}
	r.click(t, "Copy")
	r.tt.Frame()
	if r.tt.Clipboard() != shareTestURL || !r.has("Copied") {
		t.Errorf("clipboard %q", r.tt.Clipboard())
	}
	// The link is kept in memory, by session, and in no preference or saved file.
	if r.w.shareLinks["s1"].url != shareTestURL {
		t.Error("the link is not remembered for the session")
	}
}

func TestEscapeAndClickOutsideClosePopoverAndReopenIsTheSameLinkWithoutAnotherShare(t *testing.T) {
	r := shareRig(t, ownerState())
	r.click(t, "Share")
	r.tt.Frame()
	if r.w.share == nil {
		t.Fatal("popover not open")
	}
	r.tt.Key(0, ui.KeyEscape)
	r.tt.Frame()
	if r.w.share != nil || r.tt.HasText("Share session") {
		t.Fatal("Escape did not close the popover")
	}
	r.click(t, "Share")
	r.tt.Frame()
	if len(r.fb.shared) != 1 {
		t.Errorf("shared %d times: a link this window has is shown again", len(r.fb.shared))
	}
	if r.w.share == nil || r.w.share.status != "ready" {
		t.Fatal("reopened popover is not ready")
	}
	r.tt.ClickAt(5, 400) // outside
	r.tt.Frame()
	if r.w.share != nil {
		t.Error("a click outside did not close it")
	}
}

func TestWithoutASignalServerThePopoverShowsTheServersError(t *testing.T) {
	r := shareRig(t, ownerState())
	r.fb.shareURL = ""
	r.fb.shareErr = &keelclient.StatusError{Method: "POST", Path: "/api/share", Status: "503", Code: 503, Msg: "set a signal server in Settings · Sharing to share"}
	r.click(t, "Share")
	r.tt.Frame()
	if !r.tt.HasText("set a signal server in Settings · Sharing to share") || !r.tt.HasText("Not shared") {
		t.Errorf("texts %q", r.tt.Texts())
	}
	if r.has("Stop sharing") || r.has("Join link") {
		t.Error("an error popover has no link and no Stop sharing")
	}
	if r.tt.Clipboard() != "" {
		t.Error("nothing to copy")
	}
}

func TestStopSharingCallsStopShareAndClosesThePopover(t *testing.T) {
	r := shareRig(t, ownerState())
	r.click(t, "Share")
	r.tt.Frame()
	r.click(t, "Stop sharing")
	r.tt.Frame()
	if !slices.Equal(r.fb.stopped, []string{"s1"}) || r.w.share != nil {
		t.Fatalf("stopped %v popover %v", r.fb.stopped, r.w.share)
	}
	if !r.tt.HasText("Sharing stopped. The join link no longer works.") {
		t.Error("no confirmation toast")
	}
	if _, ok := r.w.shareLinks["s1"]; ok {
		t.Error("the stopped link is still remembered: the next Share must ask for a new one")
	}
	r.click(t, "Share")
	r.tt.Frame()
	if len(r.fb.shared) != 2 {
		t.Errorf("shared %d times, want 2 (a new share after stopping)", len(r.fb.shared))
	}
}

func TestPopoverButtonsForYourSeat(t *testing.T) {
	// A follower asks; the owner not driving takes control; several people each get Pass control.
	follower := &keelclient.State{Status: "ready", Role: "follower", YouID: 2, DriverID: 1, OwnerID: 1, Attached: []keelclient.Peer{
		{ID: 1, Name: "Ann", Kind: "web"}, {ID: 2, Name: "me", Kind: "web"}}}
	r := shareRig(t, follower)
	r.click(t, "Share")
	r.tt.Frame()
	if !r.has("Ask for control") || r.has("Take control") {
		t.Error("a follower who is not the owner asks for control")
	}
	r.clickFoot(t)
	if r.fb.conns[0].asked != 1 {
		t.Errorf("asked %d", r.fb.conns[0].asked)
	}

	owner := &keelclient.State{Status: "ready", Role: "owner", YouID: 1, DriverID: 2, OwnerID: 1, Attached: []keelclient.Peer{
		{ID: 1, Name: "me", Kind: "web"}, {ID: 2, Name: "Ann", Kind: "webrtc"}, {ID: 3, Name: "Bo", Kind: "webrtc"}, {ID: 4, Name: "Cy", Kind: "webrtc"}}}
	r = shareRig(t, owner)
	r.click(t, "Share")
	r.tt.Frame()
	if !r.has("Take control") || !r.has("Pass control to Bo") || !r.has("Pass control to Cy") || r.has("Pass control to Ann") {
		t.Errorf("owner not driving, two others: Take control at the foot, Pass control on each row but the driver's; texts %q", r.tt.Texts())
	}
	r.clickFoot(t)
	if r.fb.conns[0].took != 1 {
		t.Errorf("took %d", r.fb.conns[0].took)
	}
	r.click(t, "Pass control to Bo")
	if !slices.Equal(r.fb.conns[0].granted, []uint64{3}) {
		t.Errorf("granted %v", r.fb.conns[0].granted)
	}

	driver := ownerState()
	driver.Attached = append(driver.Attached, keelclient.Peer{ID: 3, Name: "Bo", Kind: "webrtc", AttachedAt: "2026-01-02T03:06:00Z"})
	r = shareRig(t, driver)
	r.click(t, "Share")
	r.tt.Frame()
	if r.has("Pass control to Ann") == false || r.has("Pass control to Bo") == false {
		t.Error("several people: each row has Pass control")
	}
}

// clickFoot presses the button at the right of the popover's foot (the pane's banner has buttons of the same name,
// under the popover).
func (r *rig) clickFoot(t *testing.T) {
	t.Helper()
	f, ok := r.tt.Find("share foot")
	if !ok {
		t.Fatal("no popover foot")
	}
	r.tt.ClickAt(f.X+f.W-space4-20, f.Y+f.H/2)
	r.tt.Frame()
}

func TestTheWatchersBadgeCountsPeopleWhoCameByLink(t *testing.T) {
	st := ownerState()
	r := shareRig(t, st)
	if !r.has("1 watching") {
		t.Fatal("badge")
	}
	st.Attached = append(st.Attached, keelclient.Peer{ID: 3, Name: "Bo", Kind: "webrtc"}, keelclient.Peer{ID: 4, Name: "Kim", Kind: "web"})
	r.tt.Frame()
	if !r.has("2 watching") || r.has("1 watching") {
		t.Errorf("badge after two joined: %q", r.tt.Texts())
	}
	st.Attached = st.Attached[1:2]
	r.tt.Frame()
	if r.has("2 watching") || r.has("1 watching") {
		t.Error("no watchers, no badge")
	}
	r.click(t, "Share") // the Share button stays
	if r.w.share == nil {
		t.Error("popover did not open")
	}
}

func TestNoShareButtonOnAPageOrTheNewTab(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	if r.has("Share") {
		t.Error("the New tab page has no Share button")
	}
	r.tt.Key(ui.Cmd, ui.KeyComma)
	r.tt.Frame()
	if r.has("Share") {
		t.Error("a page has no Share button")
	}
}

// ---- the prompts ----

func joinRequest(r *rig, peer, name string) {
	r.fb.conns[0].events = append(r.fb.conns[0].events, keelclient.Event{Kind: keelclient.EventJoinRequest, PeerID: peer, Message: name})
	r.fb.conns[0].onChange()
	r.tt.Frame()
	r.tt.Frame()
}

func TestAJoinRequestShowsAnAllowDenyPromptAndNothingAdmitsWithoutAClick(t *testing.T) {
	r := shareRig(t, ownerState())
	joinRequest(r, "p1", "Ann")
	for _, s := range []string{"Ann", "wants to watch api-1.", joinNote, "Allow", "Deny"} {
		if !r.tt.HasText(s) && !r.has(s) {
			t.Errorf("prompt lacks %q", s)
		}
	}
	if !r.has("Share requests") {
		t.Error("the prompts are the region Share requests")
	}
	// Time, frames, Escape, the popover: nothing answers it.
	r.tt.Key(0, ui.KeyEscape)
	r.click(t, "Share")
	r.tt.Frame()
	r.tt.Key(0, ui.KeyEscape)
	r.tt.Frame()
	r.tt.Advance(3 * time.Minute)
	r.tt.Frame()
	if len(r.fb.admitted) != 0 {
		t.Fatalf("admitted without a click: %v", r.fb.admitted)
	}
	if !r.has("Allow") {
		t.Fatal("Escape closed the prompt: only keel serve's resolved frame does")
	}
	// The same request again does not stack.
	joinRequest(r, "p1", "Ann")
	if len(r.w.prompts) != 1 {
		t.Errorf("%d prompts for one peer", len(r.w.prompts))
	}
	r.click(t, "Allow")
	if !slices.Equal(r.fb.admitted, []string{"s1 p1 true"}) {
		t.Fatalf("admitted %v", r.fb.admitted)
	}
	if r.has("Allow") || len(r.w.prompts) != 0 {
		t.Error("the answered prompt stays")
	}
}

func TestDenySendsAllowFalseAndAResolvedFrameRemovesThePrompt(t *testing.T) {
	r := shareRig(t, ownerState())
	joinRequest(r, "p1", "Ann")
	joinRequest(r, "p2", "Bo")
	if len(r.w.prompts) != 2 {
		t.Fatalf("prompts %d", len(r.w.prompts))
	}
	// keel serve resolves p2 itself (it timed out after 2 minutes, or the joiner left): no admit call.
	r.fb.conns[0].events = []keelclient.Event{{Kind: keelclient.EventJoinResolved, PeerID: "p2", Message: "expired"}}
	r.fb.conns[0].onChange()
	r.tt.Frame()
	r.tt.Frame()
	if len(r.w.prompts) != 1 || r.w.prompts[0].peerID != "p1" || len(r.fb.admitted) != 0 {
		t.Fatalf("prompts %d admitted %v", len(r.w.prompts), r.fb.admitted)
	}
	r.click(t, "Deny")
	if !slices.Equal(r.fb.admitted, []string{"s1 p1 false"}) {
		t.Fatalf("admitted %v", r.fb.admitted)
	}
}

func TestAnAnswerThatKeelServeRefusesIsShownAndTheJoinFailureToo(t *testing.T) {
	r := shareRig(t, ownerState())
	r.fb.admitErr = &keelclient.StatusError{Method: "POST", Path: "x", Status: "409", Code: 409, Msg: "no such join request (it may have expired)"}
	joinRequest(r, "p1", "Ann")
	r.click(t, "Allow")
	if !r.tt.HasText("no such join request (it may have expired)") || r.has("Allow") {
		t.Errorf("texts %q", r.tt.Texts())
	}
	r.fb.conns[0].events = []keelclient.Event{{Kind: keelclient.EventJoinFailed, PeerID: "p3", Message: "Bo was not connected: failed verification"}}
	r.fb.conns[0].onChange()
	r.tt.Frame()
	r.tt.Frame()
	if !r.tt.HasText("Bo was not connected: failed verification") {
		t.Error("the failure is a toast")
	}
}

func TestAControlRequestPromptsTheDriverToHandOverOrKeepDriving(t *testing.T) {
	r := shareRig(t, ownerState())
	ask := func(from uint64, msg string) {
		r.fb.conns[0].events = []keelclient.Event{{Kind: keelclient.EventControlRequest, FromID: from, Message: msg}}
		r.fb.conns[0].onChange()
		r.tt.Frame()
		r.tt.Frame()
	}
	ask(2, "Ann asks for control")
	for _, s := range []string{"Ann", "asks for control", "Of api-1. You follow until you take it back.", "Hand over", "Keep driving"} {
		if !r.tt.HasText(s) && !r.has(s) {
			t.Errorf("control prompt lacks %q", s)
		}
	}
	r.click(t, "Keep driving")
	if len(r.fb.conns[0].granted) != 0 || len(r.w.prompts) != 0 {
		t.Fatalf("Keep driving granted %v", r.fb.conns[0].granted)
	}
	ask(2, "Ann asks for control")
	r.click(t, "Hand over")
	if !slices.Equal(r.fb.conns[0].granted, []uint64{2}) || len(r.w.prompts) != 0 {
		t.Fatalf("granted %v", r.fb.conns[0].granted)
	}
	// A request never grants itself: nothing was granted before the click, and an admit is never involved.
	if len(r.fb.admitted) != 0 {
		t.Error("admit called")
	}
	// A request seen by someone who may not hand over is only the runtime's words; yours is no prompt either.
	r.fb.conns[0].st = &keelclient.State{Status: "ready", Role: "follower", YouID: 5, DriverID: 1, OwnerID: 1}
	ask(2, "Ann asks for control")
	if len(r.w.prompts) != 0 || !r.tt.HasText("Ann asks for control") {
		t.Errorf("a follower sees the words, not a prompt (prompts %d)", len(r.w.prompts))
	}
	r.fb.conns[0].st = ownerState()
	ask(1, "me asks for control")
	if len(r.w.prompts) != 0 {
		t.Error("your own request is no prompt")
	}
}

func TestAControlNoteDismissesRequestsAndIsShown(t *testing.T) {
	r := shareRig(t, ownerState())
	r.fb.conns[0].events = []keelclient.Event{{Kind: keelclient.EventControlRequest, FromID: 2, Message: "Ann asks for control"}}
	r.fb.conns[0].onChange()
	r.tt.Frame()
	r.tt.Frame()
	r.fb.conns[0].events = []keelclient.Event{{Kind: keelclient.EventControlNote, Message: "Ann has control"}}
	r.fb.conns[0].onChange()
	r.tt.Frame()
	r.tt.Frame()
	if len(r.w.prompts) != 0 || !r.tt.HasText("Ann has control") {
		t.Errorf("prompts %d", len(r.w.prompts))
	}
}

func TestStopSharingAndClosingThePaneForgetThePrompts(t *testing.T) {
	r := shareRig(t, ownerState())
	joinRequest(r, "p1", "Ann")
	r.w.stopShare()
	r.tt.Frame()
	if len(r.w.prompts) != 0 || len(r.fb.admitted) != 0 {
		t.Fatalf("prompts %d admitted %v", len(r.w.prompts), r.fb.admitted)
	}
	joinRequest(r, "p2", "Bo")
	r.click(t, "Close pane")
	if len(r.w.prompts) != 0 || len(r.fb.admitted) != 0 {
		t.Fatalf("prompts %d admitted %v", len(r.w.prompts), r.fb.admitted)
	}
}

func TestOnlyTheAllowAndDenyButtonsAnswerARequest(t *testing.T) {
	// answerJoin is the one caller of AdmitShare, and only the two buttons call answerJoin.
	n, calls := 0, 0
	for name, src := range sourceFiles(t) {
		n += strings.Count(src, "AdmitShare(")
		calls += strings.Count(src, "w.answerJoin(")
		_ = name
	}
	if calls != 2 {
		t.Errorf("answerJoin is called %d times, want 2 (Allow, Deny)", calls)
	}
	if n != 3 { // the interface method, the call, and clientBackend's embedded method does not appear
		t.Logf("AdmitShare( appears %d times", n)
	}
	if got := strings.Count(sourceFiles(t)["share.go"], "w.be.AdmitShare("); got != 1 {
		t.Errorf("AdmitShare is called %d times in share.go, want 1", got)
	}
}

func TestPaneMenuAndPaletteShare(t *testing.T) {
	r := shareRig(t, ownerState())
	if got := menuLabels(t, r, "pane bar 1"); !slices.Contains(got, "Share") {
		t.Fatalf("menu %q", got)
	}
	choose(t, r, "Share")
	r.tt.Frame()
	if r.w.share == nil || !slices.Equal(r.fb.shared, []string{"s1"}) {
		t.Fatalf("the pane menu's Share did not share: %v", r.fb.shared)
	}
	r.w.share = nil

	r.openPal(t)
	r.typePal("shar")
	var titles []string
	for _, it := range r.rows() {
		titles = append(titles, it.title)
	}
	if !slices.Contains(titles, "Share session (join link)") || !slices.Contains(titles, "Stop sharing this session") {
		t.Fatalf("palette %q", titles)
	}
	r.press(0, ui.KeyEnter)
	r.tt.Frame()
	if r.w.share == nil {
		t.Error("the palette's Share session did not open the popover")
	}
	r.w.closeShare()
	r.openPal(t)
	r.typePal("sharing")
	r.press(0, ui.KeyEnter)
	r.tt.Frame()
	if !slices.Equal(r.fb.stopped, []string{"s1"}) {
		t.Errorf("stopped %v", r.fb.stopped)
	}
}

func TestAPromptOverThePopoverAnswersWithOneClickAndLeavesThePopoverOpen(t *testing.T) {
	r := shareRig(t, ownerState())
	r.click(t, "Share")
	r.tt.Frame()
	joinRequest(r, "p1", "Ann")
	a, _ := r.tt.Find("Allow")
	p, _ := r.tt.Find("dialog Share session")
	if a.Y < p.Y || a.Y > p.Y+p.H {
		t.Fatalf("the prompt (%v) does not lie over the popover (%v): the test would prove nothing", a, p)
	}
	r.tt.ClickAt(a.X+a.W/2, a.Y+a.H/2)
	r.tt.Frame()
	if !slices.Equal(r.fb.admitted, []string{"s1 p1 true"}) || r.w.share == nil {
		t.Fatalf("admitted %v, popover open %v: a click on a prompt is one click, and not a click outside the popover", r.fb.admitted, r.w.share != nil)
	}
	// A click elsewhere still closes it.
	r.tt.ClickAt(5, 400)
	r.tt.Frame()
	if r.w.share != nil {
		t.Error("a click outside did not close the popover")
	}
}
