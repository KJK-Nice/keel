package native

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"

	"github.com/egoist/mygo/ui"
)

// The sidebar's width (lib/sidebarwidth.js, term.js createSidebarResize): a whole number of pixels from size-rail up to twice
// the default, 0 for the default (size-sidebar, grown with the UI size), stepped by space-4, kept as sidebarWidth in
// keel-native.json.
const sidebarWidthFileKey = "sidebarWidth"

// sidebarMin is size-rail (208) in the app. Here the four section labels (Hosts, Panes 2, Snippets, Forwards) do not fit in
// 208 at any UI size (plan 031 step 1), so the minimum is the width they need (measured: 246 at the default UI size, only a
// little under the default 252), growing with the UI size as the labels do. The sidebar can be widened to twice the default
// but hardly narrowed; TestSidebarSegmentLabelsFitAtEveryUISize holds the number.
const sidebarLabelsMin = 246

func sidebarMin() float32 { return max(sizeRail, sidebarLabelsMin, rem(sidebarLabelsMin)) }
func sidebarMax() float32 { return 2 * sidebarWidth() }

// clampSidebarWidth is clampWidth: w rounded and held inside the bounds; 0 and less is the default.
func clampSidebarWidth(v float32) float32 {
	if v <= 0 {
		return sidebarWidth()
	}
	return min(sidebarMax(), max(sidebarMin(), float32(math.Round(float64(v)))))
}

// sidebarNow is the width the sidebar is drawn at.
func (w *workspace) sidebarNow() float32 { return clampSidebarWidth(w.sideWidth) }

// loadSidebarWidth is savedWidth: the width the sidebar was left at, 0 (the default) when absent or not a number.
func loadSidebarWidth(dataDir string) float32 {
	b, err := os.ReadFile(filepath.Join(dataDir, "keel-native.json"))
	if err != nil {
		return 0
	}
	var f struct {
		W float32 `json:"sidebarWidth"`
	}
	if json.Unmarshal(b, &f) != nil || f.W <= 0 || math.IsNaN(float64(f.W)) || math.IsInf(float64(f.W), 0) {
		return 0
	}
	return f.W
}

// setSidebarWidth keeps a new width (0 puts the default back) in keel-native.json.
func (w *workspace) setSidebarWidth(v float32) {
	if v > 0 {
		v = clampSidebarWidth(v)
	}
	w.sideWidth = v
	if w.dataDir != "" {
		if err := savePrefs(w.dataDir, map[string]any{sidebarWidthFileKey: v}); err != nil {
			w.fail("save the sidebar width", err)
		}
	}
}

// sidebarSash is the handle on the sidebar's right edge.
func (w *workspace) sidebarSash(c *ui.Context) {
	w.widthSash(c, &w.sideSash, sashSpec{
		label: "Resize sidebar", rightEdge: true, cur: w.sidebarNow(), step: space4,
		clamp: clampSidebarWidth, home: sidebarMin(), end: sidebarMax(),
		live: func(v float32) { w.sideWidth = v }, keep: w.setSidebarWidth,
	})
}

// The one sash (.k-agent-sash, sidebarresize.js): a strip on the edge of a panel whose width the user sets. The agent panel
// has it on its left edge and the vault sidebar on its right. It is MyGo's drag and focus on a plain box because ui.Split
// keeps only 40 DIPs from each edge and has no key steps, Home / End or saved width of the panel's own.

// sashState is a sash's own state: whether it is being dragged (the width is kept when it lets go) and its keyboard handle.
type sashState struct {
	drag bool
	h    ui.Handle
}

// sashSpec says what a sash resizes. Widths handed to keep are what is saved: 0 is the default.
type sashSpec struct {
	label     string
	rightEdge bool    // on the panel's right edge (a sidebar on the left): dragging right widens it
	cur       float32 // the width now
	step      float32 // what ← and → move by, ⇧ four times that
	clamp     func(float32) float32
	home, end float32       // what Home and End keep (end is clamped)
	live      func(float32) // the width while the pointer drags
	keep      func(float32) // the width to keep
}

// widthSash draws the sash in the panel being built (the panel is its parent) and applies what the user does with it. Dragging
// away from the panel widens it; ← and → step it, Home and End go to the ends, a double click or Enter puts the default back.
func (w *workspace) widthSash(c *ui.Context, s *sashState, sp sashSpec) {
	sign := float32(-1) // a sash on the left edge: dragging left widens
	b := ui.Box(c).Absolute().Top(0).Bottom(0).Width(space2).Cursor(ui.CursorResizeColumn).Focusable().Bind(&s.h).Label(sp.label)
	// The panel's 1 px border is outside the box's containing block, so the strip reaches one pixel over it and the visible
	// line (below) covers the border and the pixel inside it.
	if sp.rightEdge {
		sign = 1
		b.Right(-1)
	} else {
		b.Left(-1)
	}
	if dx, _, down := b.Dragged(); down {
		s.drag = true
		if dx != 0 {
			sp.live(sp.clamp(sp.cur + sign*dx))
		}
	} else if s.drag { // let go: keep the width
		s.drag = false
		w.act(func() { sp.keep(sp.cur) })
	}
	if b.DoubleClicked() {
		w.act(func() { sp.keep(0) })
	}
	b.ObserveInput(func(ev ui.InputEvent) {
		if ev.Kind != ui.InputKeyDown {
			return
		}
		step := sp.step
		if ev.Mods&ui.Shift != 0 {
			step *= 4
		}
		switch ev.Key {
		case ui.KeyLeft:
			w.act(func() { sp.keep(sp.clamp(sp.cur - sign*step)) })
		case ui.KeyRight:
			w.act(func() { sp.keep(sp.clamp(sp.cur + sign*step)) })
		case ui.KeyHome:
			w.act(func() { sp.keep(sp.home) })
		case ui.KeyEnd:
			w.act(func() { sp.keep(sp.clamp(sp.end)) })
		case ui.KeyEnter:
			w.act(func() { sp.keep(0) })
		}
	})
	if s.drag || b.Hovered() || s.h.FocusWithin(c) {
		b.Children(func() {
			l := ui.Box(c).Absolute().Top(0).Bottom(0).Width(2).Background(accentInk)
			if sp.rightEdge {
				l.Right(0)
			} else {
				l.Left(0)
			}
		})
	}
}
