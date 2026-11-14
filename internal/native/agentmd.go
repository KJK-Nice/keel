package native

import (
	"regexp"
	"strings"
)

// The answer's text, as much markdown as a first panel needs: paragraphs, fenced code
// blocks, `inline code` and **bold**. The model's text is shown as text: nothing here fetches an image or opens a link,
// so a link, a heading, a list or an image is shown as the characters the model wrote (a known gap, docs/native.md).

type mdBlock struct {
	code bool
	text string
}

// mdBlocks splits an answer into paragraphs (separated by a blank line) and fenced code blocks (``` … ```). An unclosed
// fence runs to the end.
func mdBlocks(s string) (out []mdBlock) {
	var para []string
	flush := func() {
		if t := strings.TrimSpace(strings.Join(para, "\n")); t != "" {
			out = append(out, mdBlock{text: t})
		}
		para = nil
	}
	var code []string
	inCode := false
	for _, line := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		switch {
		case strings.HasPrefix(strings.TrimSpace(line), "```"):
			if inCode {
				out = append(out, mdBlock{code: true, text: strings.Join(code, "\n")})
				code, inCode = nil, false
			} else {
				flush()
				inCode = true
			}
		case inCode:
			code = append(code, line)
		case strings.TrimSpace(line) == "":
			flush()
		default:
			para = append(para, line)
		}
	}
	if inCode {
		out = append(out, mdBlock{code: true, text: strings.Join(code, "\n")})
	}
	flush()
	return out
}

type mdSpan struct {
	text       string
	code, bold bool
}

var (
	codeSpan = regexp.MustCompile("(`[^`\n]+`)")
	boldSpan = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
)

// mdInline sets `code` spans in mono and **bold** in bold; bold is not applied inside code.
func mdInline(s string) (out []mdSpan) {
	rest := s
	for rest != "" {
		loc := codeSpan.FindStringIndex(rest)
		plain := rest
		if loc != nil {
			plain = rest[:loc[0]]
		}
		out = append(out, boldSpans(plain)...)
		if loc == nil {
			break
		}
		out = append(out, mdSpan{text: rest[loc[0]+1 : loc[1]-1], code: true})
		rest = rest[loc[1]:]
	}
	return out
}

func boldSpans(s string) (out []mdSpan) {
	for {
		loc := boldSpan.FindStringSubmatchIndex(s)
		if loc == nil {
			break
		}
		if loc[0] > 0 {
			out = append(out, mdSpan{text: s[:loc[0]]})
		}
		out = append(out, mdSpan{text: s[loc[2]:loc[3]], bold: true})
		s = s[loc[1]:]
	}
	if s != "" {
		out = append(out, mdSpan{text: s})
	}
	return out
}
