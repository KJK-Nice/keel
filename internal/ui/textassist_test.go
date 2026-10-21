package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"golang.org/x/net/html"

	"keel/internal/store"
)

// The native window is a WKWebView, which capitalizes the first letter and
// autocorrects words in text fields. Hostnames, usernames, paths, commands
// and filters are not prose, so every text-like input and textarea Keel
// renders turns all three off. Prose fields may keep spellcheck (and only
// spellcheck): proseFields below.

// textLikeTypes are the input types WebKit offers text assistance on.
var textLikeTypes = map[string]bool{
	"": true, "text": true, "search": true, "email": true, "url": true,
	"tel": true, "password": true,
}

// proseFields are the fields where spellcheck stays on: the agent chat
// composers and host notes. Autocapitalize and autocorrect are off there too.
var proseFields = map[string]bool{
	"agent-input":      true,
	"agentpage-input":  true,
	"data-notes-input": true,
}

func fieldKey(attrs map[string]string) string {
	if id := attrs["id"]; id != "" {
		return id
	}
	if _, ok := attrs["data-notes-input"]; ok {
		return "data-notes-input"
	}
	if n := attrs["name"]; n != "" {
		return "name=" + n
	}
	return fmt.Sprint(attrs)
}

// textAssistProblems lists every text-like field in doc that leaves
// autocapitalize, autocorrect or spellcheck on.
func textAssistProblems(doc string) ([]string, int) {
	var problems []string
	fields := 0
	z := html.NewTokenizer(strings.NewReader(doc))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			return problems, fields
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		tok := z.Token()
		if tok.Data != "input" && tok.Data != "textarea" {
			continue
		}
		attrs := map[string]string{}
		for _, a := range tok.Attr {
			attrs[a.Key] = a.Val
		}
		if tok.Data == "input" && !textLikeTypes[strings.ToLower(attrs["type"])] {
			continue
		}
		fields++
		key := fieldKey(attrs)
		if attrs["autocapitalize"] != "off" {
			problems = append(problems, key+`: autocapitalize="off" missing`)
		}
		if attrs["autocorrect"] != "off" {
			problems = append(problems, key+`: autocorrect="off" missing`)
		}
		want := "false"
		if proseFields[key] {
			want = "true"
		}
		if attrs["spellcheck"] != want {
			problems = append(problems, fmt.Sprintf("%s: spellcheck=%q, want %q", key, attrs["spellcheck"], want))
		}
	}
}

func TestTextFieldsHaveNoTextAssist(t *testing.T) {
	p := PageProps{
		Hosts:      []Host{{ID: "h1", Name: "web", Address: "10.0.0.1", Username: "ops"}},
		Identities: []store.Identity{{ID: "k1", Name: "deploy", Kind: "key"}},
		Snippets:   []store.Snippet{{ID: "s1", Name: "disk", Command: "df -h"}},
	}
	pages := map[string]templ.Component{
		"Page":            Page(p),
		"HostList":        HostList(p),
		"IdentityList":    IdentityList(p),
		"TunnelList":      TunnelList(p),
		"SnippetList":     SnippetList(p),
		"NotebookList":    NotebookList(p),
		"LogList":         LogList(p),
		"SidebarSnippets": SidebarSnippets(p),
		"SidebarForwards": SidebarForwards(p),
	}
	total := 0
	for name, c := range pages {
		problems, n := textAssistProblems(renderString(t, c))
		total += n
		for _, pr := range problems {
			t.Errorf("%s: %s", name, pr)
		}
	}
	// Page alone holds dozens of fields; a scan that finds a handful means
	// the scan, not the markup, broke.
	if total < 20 {
		t.Fatalf("scanned only %d text fields; the scan is not seeing the page", total)
	}
}

func TestTextAssistScanCatchesAMissingAttribute(t *testing.T) {
	problems, n := textAssistProblems(`<input type="text" id="a" autocapitalize="off" autocorrect="off" spellcheck="false">` +
		`<input type="checkbox" id="b">` +
		`<textarea id="c"></textarea>` +
		`<input id="agent-input" autocapitalize="off" autocorrect="off" spellcheck="false">`)
	if n != 3 {
		t.Fatalf("fields = %d, want 3 (the checkbox is not text-like)", n)
	}
	if len(problems) != 4 {
		t.Fatalf("problems = %v, want three for #c and one for #agent-input's spellcheck", problems)
	}
}
