package native

import (
	"slices"
	"strings"
	"testing"

	"keel/internal/native/keelclient"
)

// Test ports of cmd/keel/web/js/test/notebooks.test.mjs: the trace words, attaching, and the card words and actions. (The
// panel cases of that file, which drive createAgentPanel with a fake DOM, are agentnotebooks_test.go.)

func TestTheTraceSaysWhichNotebooksWereSentReadOrNotSent(t *testing.T) {
	for _, c := range []struct {
		step keelclient.TraceStep
		want string
	}{
		{keelclient.TraceStep{Status: "notebook", Notebook: "Patroni failover", Bytes: 2048, Redactions: map[string]int{"secret": 2, "captured": 1}}, "Sent notebook Patroni failover (2048 bytes, 3 redacted)"},
		{keelclient.TraceStep{Status: "notebook", Notebook: "Big", Bytes: 32768, Truncated: true}, "Sent notebook Big (32768 bytes, cut by Keel, 0 redacted)"},
		{keelclient.TraceStep{Status: "notebook", Notebook: "Ops", Bytes: 10, Asked: true}, "Read notebook Ops (10 bytes, 0 redacted) · the agent asked"},
		{keelclient.TraceStep{Status: "notebook", Notebook: "One", Bytes: 1}, "Sent notebook One (1 byte, 0 redacted)"},
		{keelclient.TraceStep{Status: "notebook-index", Count: 3}, "Sent the titles of 3 notebooks (not private)"},
		{keelclient.TraceStep{Status: "notebook-index", Count: 1}, "Sent the titles of 1 notebook (not private)"},
		{keelclient.TraceStep{Status: "skipped", Reason: "notebook-missing"}, "Skipped a notebook · it was deleted"},
		{keelclient.TraceStep{Status: "skipped", Reason: "notebook-private"}, "Not sent · a private notebook (the agent cannot read it)"},
		{keelclient.TraceStep{Status: "skipped", Reason: "notebook-limit"}, "Stopped reading notebooks · Keel's limit for one question"},
	} {
		if got := traceText(c.step); got != c.want {
			t.Errorf("traceText(%+v) = %q, want %q", c.step, got, c.want)
		}
		if !isNotebookStep(c.step) {
			t.Errorf("%+v is a notebook step", c.step)
		}
	}
	for _, s := range []keelclient.TraceStep{{Status: "read"}, {Status: "skipped", Reason: "not-open"}, {Status: "runbook"}, {Status: "notes"}} {
		if isNotebookStep(s) {
			t.Errorf("%+v is not a notebook step", s)
		}
	}
}

func TestAttachingIsAtMostTwoOnlySavedNotebooksNeverAPrivateOne(t *testing.T) {
	list0 := []keelclient.Notebook{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "p", Private: true}}
	ids := toggleAttached(nil, "a", list0)
	if got := toggleAttached(ids, "p", list0); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("a private notebook is not attached: %v", got)
	}
	ids = toggleAttached(ids, "b", list0)
	if got := toggleAttached(ids, "c", list0); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("a third is not added: %v", got)
	}
	if got := toggleAttached(ids, "a", list0); !slices.Equal(got, []string{"b"}) {
		t.Fatal(got)
	}
	if got := toggleAttached(ids, "", list0); !slices.Equal(got, ids) {
		t.Fatal(got)
	}
	if got := reconcileAttached([]string{"a", "gone", "a", "b", "p"}, list0); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("made private or gone: dropped, once each: %v", got)
	}
	list := attachable([]keelclient.Notebook{{ID: "z", Name: "zeta"}, {ID: "p", Name: "pinned", Pinned: true}, {ID: "a", Name: "alpha"}, {Name: "unsaved"}})
	var got []string
	for _, n := range list {
		got = append(got, n.ID)
	}
	if !slices.Equal(got, []string{"p", "a", "z"}) {
		t.Fatal(got)
	}
	if attachHint(0) != "" || attachHint(1) != "This notebook is sent, redacted, with each question until you remove it." ||
		attachHint(2) != "These notebooks are sent, redacted, with each question until you remove them." {
		t.Fatal(attachHint(1), attachHint(2))
	}
	if !strings.HasPrefix(privateRefused, "This notebook is private: the agent cannot read it.") {
		t.Fatal(privateRefused)
	}
}

func TestCardWordsAndActionsFollowWhatKeelServeDid(t *testing.T) {
	np := func(kind, status string) keelclient.NotebookProposal {
		return keelclient.NotebookProposal{Kind: kind, Status: status, NotebookID: "n"}
	}
	eq := func(got []string, want ...string) {
		t.Helper()
		if !slices.Equal(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if cardHead(np("create", "created")) != "Created notebook" || cardHead(np("edit", "edited")) != "Edited notebook" || cardHead(np("delete", "proposed")) != "Delete notebook?" {
		t.Fatal("heads")
	}
	eq(cardActions(np("create", "created")), "open", "undo")
	p := np("create", "undone")
	p.VersionID = "v"
	eq(cardActions(p), "restore")
	if !strings.Contains(cardStatus(np("edit", "edited")), "Undo puts it back") {
		t.Fatal(cardStatus(np("edit", "edited")))
	}
	eq(cardActions(np("edit", "edited")), "open", "undo")
	conflict := np("edit", "refused")
	conflict.Reason = "conflict"
	if !strings.Contains(cardStatus(conflict), "You changed it after the agent read it") {
		t.Fatal(cardStatus(conflict))
	}
	private := np("edit", "refused")
	private.Reason = "private"
	eq(cardActions(private))
	eq(cardActions(np("delete", "proposed")), "open", "keep", "delete")
	del := np("delete", "deleted")
	del.VersionID = "v"
	eq(cardActions(del), "restore")
	expired := np("edit", "edited")
	expired.Expired = true
	eq(cardActions(expired), "open") // an expired card cannot undo
	if !strings.Contains(cardStatus(keelclient.NotebookProposal{Status: "open"}), "earlier version of Keel") {
		t.Fatal(cardStatus(keelclient.NotebookProposal{Status: "open"}))
	}
	if c := deleteConfirmText(keelclient.NotebookProposal{Title: "Ops"}); !strings.Contains(c, "“Ops”") || !strings.Contains(c, "Recently deleted") {
		t.Fatal(c)
	}
	if warned(keelclient.NotebookProposal{PanesRead: []string{}}) || !warned(keelclient.NotebookProposal{PanesRead: []string{"web-1"}}) || !warned(keelclient.NotebookProposal{FromPane: true}) {
		t.Fatal("warned")
	}
	if w := paneWarning(keelclient.NotebookProposal{FromPane: true, SeenIn: "db-2"}); !strings.Contains(w, "matches pane db-2") {
		t.Fatal(w)
	}
}

// Every kind and status the service writes has a head, a status line (or none) and the buttons the app's table gives.
func TestEveryKindAndStatusHasItsHeadStatusAndButtons(t *testing.T) {
	type row struct {
		kind, status, head string
		status_ok          bool // a status line exists
		actions            []string
	}
	rows := []row{
		{"create", "created", "Created notebook", true, []string{"open", "undo"}},
		{"create", "undone", "Created notebook", true, nil},
		{"create", "refused", "Notebook not created", true, nil},
		{"edit", "edited", "Edited notebook", true, []string{"open", "undo"}},
		{"edit", "undone", "Edited notebook", true, []string{"open"}},
		{"edit", "refused", "Notebook not changed", true, []string{"open"}},
		{"delete", "proposed", "Delete notebook?", true, []string{"open", "keep", "delete"}},
		{"delete", "deleted", "Delete notebook", true, nil},
		{"delete", "dismissed", "Delete notebook", true, []string{"open"}},
		{"delete", "restored", "Delete notebook", true, []string{"open"}},
		{"", "saved", "Notebook draft", true, []string{"open"}},
		{"", "dismissed", "Notebook draft", true, nil},
		{"", "open", "Notebook draft", true, nil},
	}
	for _, r := range rows {
		p := keelclient.NotebookProposal{Kind: r.kind, Status: r.status, NotebookID: "n"}
		if cardHead(p) != r.head || (cardStatus(p) != "") != r.status_ok || !slices.Equal(cardActions(p), r.actions) {
			t.Errorf("%s/%s: head %q status %q actions %v", r.kind, r.status, cardHead(p), cardStatus(p), cardActions(p))
		}
	}
	for reason := range refusedTexts {
		if refusedText(reason) == refusedText("invalid") {
			t.Errorf("%s has no words of its own", reason)
		}
	}
	if refusedText("invalid") != "The agent's edit was not valid. Nothing changed." {
		t.Fatal(refusedText("invalid"))
	}
	for _, reason := range []string{"private", "not-found", "deleted"} {
		p := keelclient.NotebookProposal{Kind: "edit", Status: "refused", Reason: reason, NotebookID: "n"}
		if len(cardActions(p)) != 0 {
			t.Errorf("%s: %v", reason, cardActions(p))
		}
	}
}
