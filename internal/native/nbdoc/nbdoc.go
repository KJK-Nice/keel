// Package nbdoc holds a Keel notebook (one Markdown document) in MyGo's rich text editor without losing the language
// of its code fences. richtext's code block has no language, so the info string of each fence is kept apart, keyed by
// the ID of the fence's first paragraph (ParagraphStyle.ID), which stays with the paragraph through edits and undo.
// This is a spike; docs/native-notebook-spike.md has what it found.
package nbdoc

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui/markdown/parser"
	"github.com/egoist/mygo/ui/richtext"
)

// Open parses a notebook's Markdown for the editor. langs maps a paragraph ID to its fence's info string
// (the text after the opening ```), "" for none. Every first paragraph of a code block gets an ID.
// shortcut: when the parser's code blocks and the editor's code paragraphs do not add up (they have, for every corpus
// file), the IDs go by runs of code paragraphs and no fence gets a language.
func Open(md string) (richtext.Text, map[string]string) {
	text := richtext.ParseMarkdown(md)
	var infos []string
	var sizes []int
	d := parser.New(parser.GFM | parser.ExtFootnotes)
	d.Set(md)
	var walk func(i int)
	walk = func(i int) {
		for k := range d.Children(i) {
			if b := d.Block(k); b.Kind == parser.CodeBlock {
				infos = append(infos, b.Info())
				sizes = append(sizes, max(1, d.LineCount(k)))
			} else {
				walk(k)
			}
		}
	}
	walk(0)

	var code, index []int // the first rune and the index of each code paragraph (Paragraphs yields the rune, not an index)
	n := 0
	for start, p := range text.Paragraphs() {
		if p.Style.CodeBlock {
			code, index = append(code, start), append(index, n)
		}
		n++
	}
	total := 0
	for _, n := range sizes {
		total += n
	}
	langs := map[string]string{}
	var firsts []int // first rune of each block's first paragraph
	var info []string
	if total == len(code) {
		at := 0
		for b, n := range sizes {
			firsts = append(firsts, code[at])
			info = append(info, infos[b])
			at += n
		}
	} else {
		prev := -2
		for k, start := range code {
			if index[k] != prev+1 {
				firsts = append(firsts, start)
				info = append(info, "")
			}
			prev = index[k]
		}
	}
	for n, start := range firsts {
		id := fmt.Sprintf("f%d", n+1)
		text = text.FormatParagraphs(start, start, func(p *richtext.ParagraphStyle) { p.ID = id })
		langs[id] = info[n]
	}
	return text, langs
}

// Fence is one code block as the editor holds it.
type Fence struct {
	ID        string // first paragraph's ID
	Lang      string // first word of the info string, lower case
	Code      string // the block's lines joined with "\n"
	Paragraph int    // index of its first paragraph
}

// block is a run of code paragraphs of one kind of quote, with the info string of the first of them that has an ID.
type block struct {
	Fence
	info string
}

func blocks(text richtext.Text, langs map[string]string) []block {
	var out []block
	var lines []string
	flush := func() {
		if len(out) > 0 && lines != nil {
			out[len(out)-1].Code = strings.Join(lines, "\n")
		}
		lines = nil
	}
	prev := -2
	prevQuote := false
	found := false
	i := -1
	for _, p := range text.Paragraphs() {
		i++
		if !p.Style.CodeBlock {
			continue
		}
		if i != prev+1 || p.Style.Quote != prevQuote {
			flush()
			out = append(out, block{Fence: Fence{ID: p.Style.ID, Paragraph: i}})
			lines = []string{}
			found = false
		}
		prev, prevQuote = i, p.Style.Quote
		b := &out[len(out)-1]
		if !found && p.Style.ID != "" {
			if info, ok := langs[p.Style.ID]; ok {
				b.info, found = info, true
				b.Lang = strings.ToLower(firstWord(info))
			}
		}
		var line strings.Builder
		for _, r := range p.Runs {
			line.WriteString(r.Text)
		}
		lines = append(lines, line.String())
	}
	flush()
	return out
}

func firstWord(s string) string {
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i]
	}
	return s
}

// Fences lists the code blocks in order. The language comes from the first paragraph of the block that has an ID
// langs knows, not only the first paragraph.
func Fences(text richtext.Text, langs map[string]string) []Fence {
	var out []Fence
	for _, b := range blocks(text, langs) {
		out = append(out, b.Fence)
	}
	return out
}

// Markdown writes the editor's text back, each fence with the language langs holds for its first paragraph's ID.
// shortcut: an info string holding a backtick is written after a backtick fence as is, which CommonMark does not read
// as a fence; Open never makes one from a ``` fence, only from ~~~.
func Markdown(text richtext.Text, langs map[string]string) string {
	bs := blocks(text, langs)
	lines := strings.Split(text.Markdown(), "\n")
	n := 0
	fence := "" // the closing fence while inside a block, with its quote prefix
	for i, l := range lines {
		if fence != "" {
			if l == fence {
				fence = ""
			}
			continue
		}
		rest := strings.TrimPrefix(l, "> ")
		if len(rest) >= 3 && strings.Trim(rest, "`") == "" {
			if n < len(bs) && bs[n].info != "" {
				lines[i] = l + bs[n].info
			}
			n++
			fence = l
		}
	}
	return strings.Join(lines, "\n")
}
