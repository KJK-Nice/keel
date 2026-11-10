package native

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"keel/internal/native/keelclient"
)

// Share (docs/ui-redesign.md §5.6), as the app has it: features/share.md. The rules are the pure functions of
// lib/share.js; the Share popover is ui/sharepop.js, the Allow / Deny prompt ui/joinprompt.js, the control request
// ui/sharecontrol.js. Nothing here decides who may watch or type: admission is keel serve's (internal/share), and
// the only calls that answer a join request are the Allow and Deny buttons (answerJoin); the only call that hands
// control over is the Hand over button and the Pass control buttons (sendControl with GrantControl).

// shareLink is the join link of a share. Its #k= part is the share's secret: it authenticates the two WebRTC peers
// and never reaches the signal server. It lives in memory only (never in a pref, the saved workspace, a toast or a
// log), and printing it shows nothing.
type shareLink struct{ url string }

func (shareLink) String() string     { return "{share link}" }
func (l shareLink) GoString() string { return l.String() }

// redacted is the link as the popover's field shows it: the key's value is not drawn. Copy puts the whole link on the
// clipboard.
func (l shareLink) redacted() string {
	before, _, ok := strings.Cut(l.url, "#k=")
	if !ok {
		return l.url
	}
	return before + "#k=…"
}

// sharePop is the open Share popover: the pane it shares and what keel serve answered.
type sharePop struct {
	open     bool
	p        *pane
	status   string // pending | ready | error
	link     shareLink
	err      string
	copied   bool
	wantCopy bool // the link just arrived: the next frame puts it on the clipboard
	seq      int
}

func (s sharePop) String() string   { return fmt.Sprintf("{share popover %s open=%v}", s.status, s.open) }
func (s sharePop) GoString() string { return s.String() }

// sharePrompt is one prompt of the region "Share requests": someone asks to watch (Allow / Deny), or someone asks
// for control (Hand over / Keep driving).
type sharePrompt struct {
	p       *pane
	control bool
	peerID  string // a join request's peer
	fromID  uint64 // a control request's client
	name    string
	busy    bool // Allow or Deny was pressed; the answer is on its way
}

func (s *sharePrompt) key() string {
	if s.control {
		return fmt.Sprintf("control\n%d", s.fromID)
	}
	return "join\n" + s.peerID
}

// ---- the pure rules of lib/share.js ----

// roleOf is a client's role in a session as the share hub reports it: the driver drives, the owner may take control
// back, everyone else follows.
func roleOf(id, ownerID, driverID uint64) string {
	switch {
	case id != 0 && id == driverID:
		return "driver"
	case id != 0 && id == ownerID:
		return "owner"
	}
	return "follower"
}

// kindLabel is lib/share.js KIND_LABEL: how a client came.
func kindLabel(kind string) string {
	switch kind {
	case "webrtc":
		return "joined by link"
	case "web":
		return "Keel window"
	case "tui":
		return "terminal"
	}
	return ""
}

// person is a row of the people list.
type person struct {
	id       uint64
	name     string
	initials string
	you      bool
	role     string
	sub      string
	viaLink  bool
	at       time.Time
	canGrant bool
}

// initials is the first letter of the first two words, upper-cased; "?" for none.
func initials(name string) string {
	var out []rune
	for _, w := range strings.Fields(name) {
		if len(out) == 2 {
			break
		}
		r, _ := utf8.DecodeRuneInString(w)
		out = append(out, r)
	}
	if len(out) == 0 {
		return "?"
	}
	return strings.ToUpper(string(out))
}

// sharePeople lists who is attached, you first, then whoever drives, then by arrival (lib/share.js sharePeople).
func sharePeople(st keelclient.State, now time.Time) []person {
	rows := make([]person, 0, len(st.Attached))
	for _, c := range st.Attached {
		isYou := st.YouID != 0 && c.ID == st.YouID
		var at time.Time
		when := ""
		if t, err := time.Parse(time.RFC3339Nano, c.AttachedAt); err == nil {
			at, when = t, ageLabel(t, now)
		}
		var sub string
		switch {
		case isYou && c.ID == st.OwnerID:
			sub = "owner · this window"
		case isYou:
			sub = "this window"
		default:
			first := kindLabel(c.Kind)
			if c.ID == st.OwnerID {
				first = "owner"
			}
			sub = strings.Join(slices.DeleteFunc([]string{first, when}, func(s string) bool { return s == "" }), " · ")
		}
		name := c.Name
		if name == "" {
			name = "guest"
		}
		row := person{id: c.ID, name: name, initials: initials(name), you: isYou, role: roleOf(c.ID, st.OwnerID, st.DriverID), sub: sub, viaLink: c.Kind == "webrtc", at: at}
		if isYou {
			row.name = "You"
		}
		rows = append(rows, row)
	}
	rank := func(r person) int {
		switch {
		case r.you:
			return 0
		case r.role == "driver":
			return 1
		}
		return 2
	}
	slices.SortStableFunc(rows, func(a, b person) int {
		if d := rank(a) - rank(b); d != 0 {
			return d
		}
		return a.at.Compare(b.at)
	})
	return rows
}

// watchingCount is how many people watch through a share link.
func watchingCount(attached []keelclient.Peer) (n int) {
	for _, c := range attached {
		if c.Kind == "webrtc" {
			n++
		}
	}
	return n
}

// clientName is a client's name, or "Someone".
func clientName(attached []keelclient.Peer, id uint64) string {
	for _, c := range attached {
		if c.ID == id && c.Name != "" {
			return c.Name
		}
	}
	return "Someone"
}

// seat is what the pane's session says about this client's seat: it owns, drives or follows.
type seat struct{ owner, driving bool }

func seatOf(st keelclient.State) seat {
	return seat{owner: st.OwnerID != 0 && st.OwnerID == st.YouID, driving: st.Role == "driver"}
}

// followState is lib/share.js followState: who owns and who drives, by name, and whether that is you.
func followState(st keelclient.State) (owner, driver string, driving bool, host string) {
	owner = "the host"
	if st.OwnerID != 0 {
		owner = clientName(st.Attached, st.OwnerID)
	}
	driving = st.YouID != 0 && st.DriverID == st.YouID
	switch {
	case driving:
		driver = "You"
	case st.DriverID != 0:
		driver = clientName(st.Attached, st.DriverID)
	}
	host = st.Title
	if host == "" {
		host = "session"
	}
	return owner, driver, driving, host
}

// popoverAction is the one button at the foot of the popover (lib/share.js sharePopoverHTML): Pass control to the one
// person who can be handed it, else Take control for the owner, else Ask for control. who is the grantee for pass.
func popoverAction(rows []person, s seat) (kind string, who person) {
	var grantable []person
	for _, p := range rows {
		if (s.driving || s.owner) && !p.you && p.role != "driver" {
			grantable = append(grantable, p)
		}
	}
	switch {
	case len(grantable) == 1:
		return "pass", grantable[0]
	case s.owner && !s.driving:
		return "take", person{}
	case !s.owner && !s.driving:
		return "ask", person{}
	}
	return "", person{}
}

// ---- the window's share state ----

// shareTarget is the pane Share acts on: the focused pane of a session tab, when it is a live local session.
func (w *workspace) shareTarget() *pane {
	t := w.tab()
	if t == nil || t.isNew() || t.isPage() || t.remote {
		return nil
	}
	if p := t.panes[t.focused]; p != nil && !p.remote && p.sessionID() != "" {
		return p
	}
	return nil
}

// openShare opens the Share popover on the focused pane. It shares that pane's session (POST /api/share returns the
// live link when there is one) and puts the link on the clipboard.
func (w *workspace) openShare() {
	p := w.shareTarget()
	if p == nil {
		return
	}
	sid := p.sessionID()
	known := w.shareLinks[sid]
	s := &sharePop{open: true, p: p, status: "pending", link: known}
	if known.url != "" {
		s.status = "ready"
	}
	w.share = s
	if known.url == "" {
		w.startShare(s)
	}
}

func (w *workspace) startShare(s *sharePop) {
	s.seq++
	seq, p := s.seq, s.p
	sid := p.sessionID()
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		url, err := w.be.Share(c, sid)
		w.post(func() {
			current := w.share == s && s.seq == seq && s.open
			switch {
			case err != nil:
				if current {
					s.status, s.err = "error", errText("share", err)
				} else {
					w.fail("share", err)
				}
			case url == "":
				if current {
					s.status, s.err = "error", "The share started but returned no link."
				}
			default:
				if w.shareLinks == nil {
					w.shareLinks = map[string]shareLink{}
				}
				w.shareLinks[sid] = shareLink{url}
				if current {
					s.status, s.link, s.err, s.wantCopy = "ready", shareLink{url}, "", true
				}
			}
		})
	})
}

func (w *workspace) closeShare() {
	if s := w.share; s != nil {
		w.share = nil
		if t := w.tabOf(s.p); t != nil && t == w.tab() {
			w.wantFocus = s.p.id
		}
	}
}

// toggleShare is the titlebar button and the badge.
func (w *workspace) toggleShare() {
	if w.share != nil && w.share.open {
		w.closeShare()
		return
	}
	w.openShare()
}

// stopShare is Stop sharing (and the palette's "Stop sharing this session"): keel serve ends the share, every viewer
// is disconnected and the link stops working.
func (w *workspace) stopShare() {
	p := w.shareTarget()
	if w.share != nil {
		p = w.share.p
	}
	if p == nil || p.remote {
		return
	}
	sid := p.sessionID()
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		err := w.be.StopShare(c, sid)
		w.post(func() {
			if err != nil {
				w.fail("stop sharing", err)
				return
			}
			delete(w.shareLinks, sid)
			w.forgetPrompts(p)
			w.closeShare()
			w.say("Sharing stopped. The join link no longer works.")
		})
	})
}

// syncShare closes the popover when focus moved to another pane or to a joined one (sharepop.js follow).
func (w *workspace) syncShare() {
	if s := w.share; s != nil && (w.tabOf(s.p) == nil || w.shareTarget() != s.p) {
		w.share = nil
	}
}

// ---- announcements: the prompts ----

// drainEvents reads what the share hub and the runtime announced on every pane's socket: a join request becomes a
// prompt, a control request becomes a prompt for whoever may hand over, everything else is the app's toast.
func (w *workspace) drainEvents() {
	for _, t := range w.tabs {
		for _, p := range t.panes {
			for _, ev := range p.conn.Events() {
				w.onEvent(p, ev)
			}
		}
	}
}

func (w *workspace) onEvent(p *pane, ev keelclient.Event) {
	st := p.conn.Snapshot()
	switch ev.Kind {
	case keelclient.EventJoinRequest:
		if ev.PeerID != "" && !w.hasPrompt(p, "join\n"+ev.PeerID) {
			name := ev.Message
			if name == "" {
				name = "Someone"
			}
			w.prompts = append(w.prompts, &sharePrompt{p: p, peerID: ev.PeerID, name: name})
		}
	case keelclient.EventJoinResolved:
		w.dropPrompt(p, "join\n"+ev.PeerID)
	case keelclient.EventJoinFailed:
		msg := ev.Message
		if msg == "" {
			msg = "A viewer could not be connected"
		}
		w.warn(msg)
	case keelclient.EventControlRequest:
		if s := seatOf(st); ev.FromID != st.YouID && (s.driving || s.owner) {
			if !w.hasPrompt(p, fmt.Sprintf("control\n%d", ev.FromID)) {
				w.prompts = append(w.prompts, &sharePrompt{p: p, control: true, fromID: ev.FromID, name: clientName(st.Attached, ev.FromID)})
			}
		} else if ev.Message != "" {
			w.say(ev.Message)
		}
	case keelclient.EventControlNote:
		w.dropControlPrompts(p)
		w.say(ev.Message)
	}
}

func (w *workspace) hasPrompt(p *pane, key string) bool {
	return slices.ContainsFunc(w.prompts, func(s *sharePrompt) bool { return s.p == p && s.key() == key })
}

func (w *workspace) dropPrompt(p *pane, key string) {
	w.prompts = slices.DeleteFunc(w.prompts, func(s *sharePrompt) bool { return s.p == p && s.key() == key })
}

func (w *workspace) dropControlPrompts(p *pane) {
	w.prompts = slices.DeleteFunc(w.prompts, func(s *sharePrompt) bool { return s.p == p && s.control })
}

// forgetPrompts drops every prompt of a pane (the share stopped, or the pane closed).
func (w *workspace) forgetPrompts(p *pane) {
	w.prompts = slices.DeleteFunc(w.prompts, func(s *sharePrompt) bool { return s.p == p })
}

// answerJoin is the Allow and Deny buttons, and nothing else: it is the one place that answers a join request, and
// allow is true only for Allow. Deny, Escape, closing the window and the two-minute timeout never admit: the timeout
// is keel serve's (it denies the request itself and announces it resolved).
func (w *workspace) answerJoin(s *sharePrompt, allow bool) {
	if s.busy {
		return
	}
	s.busy = true
	sid := s.p.sessionID()
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		err := w.be.AdmitShare(c, sid, s.peerID, allow)
		w.post(func() {
			w.dropPrompt(s.p, s.key())
			if err != nil {
				w.fail("answer the request", err)
			}
		})
	})
}

// handOver is the control prompt's Hand over: grant-control to that person. Keep driving sends nothing.
func (w *workspace) handOver(s *sharePrompt) {
	w.dropPrompt(s.p, s.key())
	w.sendControl(func() error { return s.p.conn.GrantControl(s.fromID) }, "hand over control")
}

// passControl is the popover's Pass control to <name>.
func (w *workspace) passControl(p *pane, to uint64) {
	w.sendControl(func() error { return p.conn.GrantControl(to) }, "pass control")
}

// The words of ui/sharecontrol.js and ui/joinprompt.js.
func controlDesc(p *pane, owner bool) string {
	title := p.host.Name
	if title == "" {
		title = "this session"
	}
	until := "it is passed back"
	if owner {
		until = "you take it back"
	}
	return "Of " + title + ". You follow until " + until + "."
}

const joinNote = "The name is whatever they typed. Allow only if you're expecting them; they'll see output from when you started sharing."
