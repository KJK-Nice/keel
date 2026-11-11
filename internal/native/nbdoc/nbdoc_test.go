package nbdoc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/richtext"
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

// Step 2: IDs through edits, on a document with no window. Rune offsets in edited: "Intro." is 0-6, "line one" 7-15,
// "line two" 16-24, "End." starts at 25.
const edited = "Intro.\n\n```sh\nline one\nline two\n```\n\nEnd."

// edit applies fn, then checks the Markdown after the edit, after Undo (which must be the start) and after Redo.
func edit(t *testing.T, md string, fn func(e *richtext.Edit), wantMD string) (*richtext.Document, map[string]string) {
	t.Helper()
	text, langs := Open(md)
	d := richtext.NewDocument(text)
	d.Edit(fn)
	if got := Markdown(d.Text(), langs); got != wantMD {
		t.Errorf("after the edit: %q, want %q", got, wantMD)
	}
	d.Undo()
	if got := Markdown(d.Text(), langs); got != md {
		t.Errorf("after undo: %q, want %q", got, md)
	}
	d.Redo()
	if got := Markdown(d.Text(), langs); got != wantMD {
		t.Errorf("after redo: %q, want %q", got, wantMD)
	}
	return d, langs
}

func replaceAt(from, to int, s string) func(e *richtext.Edit) {
	return func(e *richtext.Edit) { e.ReplaceString(from, to, s, richtext.Attributes{}) }
}

func TestTypingInsideAFenceKeepsItsLanguage(t *testing.T) {
	d, langs := edit(t, edited, replaceAt(10, 10, "X"), "Intro.\n\n```sh\nlinXe one\nline two\n```\n\nEnd.")
	if f := Fences(d.Text(), langs); len(f) != 1 || f[0].ID != "f1" || f[0].Lang != "sh" {
		t.Errorf("fences %+v", f)
	}
}

func TestSplittingAFencesFirstLine(t *testing.T) {
	// At the end and in the middle the first half keeps the ID; at the start the second half does, and the empty
	// first half has none. Fences finds the language from any paragraph of the block, so all three keep "sh".
	for _, c := range []struct {
		name   string
		at     int
		wantID string
		wantMD string
	}{
		{"end", 15, "f1", "Intro.\n\n```sh\nline one\n\nline two\n```\n\nEnd."},
		{"middle", 11, "f1", "Intro.\n\n```sh\nline\n one\nline two\n```\n\nEnd."},
		{"start", 7, "", "Intro.\n\n```sh\n\nline one\nline two\n```\n\nEnd."},
	} {
		d, langs := edit(t, edited, replaceAt(c.at, c.at, "\n"), c.wantMD)
		f := Fences(d.Text(), langs)
		if len(f) != 1 || f[0].ID != c.wantID || f[0].Lang != "sh" {
			t.Errorf("%s: fences %+v, want ID %q and lang sh", c.name, f, c.wantID)
		}
	}
}

func TestDeletingAFencesFirstLineLosesItsLanguage(t *testing.T) {
	d, langs := edit(t, edited, replaceAt(7, 16, ""), "Intro.\n\n```\nline two\n```\n\nEnd.")
	if f := Fences(d.Text(), langs); len(f) != 1 || f[0].ID != "" || f[0].Lang != "" {
		t.Errorf("fences %+v", f)
	}
}

func TestEveryParagraphOfAFenceHavingAnIDKeepsTheLanguageWhenTheFirstLineGoes(t *testing.T) {
	// The smallest fix for the case above: name every code paragraph, not only the first, and let each stand for the
	// fence's info string. Done here with richtext's own calls, as a proof; Open does not do it.
	text, langs := Open(edited)
	n := 0
	for start, p := range text.Paragraphs() {
		if p.Style.CodeBlock && p.Style.ID == "" {
			n++
			id := "x" + string(rune('0'+n))
			langs[id] = langs["f1"]
			text = text.FormatParagraphs(start, start, func(s *richtext.ParagraphStyle) { s.ID = id })
		}
	}
	d := richtext.NewDocument(text)
	d.Edit(replaceAt(7, 16, ""))
	if got, want := Markdown(d.Text(), langs), "Intro.\n\n```sh\nline two\n```\n\nEnd."; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCutAndPasteLosesTheLanguage(t *testing.T) {
	// The clipboard leaves IDs out (ParagraphStyle.ID). Stripped here as it strips them: the pasted block is a
	// code block with no language, and its lines stay.
	text, langs := Open(edited)
	d := richtext.NewDocument(text)
	cut := text.Slice(7, 25)
	d.Edit(replaceAt(7, 25, ""))
	if got := Markdown(d.Text(), langs); got != "Intro.\n\nEnd." {
		t.Fatalf("after the cut: %q", got)
	}
	clip := cut.FormatParagraphs(0, cut.Len(), func(p *richtext.ParagraphStyle) { p.ID = "" })
	n := d.Text().Len() // not inside Edit: Document.Text locks
	d.Edit(func(e *richtext.Edit) { e.Replace(n, n, richtext.NewText("\n")) })
	end := d.Text().Len()
	d.Edit(func(e *richtext.Edit) { e.Replace(end, end, clip) })
	got := Markdown(d.Text(), langs)
	t.Logf("pasted: %q", got)
	// the last "\n\n" is the empty paragraph the clip's final newline leaves
	if got != "Intro.\n\nEnd.\n\n```\nline one\nline two\n```\n\n" {
		t.Errorf("pasted: %q", got)
	}
	d.Undo()
	d.Undo()
	if got := Markdown(d.Text(), langs); got != "Intro.\n\nEnd." {
		t.Errorf("after undoing the paste: %q", got)
	}
}

func TestJoiningTwoFencesKeepsTheFirstLanguage(t *testing.T) {
	md := "```sh\na\n```\n\nmid\n\n```go\nb\n```"
	d, langs := edit(t, md, replaceAt(2, 6, ""), "```sh\na\nb\n```")
	if f := Fences(d.Text(), langs); len(f) != 1 || f[0].Lang != "sh" || f[0].Code != "a\nb" {
		t.Errorf("fences %+v", f)
	}
}
