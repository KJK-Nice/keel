package native

import (
	"slices"
	"strings"
	"testing"
)

// Plan 028 step 4: one empty state. Each place that can be empty renders it, its action does what the page's own button
// does, and where a list can be filtered or can fail the three states are told apart.

// emptyTitle is the title emptySentences makes of one of the app's empty lines.
func emptyTitle(words string) string { return emptySentences("", words, "").title }

func emptyRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t, okBackend())
	r.tt.Frame()
	return r
}

func TestSidebarHostsEmptyStates(t *testing.T) {
	r := newRig(t, &fakeBackend{})
	r.tt.Frame()
	r.texts(t, "No hosts yet")
	r.click(t, "empty New host") // the sidebar's New host does the same
	if r.w.hostDlg == nil {
		t.Error("the empty state's New host did not open the host dialog")
	}

	r = emptyRig(t)
	r.w.filter = "zzz"
	r.tt.Frame()
	r.texts(t, "Nothing matches “zzz”", "empty Clear filter")
	r.click(t, "empty Clear filter")
	if r.w.filter != "" || r.tt.HasText("Nothing matches") {
		t.Errorf("Clear filter left %q", r.w.filter)
	}
}

func TestSidebarListsEmptyStates(t *testing.T) {
	for _, c := range []struct {
		seg, title, action string
		did                func(*workspace) bool
	}{
		{"Snippets", "No snippets yet", "empty New snippet", func(w *workspace) bool { return w.snipDlg != nil }},
		{"Forwards", "No forwards yet", "New forward", func(w *workspace) bool { return w.fwdDlg != nil }}, // the button below the words
	} {
		r := emptyRig(t)
		r.click(t, c.seg)
		r.texts(t, c.title)
		r.click(t, c.action)
		if !c.did(r.w) {
			t.Errorf("%s: the empty state's action did not do what the section's does", c.seg)
		}
	}
	r := emptyRig(t) // the Snippets segment also holds the Notebooks half
	r.click(t, "Snippets")
	r.texts(t, "No notebooks yet", "New notebook") // the button below the words, not a second one
}

func TestPageEmptyStates(t *testing.T) {
	for _, c := range []struct {
		page, title, action string
		did                 func(*workspace) bool
	}{
		{"snippets", "No snippets yet", "empty New snippet", func(w *workspace) bool { return w.snipDlg != nil }},
		{"notebooks", "No notebooks yet", "empty New notebook", func(w *workspace) bool { return slices.ContainsFunc(w.tabs, (*tab).onlyNotebooks) }},
		{"forwards", "No forwards yet", "empty New forward", func(w *workspace) bool { return w.fwdDlg != nil }},
		{"keychain", "No identities yet", "empty New identity", func(w *workspace) bool { return w.identDlg != nil }},
		{"runbooks", "No runbooks yet", "empty New runbook", func(w *workspace) bool { return w.tab().pg.runbooks.ed != nil }},
	} {
		r := emptyRig(t)
		r.w.openPage(c.page)
		r.tt.Frame()
		r.tt.Frame()
		r.texts(t, c.title, c.action)
		r.click(t, c.action)
		if !c.did(r.w) {
			t.Errorf("%s page: the empty state's action did not do what the page's does", c.page)
		}
	}
	r := emptyRig(t)
	r.w.openPage("logs")
	r.tt.Frame()
	r.tt.Frame()
	r.texts(t, "No log entries")
}

func TestLogsCannotLoadHasTryAgain(t *testing.T) {
	r := newRig(t, logsBackend())
	r.tt.Frame()
	r.w.openPage("logs")
	r.tt.Frame()
	r.w.tab().pg.logs.err = "keel serve is not answering"
	r.tt.Frame()
	r.texts(t, "Cannot load the log", "keel serve is not answering", "empty Try again")
	before := r.fb.logsCalls
	r.click(t, "empty Try again")
	if r.fb.logsCalls == before {
		t.Error("Try again did not read the log again")
	}
}

func TestEmptyPageSitsAboveTheMiddleAndBelowTheHeader(t *testing.T) {
	r := emptyRig(t)
	r.w.openPage("forwards")
	r.tt.Frame()
	r.tt.Frame()
	title, ok := r.tt.Find("No forwards yet")
	if !ok {
		t.Fatal("no empty state on the forwards page")
	}
	// The window is 720 high: the header and the filter take the top 150, the status bar the bottom 24. The block's
	// title sits above the middle of what is left (about 38% of the free height above the block) and below the header.
	if mid := float32(150+696) / 2; title.Y > mid || title.Y < 150 {
		t.Errorf("the title sits at y=%v: want between the header (150) and the middle (%v)", title.Y, mid)
	}
}

func TestPaletteWithNoMatchSaysSo(t *testing.T) {
	r := emptyRig(t)
	r.openPal(t)
	r.tt.Type("zzzqqq")
	r.tt.Frame()
	if !r.tt.HasText("No matching commands") {
		t.Errorf("no empty row: %v", strings.Join(r.tt.Texts(), "|"))
	}
}
