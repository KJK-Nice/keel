package native

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"

	"keel/internal/native/nbdoc"
)

// A spike (docs/native-notebook-spike.md): richtext.Editor on a notebook from nbdoc, with a bar over each code block
// drawn beside the editor from Document.VisibleParagraphs. The view lives in this file only and types nothing anywhere;
// a click on the bar records what it would have sent.

const spikeText = "Intro.\n\n```sh\necho one\n```\n\nBetween the two.\n\n```sh\necho two\n```\n\nEnd."

type spikeHit struct{ action, id, code string }

type spike struct {
	doc  *richtext.Document
	lang map[string]string
	hits []spikeHit
	ed   ui.Rect // the editor's box in the window, as of the last frame
	bars map[string]ui.Rect
}

func newSpike(md string) *spike {
	text, langs := nbdoc.Open(md)
	return &spike{doc: richtext.NewDocument(text), lang: langs}
}

func (s *spike) view(c *ui.Context) {
	windowTheme(c, true)
	s.bars = map[string]ui.Rect{}
	ui.Box(c).Fill().Background(chrome).Children(func() {
		ed := richtext.Editor(c, s.doc, richtext.EditorOptions{NoMarkdown: false}).Fill()
		s.ed = ed.Bounds()
		boxes := s.doc.VisibleParagraphs()
		at := map[int]richtext.ParagraphBox{}
		for _, b := range boxes {
			at[b.Index] = b
		}
		for _, f := range nbdoc.Fences(s.doc.Text(), s.lang) {
			b, ok := at[f.Paragraph]
			if !ok {
				continue
			}
			s.bar(c, f, b)
		}
	})
}

// bar is the fence's bar at the top right of its first paragraph.
func (s *spike) bar(c *ui.Context, f nbdoc.Fence, b richtext.ParagraphBox) {
	key := f.ID
	if key == "" {
		key = fmt.Sprint("p", f.Paragraph)
	}
	ui.Row(c.Key(key)).Absolute().Top(b.Top).Right(space4).Height(sizeControlSm).Gap(space1).
		Background(raised).Radius(radiusSm).Border(1, line).Children(func() {
		for _, a := range []string{"Copy code", "Type into pane"} {
			btn := ui.Row(c.Key(a)).Height(sizeControlSm).Padding(0, space2).Radius(radiusSm).Justify(ui.Center).
				Label(a + " " + key).Cursor(ui.CursorPointer)
			btn.Children(func() { caption(c, a, inkMuted) })
			if btn.Clicked() {
				s.hits = append(s.hits, spikeHit{a, f.ID, f.Code})
			}
		}
	})
}

func (s *spike) tester(w, h int) *ui.Tester {
	tt := ui.NewTester(s.view, w, h)
	tt.Frame()
	return tt
}

func (s *spike) code(i int) nbdoc.Fence { return nbdoc.Fences(s.doc.Text(), s.lang)[i] }

// barRect is where a button of a fence is, by the label the tester finds.
func barRect(t *testing.T, tt *ui.Tester, action, key string) ui.Rect {
	t.Helper()
	r, ok := tt.Find(action + " " + key)
	if !ok {
		t.Fatalf("no button %q", action+" "+key)
	}
	return r
}

func (s *spike) top(t *testing.T, para int) richtext.ParagraphBox {
	t.Helper()
	for _, b := range s.doc.VisibleParagraphs() {
		if b.Index == para {
			return b
		}
	}
	t.Fatalf("paragraph %d is not in view", para)
	return richtext.ParagraphBox{}
}

func TestNotebookSpikeBar(t *testing.T) {
	s := newSpike(spikeText)
	tt := s.tester(640, 420)

	// Both bars are there, and each button reports its own fence and exact code.
	for _, c := range []struct{ key, code string }{{"f1", "echo one"}, {"f2", "echo two"}} {
		for _, a := range []string{"Copy code", "Type into pane"} {
			s.hits = nil
			if err := tt.Click(a + " " + c.key); err != nil {
				t.Fatalf("click %s %s: %v", a, c.key, err)
			}
			tt.Frame()
			if len(s.hits) != 1 || s.hits[0] != (spikeHit{a, c.key, c.code}) {
				t.Errorf("%s %s: hits %+v", a, c.key, s.hits)
			}
		}
	}

	// The bar sits on the fence's first line.
	for _, c := range []struct {
		key  string
		para int
	}{{"f1", 1}, {"f2", 3}} {
		r, b := barRect(t, tt, "Copy code", c.key), s.top(t, c.para)
		t.Logf("%s: bar y=%.1f h=%.1f, paragraph top=%.1f h=%.1f, editor at y=%.1f", c.key, r.Y, r.H, b.Top, b.Height, s.ed.Y)
		if d := r.Y - (s.ed.Y + b.Top); d < -1 || d > r.H {
			t.Errorf("%s: the bar is %.1f DIPs from the paragraph's top", c.key, d)
		}
	}

	// A line typed into the first fence: the click reports the new code. The caret goes to the end of the line by a
	// click on the code left of the bar, then End.
	b := s.top(t, 1)
	tt.ClickAt(s.ed.X+40, s.ed.Y+b.Top+b.Height/2)
	tt.Key(0, ui.KeyEnd)
	tt.Key(0, ui.KeyEnter)
	tt.Type("ls -la")
	tt.Frame()
	s.hits = nil
	if err := tt.Click("Type into pane f1"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(s.hits) != 1 || s.hits[0].code != "echo one\nls -la" {
		t.Errorf("after typing a line: %+v", s.hits)
	}
	if got := nbdoc.Markdown(s.doc.Text(), s.lang); !strings.Contains(got, "```sh\necho one\nls -la\n```") {
		t.Errorf("markdown after typing: %q", got)
	}

	// Three paragraphs inserted above: the bar follows the fence.
	s.doc.Edit(func(e *richtext.Edit) { e.ReplaceString(0, 0, "A\nB\nC\n", richtext.Attributes{}) })
	tt.Frame()
	f1 := s.code(0)
	r, b := barRect(t, tt, "Copy code", f1.ID), s.top(t, f1.Paragraph)
	t.Logf("after inserting three paragraphs: fence at paragraph %d, bar y=%.1f, paragraph top=%.1f", f1.Paragraph, r.Y, s.ed.Y+b.Top)
	if f1.Paragraph != 4 || r.Y-(s.ed.Y+b.Top) < -1 || r.Y-(s.ed.Y+b.Top) > r.H {
		t.Errorf("the bar did not follow the fence")
	}
	s.hits = nil
	if err := tt.Click("Copy code f1"); err != nil {
		t.Fatal(err)
	}
	if len(s.hits) != 1 || s.hits[0].id != "f1" || s.hits[0].code != "echo one\nls -la" {
		t.Errorf("after inserting above: %+v", s.hits)
	}
}

func TestNotebookSpikeScroll(t *testing.T) {
	var long strings.Builder
	long.WriteString("```sh\necho first\n```\n\n")
	for i := range 40 {
		fmt.Fprintf(&long, "Paragraph %d of filler text.\n\n", i)
	}
	long.WriteString("```sh\necho second\n```\n\nEnd.")
	s := newSpike(long.String())
	// The editor reveals the caret, which starts at the end of the text: the document would open scrolled to its end.
	s.doc.SetSelection(0, 0)
	tt := s.tester(640, 300)
	if _, ok := tt.Find("Copy code f1"); !ok {
		t.Fatal("the first bar is not drawn at the top")
	}
	if _, ok := tt.Find("Copy code f2"); ok {
		t.Fatal("the second bar is drawn before it is in view")
	}
	for i := 0; i < 80; i++ {
		if _, ok := tt.Find("Copy code f2"); ok {
			break
		}
		tt.Scroll(s.ed.X+100, s.ed.Y+100, 0, 60)
		tt.Frame()
	}
	if _, ok := tt.Find("Copy code f2"); !ok {
		t.Fatal("scrolling never brought the second bar into view")
	}
	if _, ok := tt.Find("Copy code f1"); ok {
		t.Error("the first fence is out of view and its bar is still drawn")
	}
	s.hits = nil
	if err := tt.Click("Type into pane f2"); err != nil {
		t.Fatal(err)
	}
	if len(s.hits) != 1 || s.hits[0] != (spikeHit{"Type into pane", "f2", "echo second"}) {
		t.Errorf("after scrolling: %+v", s.hits)
	}
}

func TestNotebookSpikeBarDoesNotTakeTheTextsClick(t *testing.T) {
	s := newSpike(spikeText)
	tt := s.tester(640, 420)
	b := s.top(t, 1)
	tt.ClickAt(s.ed.X+40, s.ed.Y+b.Top+b.Height/2)
	tt.Type("Z")
	tt.Frame()
	if got := s.code(0).Code; got == "echo one" || !strings.Contains(got, "Z") {
		t.Errorf("a click beside the bar and a typed character left the code %q", got)
	}
	if len(s.hits) != 0 {
		t.Errorf("the click reached a button: %+v", s.hits)
	}
}

func TestNotebookSpikeLongFirstLineUnderTheBar(t *testing.T) {
	long := "```sh\n" + strings.Repeat("abcdefghij", 8) + "\n```\n\nEnd."
	s := newSpike(long)
	tt := s.tester(640, 300)
	bar := barRect(t, tt, "Copy code", "f1")
	barLeft := bar.X
	// The caret at the end of the line shows how far the text runs.
	b := s.top(t, 0)
	tt.ClickAt(s.ed.X+40, s.ed.Y+b.Top+b.Height/2)
	tt.Key(0, ui.KeyEnd)
	tt.Frame()
	caret, ok := tt.TextCaret()
	if !ok {
		t.Fatal("no caret")
	}
	t.Logf("80-character first line: text ends at x=%.1f, the bar starts at x=%.1f (bar y=%.1f h=%.1f, caret y=%.1f), editor width %.1f",
		caret.X, barLeft, bar.Y, bar.H, caret.Y, s.ed.W)
	if caret.X > barLeft && caret.Y < bar.Y+bar.H && caret.Y+caret.H > bar.Y {
		t.Logf("the bar hides %.1f DIPs of the line", caret.X-barLeft)
	} else {
		t.Logf("the text does not reach the bar")
	}
}

func TestNotebookSpike256KiB(t *testing.T) {
	starter, err := os.ReadFile("nbdoc/testdata/starter.md")
	if err != nil {
		t.Fatal(err)
	}
	unit := string(starter) + "\n```sh\necho hi\n```\n\n"
	md := strings.Repeat(unit, 256*1024/len(unit)+1)[:256*1024]
	t0 := time.Now()
	s := newSpike(md)
	opened := time.Since(t0)
	t1 := time.Now()
	tt := s.tester(1280, 720)
	painted := time.Since(t1)
	t.Logf("256 KiB (%d bytes): Open %v, first paint %v, %d paragraphs", len(md), opened, painted, s.doc.Text().ParagraphCount())
	t2 := time.Now()
	tt.Scroll(s.ed.X+100, s.ed.Y+100, 0, 400)
	tt.Frame()
	t.Logf("one scroll frame %v; Markdown of the whole document %v", time.Since(t2), func() time.Duration {
		t3 := time.Now()
		nbdoc.Markdown(s.doc.Text(), s.lang)
		return time.Since(t3)
	}())
}

func TestNotebookSpikePicture(t *testing.T) {
	s := newSpike(spikeText)
	tt := s.tester(640, 420)
	img := tt.Image()
	if img == nil {
		t.Fatal("Tester.Image gave no pixels")
	}
	dir := filepath.Join("..", "..", ".scratch-native", "e2e")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, "TestNotebookSpikeBar.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
