package native

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"slime/internal/native/keelclient"
	"slime/internal/wire"
)

// Presence: who else is in a live pane of this window, said by a ring around the terminal and a notch on its top
// edge (docs/native.md, "Presence"). People who watch your pane are haze; someone else at the keys has their own
// colour, which their cursor takes too. You at the keys, alone, shows nothing.

type presenceKind int

const (
	presenceNone      presenceKind = iota
	presenceWatched                // you drive, and people who came by link watch
	presenceFollowing              // someone else drives (or nobody does), and you watch
)

type presence struct {
	kind  presenceKind
	who   []string // the watchers, in arrival order; or the driver alone ("" when nobody drives)
	owner bool     // following: you own the session, so you may take control back
}

// presenceOf reads a pane's state. A joined pane (§5.7) has its own banner and is never asked.
func presenceOf(st keelclient.State) presence {
	switch {
	case !st.Ready():
		return presence{}
	case st.Role == wire.RoleFollower || st.Role == wire.RoleOwner:
		pr := presence{kind: presenceFollowing, who: []string{""}, owner: st.OwnerID != 0 && st.OwnerID == st.YouID}
		if st.DriverID != 0 {
			pr.who[0] = clientName(st.Attached, st.DriverID)
		}
		return pr
	case st.Driving():
		var who []string
		for _, a := range st.Attached {
			if a.Kind == keelclient.KindJoined {
				who = append(who, watcherName(a.Name))
			}
		}
		if len(who) > 0 {
			return presence{kind: presenceWatched, who: who}
		}
	}
	return presence{}
}

func watcherName(name string) string {
	if name == "" {
		return "guest"
	}
	return name
}

// text is the notch's words: "Ann watching", "Ann, Bo watching", "Ann +2 watching"; "Ann is driving", "Nobody is driving".
func (pr presence) text() string {
	switch pr.kind {
	case presenceWatched:
		if len(pr.who) <= 2 {
			return strings.Join(pr.who, ", ") + " watching"
		}
		return fmt.Sprintf("%s +%d watching", pr.who[0], len(pr.who)-1)
	case presenceFollowing:
		if pr.who[0] == "" {
			return "Nobody is driving"
		}
		return pr.who[0] + " is driving"
	}
	return ""
}

// A driver's colour is one of ansi-1 … ansi-6: the normal ANSI colours, not the bright ones the host labels use,
// because the notch's small text in stage reads on them at 4.5:1 or more in both themes (the bright ones are under it in light).
const presenceFirst, presenceCount = 1, 6

// personColor is the colour of a person at the keys, the same for the same name in every window: an index into the
// normal ANSI colours, so it follows the theme.
func personColor(name string) ui.Color {
	h := uint32(2166136261) // FNV-1a, by hand: guard_test.go allows no Write outside the terminal view
	for i := 0; i < len(name); i++ {
		h = (h ^ uint32(name[i])) * 16777619
	}
	return ansi[presenceFirst+int(h%presenceCount)]
}

// color is the ring's and the notch's colour; the cursor takes it while someone else drives. Nobody driving is haze.
func (pr presence) color() ui.Color {
	if pr.kind == presenceFollowing && pr.who[0] != "" {
		return personColor(pr.who[0])
	}
	return haze
}

// cursorColor is the colour p's cursor should have: the driver's while someone else drives, else the theme's (zero).
func (pr presence) cursorColor() ui.Color {
	if pr.kind == presenceFollowing && pr.who[0] != "" {
		return pr.color()
	}
	return ui.Color{}
}

// keepCursor gives p's terminal the cursor colour pr asks for, once per change: the theme is rebuilt only then.
func (p *pane) keepCursor(pr presence) {
	if col := pr.cursorColor(); col != p.cursorCol {
		p.cursorCol = col
		if p.setTheme != nil {
			p.setTheme()
		}
	}
}

// presenceRing strokes the ring around a pane's terminal, as thick as the focus ring, in the presence's colour.
func presenceRing(pt *ui.Painter, r ui.Rect, pr presence) {
	if pr.kind != presenceNone {
		pt.Stroke(r, pr.color(), 0, ringWidth)
	}
}

// notchHeight is the lip the notch hangs in, above the terminal.
const notchHeight = space4 + space1

// presenceNotch is the tab hanging from the ring's top edge at the right: who watches (a click opens the Share
// popover with the people list) or who drives, with Take control or Ask for control.
func (w *workspace) presenceNotch(c *ui.Context, t *tab, p *pane, pr presence) {
	if pr.kind == presenceNone {
		return
	}
	lip := ui.Row(c).FillWidth().Height(notchHeight).Shrink(0).Justify(ui.End).Padding(0, space3, 0, 0)
	lip.Children(func() { w.notchFace(c, t, p, pr) })
}

func (w *workspace) notchFace(c *ui.Context, t *tab, p *pane, pr presence) {
	n := ui.Row(c).FillHeight().Padding(0, space2).Gap(space1_5).AlignItems(ui.Center).Radius(radiusXs).Background(pr.color())
	glyph := "steering"
	if pr.kind == presenceWatched {
		glyph = "eye"
		n.Label(fmt.Sprintf("%d watching", len(pr.who))).Cursor(ui.CursorPointer)
	} else {
		n.Label("presence following")
	}
	n.Children(func() {
		iconSized(c, glyph, stage, iconSizeSm)
		text(c, pr.text(), stage, kbdSize, weightSemibold)
		if pr.kind != presenceFollowing {
			return
		}
		word, ask := "Ask for control", p.conn.RequestControl
		if pr.owner {
			word, ask = "Take control", p.conn.TakeControl
		}
		text(c, "·", stage, kbdSize, weightRegular)
		b := ui.Row(c).Label(word).Cursor(ui.CursorPointer).Children(func() {
			text(c, word, stage, kbdSize, weightSemibold).Underline()
		})
		if b.Clicked() {
			w.act(func() { w.sendControl(ask, strings.ToLower(word)) })
		}
	})
	if pr.kind == presenceWatched && n.Clicked() {
		w.act(func() { w.pickPane(t, p.id); w.toggleShare() })
	}
}
