package native

import (
	"slices"
	"testing"
	"time"
)

// A copy is found on the screen's text: its one place as cells (wide characters two), or nothing when it is not on
// the screen, is there more than once, or the screen is a full-screen program's.
func TestCopiedSpansFindTheCopyOnTheScreen(t *testing.T) {
	snap := "\x1b[?1h\x1b[Hold scrollback\r\n$ echo hi\r\nhi there\r\n日本 ok\r\nlast line\r\n$ \x1b[5;3H\x1b[0m"
	for name, tc := range map[string]struct {
		text string
		want []cellSpan
	}{
		"one word":                      {"there", []cellSpan{{1, 3, 5}}},
		"wide characters":               {"ok", []cellSpan{{2, 5, 2}}},
		"lines":                         {"hi\nhi there\n日本 ok\nlast", []cellSpan{{0, 7, 2}, {1, 0, 8}, {2, 0, 7}, {3, 0, 4}}},
		"a trailing newline and spaces": {"last line  \n", []cellSpan{{3, 0, 9}}},
		"twice":                         {"hi", nil},
		"in the scrollback":             {"old", nil},
		"blank":                         {"  \n", nil},
	} {
		got, ok := copiedSpans(snap, 5, tc.text)
		if ok != (tc.want != nil) || !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v %v, want %v", name, got, ok, tc.want)
		}
	}
	if _, ok := copiedSpans("\x1b[?1049h\x1b[Hvim there\x1b[1;1H", 5, "there"); ok {
		t.Error("a full-screen program's screen is not read")
	}
}

// The setting is kept and reaches open panes; a copy flashes in the pane's overlay and fades within duration-slow;
// with the setting off nothing flashes.
func TestCopyFlashShowsAndFades(t *testing.T) {
	r := newRigIn(t, settingsBackend(), t.TempDir())
	r.w.openHost(testHosts[1])
	r.tt.Frame()
	p := r.w.focusedPane()
	snap := "\x1b[Hguest@keel $ echo flash\r\nflash me\r\n$ \x1b[3;3H\x1b[0m"
	p.onCopy = func(text string) { p.copied(snap, 3, text) }
	p.onCopy("flash me")
	r.tt.Frame()
	if !r.has("copy flash") {
		t.Fatal("no flash after a copy")
	}
	r.tt.Advance(durationSlow + 60*time.Millisecond)
	r.tt.Frame()
	if r.has("copy flash") || p.flash != nil {
		t.Error("the flash stays after duration-slow")
	}
	r.w.setCopyFlash(false)
	p.onCopy("flash me")
	r.tt.Frame()
	if r.has("copy flash") {
		t.Error("a flash with the setting off")
	}
	r.w.flushPrefs()
	if loadPrefs(r.w.dataDir).CopyFlash || defaultPrefs().CopyFlash != true {
		t.Error("the setting is not kept, or is not on by default")
	}
}
