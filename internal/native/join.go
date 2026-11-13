package native

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/joinpeer"
	"keel/internal/native/keelclient"
	"keel/internal/native/layout"
)

// Join a session (docs/ui-redesign.md §5.7), as the app has it: features/share.md. The dialog is joinDialog in
// internal/ui/dialogs.templ; the joined tab is term.js joinRemote and attachFollowChannel; the peer is
// internal/native/joinpeer. A joined tab is a terminal and nothing else: it has no Vault sidebar, no Share, no
// snippets (typeTarget refuses it), it owns no session on this machine (closing it kills nothing), and it is never
// saved. The link's key is read by joinpeer.ParseLink and used by the peer to verify the host; it is not kept here:
// the dialog's text is dropped when Join is pressed, and the pane remembers only the origin and the room.

// The dialog's words (joinDialog in internal/ui/dialogs.templ).
const (
	joinTitle = "Join a session"
	joinDesc  = "Paste the Share link from the other Keel. You follow until they pass control. You never see their hosts or vault."
)

type joinDialog struct {
	link  string // what was pasted: it holds the key until Join is pressed
	focus bool
	h     ui.Handle
}

func (d *joinDialog) String() string   { return "{join dialog}" }
func (d *joinDialog) GoString() string { return d.String() }

// openJoin is ⌘J, the New tab page's Join session, the sidebar's menu and the palette.
func (w *workspace) openJoin() { w.joinDlg = &joinDialog{} }

func (w *workspace) closeJoin() {
	w.joinDlg = nil
	if t := w.tab(); t != nil {
		if t.panes[t.focused] != nil {
			w.wantFocus = t.focused
		} else if t.page != nil {
			t.page.focus = true
		}
	}
}

// submitJoin reads the link: the app's error is a toast and the dialog stays; otherwise the dialog closes, its text
// is dropped, and the join starts.
func (w *workspace) submitJoin() {
	d := w.joinDlg
	if d == nil {
		return
	}
	link, err := joinpeer.ParseLink(d.link)
	if err != nil {
		w.warn(err.Error())
		return
	}
	w.closeJoin()
	w.joinRemote(link)
}

// joinKey names a join without its key: the same room on the same server (term.js: `join:${origin}:${room}`).
func joinKey(l joinpeer.Link) string { return "join:" + l.Origin + ":" + l.Room }

// joinName is what the host is told: the display name, as the Sharing page says (term.js state.name).
func (w *workspace) joinName() string { return displayName(w.term.Name, defaultName()) }

// joinRemote opens a tab that follows the share. Pasting the link of a join that is already open shows that tab; one
// that was denied, failed verification or was lost is closed and joined again.
func (w *workspace) joinRemote(l joinpeer.Link) {
	k := joinKey(l)
	for _, t := range w.tabs {
		for _, p := range t.panes {
			if p.remote && p.joinKey == k {
				if p.conn.Snapshot().Status == "error" {
					w.closeTab(t)
					goto open
				}
				w.selectTab(w.tabIndex(t))
				return
			}
		}
	}
open:
	name := w.joinName()
	w.work(func() {
		conn, err := w.joiner(l, name)
		w.post(func() {
			switch {
			case err != nil:
				w.fail("join", err)
			case w.closed:
				conn.Close()
			default:
				p := w.newPane(keelclient.Host{Name: joinedFallback, Address: strings.TrimPrefix(strings.TrimPrefix(l.Origin, "https://"), "http://")}, conn)
				p.remote, p.joinKey = true, k
				t := &tab{root: layout.Leaf(p.id), panes: map[string]*pane{p.id: p}, focused: p.id, remote: true}
				w.tabs = append(w.tabs, t)
				w.selectTab(len(w.tabs) - 1)
			}
		})
	})
}

// joinedFallback is the pane's title until the host names its session (term.js joinRemote: "Follow").
const joinedFallback = "Follow"

// title is the name a pane shows: a joined pane takes the title of the host's session once it says it.
func (p *pane) title() string {
	if p.remote {
		if t := p.conn.Snapshot().Title; t != "" {
			return t
		}
	}
	return p.host.Name
}

// joinedActive reports that the tab shown is a joined one: it has no Vault sidebar (§5.7).
func (w *workspace) joinedActive() bool {
	t := w.tab()
	return t != nil && t.remote
}

// leaveJoin is the banner's Leave: the same as closing the tab. The peer connection closes; the host's session is
// not touched.
func (w *workspace) leaveJoin(t *tab) { w.closeTab(t) }

// followNotice is "Typing is off while you follow" with Ask for control (ui/sharecontrol.js wireFollowerNotice), shown
// when a follower types. Feedback only: nothing was sent.
const followNotice = "Typing is off while you follow"

func (w *workspace) noteBlocked(p *pane, st keelclient.State) {
	if st.Blocked == p.blockedSeen {
		return
	}
	p.blockedSeen = st.Blocked
	if seatOf(st).owner {
		w.sayAction(followNotice, "Take control", func() { w.sendControl(p.conn.TakeControl, "take control") })
		return
	}
	w.sayAction(followNotice, "Ask for control", func() { w.sendControl(p.conn.RequestControl, "ask for control") })
}
