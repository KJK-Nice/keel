package native

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// One empty state for everything that can be empty (plan 028 step 4), in two sizes: the page size for a tab page or the
// stage, the side size for a sidebar list, a panel, a picker or a drawer. It stands for the app's k-empty and k-side-empty
// (docs/design/components.css, internal/ui/lists.templ sideEmpty) and says which of three states a list is in:
//
//	nothing yet     the title and the words of the app's own empty list, and the action that makes the first item
//	no match        "Nothing matches “<filter>”" and Clear filter
//	cannot load     "Cannot load <what>", the error line and Try again
//
// The actions are named "empty <label>" for the tester and assistive tech: a page's own button of the same words stays
// the first match.

type emptySpec struct {
	icon, title, desc string
	action, secondary string // the primary action and a quiet one, by their words; "" for none
	keys              string // the page's key cap, beside the primary action
}

// The words of the app's own empty lists (internal/ui/lists.templ, the line of each is in its comment).
const (
	snippetsNone   = "Save commands you type often. Typing one always goes through Approve before type."                               // lists.templ:251
	notebooksNone  = "Write markdown runbooks. Fenced sh and bash blocks fill a terminal's input; nothing is typed until you confirm." // lists.templ:283
	forwardsNone   = "Local (-L), remote (-R) or dynamic SOCKS5 (-D), each through a saved host."                                      // lists.templ:184
	identitiesNone = "Add an SSH key or a password and attach it to hosts."                                                            // lists.templ:124
	logsNone       = "Sessions, forwards and vault changes show up here."                                                              // lists.templ:326
	runbooksNone   = "Write the steps you take for a known problem. The agent can walk you through them, one proposal at a time."      // runbooksview.go, the app's runbooks page
)

// emptyNone is the list with nothing in it yet; action is what makes the first item.
func emptyNone(icon, title, desc, action string) emptySpec {
	return emptySpec{icon: icon, title: title, desc: desc, action: action}
}

// emptySentences is emptyNone for the app's one-line empty words ("No hosts in this folder yet. Drag hosts onto it in the
// sidebar."): the first sentence is the title, the rest the description.
func emptySentences(icon, words, action string) emptySpec {
	title, desc, _ := strings.Cut(words, ". ")
	return emptyNone(icon, strings.TrimSuffix(title, "."), desc, action)
}

// emptyNoMatch is a list with items that its filter hides.
func emptyNoMatch(icon, filter string) emptySpec {
	return emptySpec{icon: icon, title: "Nothing matches “" + filter + "”", desc: "Try other words, or clear the filter.", action: "Clear filter"}
}

// emptyFailed is a list that could not be read.
func emptyFailed(icon, what, err string) emptySpec {
	return emptySpec{icon: icon, title: "Cannot load " + what, desc: err, action: "Try again"}
}

// hostsEmpty is the sidebar's Hosts list with nothing to show: no host yet, or none that the filter keeps. Before the first
// listing is read it shows nothing.
func (w *workspace) hostsEmpty(c *ui.Context) {
	switch {
	case !w.live:
	case len(w.hosts) == 0:
		if emptySide(c, emptyNone("server", "No hosts yet", "Add a host, or paste an ssh command into a New tab to save it.", "New host")) {
			w.act(w.newHost)
		}
	default:
		if emptySide(c, emptyNoMatch("search-01", w.filter)) {
			w.act(func() { w.filter = "" })
		}
	}
}

// emptyPage is the page size: the page's glyph on a round tile of surface, the title, one or two lines, then at most one
// primary and one quiet action. It sits about 38% of the way down the free height, never against the page header. The
// caller gives it the room the list would have had (not inside a Scroll). It reports a press of each action.
func emptyPage(c *ui.Context, s emptySpec) (primary, secondary bool) {
	ui.Column(c).Grow(1).FillWidth().Children(func() {
		ui.Box(c).Grow(emptyAbove)
		ui.Column(c).FillWidth().Gap(space3).AlignItems(ui.Center).Padding(0, space4).Children(func() {
			ui.Row(c).Size(tileLgSize, tileLgSize).Radius(radiusFull).Justify(ui.Center).AlignItems(ui.Center).Shrink(0).Background(surface).
				Border(1, line).Children(func() { iconSized(c, s.icon, inkFaint, tileLgIcon) })
			text(c, s.title, ink, titleSize, weightSemibold).MaxLines(2).TextAlign(ui.Center)
			if s.desc != "" {
				ui.Text(c, s.desc).TextColor(inkMuted).FontSize(captionSize).MaxLines(0).TextAlign(ui.Center).MaxWidth(rem(emptyDescPx))
			}
			if s.action != "" || s.secondary != "" {
				ui.Row(c).Gap(space2).AlignItems(ui.Center).Padding(space1, 0, 0).Children(func() {
					if s.action != "" {
						primary = textBtnAs(c, "empty "+s.action, s.action, btnPrimary)
						if s.keys != "" {
							keyCaps(c, s.keys)
						}
					}
					if s.secondary != "" {
						secondary = textBtnAs(c, "empty "+s.secondary, s.secondary, btnGhost)
					}
				})
			}
		})
		ui.Box(c).Grow(100 - emptyAbove)
	})
	return
}

// emptyStage is emptyPage on the stage ground, for a page with nothing else on it.
func (w *workspace) emptyStage(c *ui.Context, s emptySpec) (primary, secondary bool) {
	ui.Column(c).Grow(1).FillWidth().FillHeight().Background(w.ground(stage)).Children(func() { primary, secondary = emptyPage(c, s) })
	return
}

// emptySide is the side size: no tile, a small icon, the title, a caption and an optional text button. It reports a press
// of the action. Its padding and gaps are the same in every list.
func emptySide(c *ui.Context, s emptySpec) (primary bool) {
	ui.Column(c).FillWidth().Padding(space6, space2).Gap(space1).AlignItems(ui.Center).Children(func() {
		if s.icon != "" {
			icon(c, s.icon, inkFaint)
		}
		bodyStrong(c, s.title, ink).MaxLines(2).TextAlign(ui.Center)
		if s.desc != "" {
			ui.Text(c, s.desc).TextColor(inkMuted).FontSize(captionSize).MaxLines(0).TextAlign(ui.Center)
		}
		if s.action != "" {
			primary = textBtnAs(c, "empty "+s.action, s.action, btnGhost)
		}
	})
	return
}
