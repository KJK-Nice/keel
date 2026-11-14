package native

import (
	"fmt"
	"testing"

	"keel/internal/native/keelclient"
)

// The typing gate, pinned for every input: whether a pane may be typed into (a local session that is ready and that
// this client drives) and whether a press lacks the ⇧ it needs, at each place that asks.

type gateCase struct {
	remote       bool
	status, role string
}

func (g gateCase) String() string {
	return fmt.Sprintf("remote=%v status=%q role=%q", g.remote, g.status, g.role)
}

func (g gateCase) state() keelclient.State {
	st := keelclient.State{YouID: 2, DriverID: 2}
	setWord(&st.Status, g.status)
	setWord(&st.Role, g.role)
	return st
}

func setWord[T ~string](dst *T, v string) { *dst = T(v) }

func gateCases() (out []gateCase) {
	for _, remote := range []bool{false, true} {
		for _, status := range []string{"", "connecting", "ready", "error", "exited"} {
			for _, role := range []string{"", "owner", "driver", "follower"} {
				out = append(out, gateCase{remote, status, role})
			}
		}
	}
	return out
}

func TestTheApproveDialogSaysWhyItCannotTypeForEveryPaneState(t *testing.T) {
	for _, g := range gateCases() {
		st := g.state()
		d := &approveDialog{p: &pane{conn: &fakeConn{st: &st}, remote: g.remote}, src: approveSnippet{fixed("ls")}}
		want := ""
		switch {
		case g.status != "ready":
			want = msgNotConnected
		case g.role != "driver":
			want = msgFollowing
		}
		if got := d.blocked(); got != want {
			t.Errorf("%v: blocked %q, want %q", g, got, want)
		}
	}
	st := drivingState
	gone := &approveDialog{p: &pane{conn: &fakeConn{st: &st}}, src: approveSnippet{func() (string, bool) { return "ls", false }}}
	if got := gone.blocked(); got != msgGone {
		t.Errorf("a source that is gone: %q", got)
	}
}

func TestAGroupRowSaysWhyItCannotBeTypedForEveryPaneState(t *testing.T) {
	for _, g := range gateCases() {
		st := g.state()
		ap := &agentPane{p: &pane{conn: &fakeConn{st: &st}}, remote: g.remote, driving: g.role == "driver"}
		grp := &approveGroup{pane: func(string) *agentPane { return ap }}
		want := ""
		switch {
		case g.remote:
			want = msgGroupPaneGone
		case g.role != "driver":
			want = msgGroupPaneFollow
		case g.status != "ready":
			want = msgNotConnected
		}
		r := &groupRow{bytes: "ls\n", checked: true}
		if got := grp.rowBlocked(r); got != want {
			t.Errorf("%v: rowBlocked %q, want %q", g, got, want)
		}
		grp.rows = []*groupRow{r}
		if got := len(grp.effective()) == 1; got != (want == "") {
			t.Errorf("%v: effective %v", g, got)
		}
	}
	grp := &approveGroup{pane: func(string) *agentPane { return nil }}
	for _, c := range []struct {
		r    *groupRow
		want string
	}{
		{&groupRow{bytes: "ls\n"}, msgGroupPaneGone},
		{&groupRow{}, msgGroupNoBytes},
		{&groupRow{err: "no"}, "no"},
		{&groupRow{attempted: true}, ""},
	} {
		if got := grp.rowBlocked(c.r); got != c.want {
			t.Errorf("row %+v: %q, want %q", *c.r, got, c.want)
		}
	}
	if got := grp.rowBlocked(&groupRow{bytes: "x"}); got != msgGroupPaneGone {
		t.Errorf("a closed pane: %q", got)
	}
	if got := (&approveGroup{pane: func(string) *agentPane { return &agentPane{driving: true} }}).rowBlocked(&groupRow{bytes: "x"}); got != msgGroupPaneGone {
		t.Errorf("an agent pane with no pane: %q", got)
	}
}

func TestACardOffersTypeOnlyForADrivenLocalPaneAndAsksForShiftWhenNeeded(t *testing.T) {
	for _, remote := range []bool{false, true} {
		for _, driving := range []bool{false, true} {
			for _, needs := range []bool{false, true} {
				for _, shift := range []bool{false, true} {
					p := keelclient.Proposal{SessionID: "s", NeedsShift: needs}
					pane := &agentPane{remote: remote, driving: driving}
					offer, ok, reason := offerType, true, ""
					switch {
					case remote || !driving:
						offer, ok, reason = offerFollowing, false, offerFollowing
					case needs && !shift:
						offer, ok, reason = offerShift, false, gateShift
					case needs:
						offer = offerShift
					}
					if got := typeOffer(p, pane); got != offer {
						t.Errorf("remote=%v driving=%v needs=%v: offer %q, want %q", remote, driving, needs, got, offer)
					}
					if gotOK, gotReason := typeGate(p, pane, shift); gotOK != ok || gotReason != reason {
						t.Errorf("remote=%v driving=%v needs=%v shift=%v: gate %v %q, want %v %q", remote, driving, needs, shift, gotOK, gotReason, ok, reason)
					}
				}
			}
		}
	}
	for _, c := range []struct {
		p    keelclient.Proposal
		pane *agentPane
		want string
	}{
		{keelclient.Proposal{SessionID: "s", Expired: true}, &agentPane{driving: true}, offerExpired},
		{keelclient.Proposal{SessionID: "s", Placeholders: true}, &agentPane{driving: true}, offerPlaceholder},
		{keelclient.Proposal{}, &agentPane{driving: true}, offerNone},
		{keelclient.Proposal{SessionID: "s"}, nil, offerClosed},
	} {
		if got := typeOffer(c.p, c.pane); got != c.want {
			t.Errorf("%+v: offer %q, want %q", c.p, got, c.want)
		}
		if ok, reason := typeGate(c.p, c.pane, true); ok || reason != c.want {
			t.Errorf("%+v: gate %v %q", c.p, ok, reason)
		}
	}
}

func TestAnInsertAndAGroupTypeNeedShiftExactlyWhenARiskyOneIsPicked(t *testing.T) {
	for _, needs := range []bool{false, true} {
		for _, shift := range []bool{false, true} {
			ok, reason := insertGate(&keelclient.SuggestOption{NeedsShift: needs}, shift)
			if want := !needs || shift; ok != want || (reason == needsShiftReason) != !want {
				t.Errorf("insert needs=%v shift=%v: %v %q", needs, shift, ok, reason)
			}
			st := drivingState
			ap := &agentPane{p: &pane{conn: &fakeConn{st: &st}}, driving: true}
			grp := &approveGroup{runID: "run", pane: func(string) *agentPane { return ap }, rows: []*groupRow{
				{proposalID: "a", bytes: "ls\n", checked: true},
				{proposalID: "b", bytes: "rm x\n", checked: true, needsShift: needs},
				{proposalID: "c", bytes: "rm y\n", needsShift: true}, // not ticked: it asks for nothing
			}}
			jobs := (&workspace{}).groupJobs(&approveDialog{src: grp}, shift)
			if want := !needs || shift; (len(jobs) == 2) != want || (len(jobs) == 0) == want || grp.shiftHint == want {
				t.Errorf("group needs=%v shift=%v: %d jobs, hint %v", needs, shift, len(jobs), grp.shiftHint)
			}
			for _, j := range jobs {
				if j.ref == nil || j.ref.Shift != shift || j.ref.GroupRunID != "run" {
					t.Errorf("group needs=%v shift=%v: ref %+v", needs, shift, j.ref)
				}
			}
		}
	}
	if ok, reason := insertGate(nil, true); ok || reason != "none" {
		t.Errorf("no option: %v %q", ok, reason)
	}
}

func TestSyncInputReachesOnlyAReadyPaneThisClientDrives(t *testing.T) {
	for _, g := range gateCases() {
		st := g.state()
		c := &fakeConn{st: &st}
		fanOut([]session{c}, []byte("x"))
		if want := g.status == "ready" && g.role == "driver"; (c.wrote == 1) != want {
			t.Errorf("%v: wrote %d", g, c.wrote)
		}
	}
}

func TestSuggestionsTypeNowAndThePanelReadTheSameGate(t *testing.T) {
	r := newSugRig(t)
	r.screen(false, "user@h $")
	r.c.directRes = keelclient.DirectResult{Typed: true}
	for _, g := range gateCases() {
		st := g.state()
		r.c.st = &st
		r.p.remote = g.remote
		can := !g.remote && g.status == "ready" && g.role == "driver"
		if got := r.w.canSuggest(r.p); got != can {
			t.Errorf("%v: canSuggest %v", g, got)
		}
		dialog, before := false, len(r.c.directs)
		r.w.typeNow(r.p, typeSource{line: "df -h", snippetID: "sn-1", dialog: func() { dialog = true }})
		r.tt.Frame()
		r.p.typing = false
		if dialog == can || (len(r.c.directs) == before+1) != can {
			t.Errorf("%v: dialog %v, direct types %d", g, dialog, len(r.c.directs)-before)
		}
		panes := r.w.agentPanes()
		if !g.remote && (len(panes) != 1 || panes[0].driving != (g.role == "driver") || panes[0].remote) {
			t.Errorf("%v: agent panes %+v", g, panes)
		}
		if got := r.w.groupPaneOpen(r.p.sessionID()); got != (!g.remote && g.role == "driver") {
			t.Errorf("%v: groupPaneOpen %v", g, got)
		}
		if got := len(r.w.groupPaneIDs()) == 1; got != (!g.remote && g.role == "driver") {
			t.Errorf("%v: groupPaneIDs %v", g, r.w.groupPaneIDs())
		}
		if got := seatOf(st).driving; got != (g.role == "driver") {
			t.Errorf("%v: seat driving %v", g, got)
		}
	}
}
