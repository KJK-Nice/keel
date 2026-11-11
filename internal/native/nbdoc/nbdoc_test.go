package nbdoc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// roundTrip is Markdown(Open(md)).
func roundTrip(md string) string { return Markdown(Open(md)) }

func TestLanguageSurvivesARoundTrip(t *testing.T) {
	for _, info := range []string{"sh", "bash", "shell", "go", "", `sh {title="x"}`} {
		md := "```" + info + "\necho hi\n```"
		if got := roundTrip(md); got != md {
			t.Errorf("info %q: got %q, want %q", info, got, md)
		}
	}
}

func TestTwoFencesInARow(t *testing.T) {
	// The editor holds code paragraphs flat, so two fences with one blank line between are one block of
	// the second's lines after the first's: the first's language stays, the second's is lost, and the blank line goes.
	md := "```sh\none\n```\n\n```go\ntwo\n```"
	text, langs := Open(md)
	if n := len(Fences(text, langs)); n != 1 {
		t.Fatalf("%d blocks, want 1 (merged)", n)
	}
	got := Markdown(text, langs)
	t.Logf("merged: %q", got)
	if want := "```sh\none\ntwo\n```"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFenceInsideAQuoteAndAList(t *testing.T) {
	md := "> ```sh\n> echo quoted\n> ```"
	if got := roundTrip(md); got != md {
		t.Errorf("quote: got %q, want %q", got, md)
	}
	list := "- item\n\n  ```sh\n  echo listed\n  ```\n"
	text, langs := Open(list)
	fs := Fences(text, langs)
	t.Logf("list: fences %+v, markdown %q", fs, Markdown(text, langs))
	if len(fs) != 1 || fs[0].Lang != "sh" || fs[0].Code != "echo listed" {
		t.Errorf("fence in a list: %+v", fs)
	}
}

func TestALongerFenceWhenTheCodeHoldsBackticks(t *testing.T) {
	md := "````sh\necho ```\n````"
	text, langs := Open(md)
	if fs := Fences(text, langs); len(fs) != 1 || fs[0].Lang != "sh" || fs[0].Code != "echo ```" {
		t.Fatalf("fences %+v", fs)
	}
	if got := Markdown(text, langs); got != md {
		t.Errorf("got %q, want %q", got, md)
	}
}

// corpus lists the testdata inputs.
func corpus(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("testdata/*.md")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, f := range files {
		if !strings.HasSuffix(f, ".out.md") {
			out = append(out, f)
		}
	}
	if len(out) < 9 {
		t.Fatalf("corpus has %d files", len(out))
	}
	return out
}

func TestCorpusRoundTrip(t *testing.T) {
	for _, f := range corpus(t) {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		// The editor writes no final newline, and Keel trims a notebook on save (notebookMarkdown in term.js), so the
		// input is compared without its final newline; every other byte counts.
		md := strings.TrimRight(string(b), "\n")
		got := roundTrip(md)
		want := md
		// A file that does not come back as it went in has its actual output on record in <name>.out.md.
		if out, err := os.ReadFile(strings.TrimSuffix(f, ".md") + ".out.md"); err == nil {
			want = strings.TrimRight(string(out), "\n")
			if want == md {
				t.Errorf("%s: the .out.md equals the input; delete it", f)
			}
		}
		if got != want {
			t.Errorf("%s: round trip is\n%q\nwant\n%q", f, got, want)
		}
	}
}

func TestRoundTripIsStable(t *testing.T) {
	for _, f := range corpus(t) {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		first := roundTrip(strings.TrimRight(string(b), "\n"))
		second := roundTrip(first)
		if f == "testdata/html.md" {
			// Known: the HTML block's closing tag leaves an empty paragraph, written as extra blank lines the next
			// reading drops. The text is the same; the bytes are not.
			if want := "More\n\nHidden text.\n\nPress Ctrl+C to stop."; second != want {
				t.Errorf("%s: second round trip %q, want %q", f, second, want)
			}
			continue
		}
		if second != first {
			t.Errorf("%s: second round trip\n%q\ndiffers from the first\n%q", f, second, first)
		}
	}
}

func TestFencesListsEachBlock(t *testing.T) {
	text, langs := Open("Intro.\n\n```sh\necho one\necho two\n```\n\nBetween.\n\n```Go\nx := 1\n```\n\nMid.\n\n```\nnone\n```\n")
	got := Fences(text, langs)
	want := []Fence{
		{ID: "f1", Lang: "sh", Code: "echo one\necho two", Paragraph: 1},
		{ID: "f2", Lang: "go", Code: "x := 1", Paragraph: 4},
		{ID: "f3", Lang: "", Code: "none", Paragraph: 6},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("fence %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}
