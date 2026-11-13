//go:build nativee2e

package native

import (
	"bytes"
	"image"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// Theme, UI size, the terminal font and splits that fit, against a scratch keel serve (every daemon has KEEL_KEYRING=off and
// its own KEEL_DATA under .scratch-native/e2e, on a loopback port the kernel chose).

// openSettings shows the Settings page (⌘,) and waits for its Appearance section.
func (e *win) openSettings() {
	e.t.Helper()
	e.key(ui.Cmd, ui.KeyComma)
	e.pump(func() bool { return e.hasLabel("tab Settings") && e.hasLabel("Color theme") }, "Settings · Appearance")
}

// wantPix is a pixel of the window at DIP coordinates being the colour of a token.
func (e *win) wantPix(what string, x, y float32, hex string) {
	e.t.Helper()
	if want := tok(hex); !is(e.pixel(x, y), want) {
		e.t.Errorf("%s at (%v,%v): got %v, want %s", what, x, y, e.pixel(x, y), hex)
	}
}

// paneBodyPix is a pixel in the empty lower part of pane id's terminal.
func (e *win) paneBodyPix(id string) [3]uint8 {
	r, ok := e.tt.Find("pane " + id)
	if !ok {
		e.t.Fatalf("pane %s not found", id)
	}
	return e.pixel(r.X+r.W/2, r.Y+r.H*0.85)
}

func TestE2EThemeSwitch(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.openPanes(2)
	e.settle(500 * time.Millisecond)
	e.wantPix("dark title bar", 900, 10, darkHex.chrome)
	if got := e.paneBodyPix("2"); !is(got, tok(darkHex.termBg)) {
		t.Errorf("dark terminal background %v", got)
	}
	e.openSettings()
	e.click("Light")
	e.pump(func() bool { return !e.w.dark }, "the light theme")
	e.settle(300 * time.Millisecond)
	e.wantPix("light title bar", 900, 10, lightHex.chrome)
	e.wantPix("light settings page", 900, 400, lightHex.surface)
	e.shot("TestE2EThemeSwitch-light-settings")
	// The tab with the two panes: its tile bodies and their terminals, drawn by the terminal plugin, are light.
	e.key(ui.Cmd, ui.Key1)
	e.settle(500 * time.Millisecond)
	e.wantPix("light title bar", 900, 10, lightHex.chrome)
	for _, id := range []string{"1", "2"} {
		if got := e.paneBodyPix(id); !is(got, tok(lightHex.termBg)) && !is(got, tok(lightHex.termBg).Alpha(dimAlpha).Over(tok(lightHex.chrome))) {
			t.Errorf("pane %s terminal background %v, want light stage %s", id, got, lightHex.termBg)
		}
	}
	p2, _ := e.tt.Find("pane 2")
	e.wantPix("light pane bar", p2.X+p2.W/2, p2.Y+3, lightHex.surface) // above the text
	e.shot("TestE2EThemeSwitch-light")
	// A dialog in the light theme: MyGo's Modal chrome, the check boxes and the badges of the form (plan 031 step 2).
	e.click("New host")
	e.pump(func() bool { return e.hasLabel("dialog New host") }, "the New host dialog")
	e.shot("TestE2EThemeSwitch-light-dialog")
	e.key(0, ui.KeyEscape)
	e.pump(func() bool { return e.w.hostDlg == nil }, "the dialog closed")
	// Back to dark in the same window.
	e.openSettings()
	e.click("Dark")
	e.pump(func() bool { return e.w.dark }, "the dark theme")
	e.key(ui.Cmd, ui.Key1)
	e.settle(500 * time.Millisecond)
	e.wantPix("dark title bar", 900, 10, darkHex.chrome)
	if got := e.paneBodyPix("2"); !is(got, tok(darkHex.termBg)) {
		t.Errorf("dark terminal background after switching back %v", got)
	}
	// The choice is kept, and a second window starts in it.
	e.openSettings()
	e.click("Light")
	e.pump(func() bool { return !e.w.dark }, "the light theme again")
	b, err := os.ReadFile(filepath.Join(dir, "keel-native.json"))
	if err != nil || !bytes.Contains(b, []byte(`"theme": "light"`)) {
		t.Fatalf("keel-native.json (err %v) has no light theme", err)
	}
	e.w.closeWindow()
	e2 := newWin(t, addr, dir, nil)
	e2.useSavedWindowPrefs()
	e2.pump(func() bool { return e2.hasLabel("host Local Shell") }, "the host tree")
	e2.settle(300 * time.Millisecond)
	if e2.w.dark || e2.w.prefs.Theme != themeLight {
		t.Fatalf("the second window is dark=%v with choice %q", e2.w.dark, e2.w.prefs.Theme)
	}
	img := e2.tt.Image() // the window is translucent (opacity 66 is the default): the ground is the token at that alpha
	s := float32(img.Bounds().Dx()) / 1280
	wantPixel(t, "second window title bar", img, 900*s, 10*s, tok(lightHex.chrome), e2.w.alpha)
	e2.w.closeWindow()
}

// crop copies a region of the window image, in DIPs, so a later frame cannot change it.
func (e *win) crop(r ui.Rect) *image.RGBA {
	img := e.tt.Image()
	s := float64(img.Bounds().Dx()) / 1280
	b := image.Rect(int(float64(r.X)*s), int(float64(r.Y)*s), int(float64(r.X+r.W)*s), int(float64(r.Y+r.H)*s)).Intersect(img.Bounds())
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		copy(out.Pix[y*out.Stride:], img.Pix[img.PixOffset(b.Min.X, b.Min.Y+y):img.PixOffset(b.Max.X, b.Min.Y+y)])
	}
	return out
}

func TestE2EUISizeAndFont(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	sid := e.openLocal()
	e.clickBody("1")
	e.typeLine("echo 'Keel 0Oo1lI {}[] wmi'")
	e.waitScrollback(sid, "Keel 0Oo1lI")
	e.settle(500 * time.Millisecond)
	cols := func() int {
		for _, s := range e.sessions() {
			if s.ID == sid {
				return s.Cols
			}
		}
		return -1
	}
	colsBefore := cols()
	row, _ := e.tt.Find("host Local Shell")
	status, _ := e.tt.Find("keel serve · 1 session")
	paneBefore, _ := e.tt.Find("pane 1")
	e.shot("TestE2EUISizeAndFont-default")

	// UI size to its maximum (16): the app's setting scales what is written in rem (text), not the px sizes (a row, the
	// title bar, the sidebar), and not the terminal, so the session keeps its columns.
	e.openSettings()
	e.click("UI size slider")
	for e.w.prefs.UISize < uiSizeMax {
		e.key(0, ui.KeyRight)
	}
	e.key(ui.Cmd, ui.Key1)
	e.settle(500 * time.Millisecond)
	row2, _ := e.tt.Find("host Local Shell")
	status2, _ := e.tt.Find("keel serve · 1 session")
	if row2.H != row.H || row.H != sizeControl {
		t.Errorf("the sidebar row is %v high at 16 and %v at 12: the app's row is px (size-control %d)", row2.H, row.H, sizeControl)
	}
	if got, want := status2.H/status.H, float32(16)/12; got < want*0.93 || got > want*1.07 {
		t.Errorf("the status text grew %vx, want about %v (16 over the default 12)", got, want)
	}
	if want := float32(13 * 16.0 / 12); uiFontSize < want-0.01 || uiFontSize > want+0.01 { // plan 030: MyGo's 13 at the default size, scaled
		t.Errorf("body text is %v px at UI size 16, want %v", uiFontSize, want)
	}
	// Plan 031: the sidebar is wider at size 16 (its labels scale), so the stage is narrower by that much and no more:
	// the cell is the same size, the columns lost are the sidebar's growth over the cell width.
	cell := paneBefore.W / float32(colsBefore)
	lost := (sidebarWidth() - sizeSidebar) / cell
	if got := float32(cols()); got < float32(colsBefore)-lost-1.5 || got > float32(colsBefore)-lost+1.5 {
		t.Errorf("the session has %v columns after UI size 16, %d before, about %.1f of them gone with the sidebar's growth: the terminal is scaled", got, colsBefore, lost)
	}
	e.shot("TestE2EUISizeAndFont-ui16")
	e.openSettings()
	e.click("UI size slider")
	for e.w.prefs.UISize > uiSizeDefault {
		e.key(0, ui.KeyLeft)
	}

	// Terminal · Font to IBM Plex Mono: the text of the open pane is drawn in another face.
	e.key(ui.Cmd, ui.Key1)
	e.settle(500 * time.Millisecond)
	pane, _ := e.tt.Find("pane 1")
	area := ui.Rect{X: pane.X, Y: pane.Y + sizePanebar, W: pane.W, H: 120}
	jb := e.crop(area)
	e.shot("TestE2EUISizeAndFont-jetbrains")
	e.openSettings()
	e.click("settings nav Terminal")
	e.pump(func() bool { return e.hasLabel("Terminal font picker") }, "Terminal · Font")
	e.click("Terminal font picker")
	if err := e.tt.ChooseMenuItem("IBM Plex Mono"); err != nil {
		t.Fatal(err)
	}
	e.step()
	e.pump(func() bool { return e.w.prefs.TermFont == fontPlex }, "the font choice")
	e.key(ui.Cmd, ui.Key1)
	e.settle(800 * time.Millisecond)
	plex := e.crop(area)
	e.shot("TestE2EUISizeAndFont-plex")
	if bytes.Equal(jb.Pix, plex.Pix) {
		t.Error("the terminal text is drawn the same in IBM Plex Mono and in JetBrains Mono")
	}
	if got := e.w.focusedPane().fontFamily; got == "" || got[:len(plexFamily)] != plexFamily {
		t.Errorf("the pane's family is %q", got)
	}
	b, err := os.ReadFile(filepath.Join(dir, "keel-native.json"))
	if err != nil || !bytes.Contains(b, []byte(`"termFont": "ibmplex"`)) || !bytes.Contains(b, []byte(`"uiSize": 12`)) {
		t.Errorf("keel-native.json (err %v): %s", err, b)
	}
	e.w.closeWindow()
}

func TestE2EFourPanesFit(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.openPanes(4)
	e.settle(500 * time.Millisecond)
	assertFits(t, e.w, e.tt, 1280, 800)
	e.shot("TestE2EFourPanesFit")
	last := e.paneSID("4")
	e.clickBody("4")
	e.typeLine("echo four-$((2+2))")
	e.waitScrollback(last, "four-4")
	// A small window: 900 by 600, the sidebar shown, then hidden.
	e.tt.SetSize(900, 600)
	e.settle(500 * time.Millisecond)
	assertFits(t, e.w, e.tt, 900, 600)
	e.shot("TestE2EFourPanesFit-900")
	e.clickBody("4")
	e.typeLine("echo small-$((3+4))")
	e.waitScrollback(last, "small-7")
	e.key(ui.Cmd, ui.KeyB)
	e.settle(300 * time.Millisecond)
	assertFits(t, e.w, e.tt, 900, 600)
	e.w.closeWindow()
}
