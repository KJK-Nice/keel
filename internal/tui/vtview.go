package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/hinshun/vt10x"
)

var ansi16 = []string{
	"#1b2230", "#ff6b7a", "#5ee0b5", "#f5c15d",
	"#6ea8ff", "#c084fc", "#67e8f9", "#e8edf7",
	"#6b7385", "#ff6b7a", "#5ee0b5", "#f5c15d",
	"#6ea8ff", "#c084fc", "#67e8f9", "#ffffff",
}

func renderVT(term vt10x.Terminal, viewCols, viewRows int) string {
	if term == nil || viewCols < 2 || viewRows < 1 {
		return ""
	}
	term.Lock()
	defer term.Unlock()
	cols, rows := term.Size()
	if viewCols < cols {
		cols = viewCols
	}
	if viewRows < rows {
		rows = viewRows
	}
	cur := term.Cursor()
	vis := term.CursorVisible()
	var b strings.Builder
	for y := 0; y < rows; y++ {
		if y > 0 {
			b.WriteByte('\n')
		}
		var run strings.Builder
		var runKey string
		flush := func() {
			if run.Len() == 0 {
				return
			}
			style := styleFromKey(runKey)
			b.WriteString(style.Render(run.String()))
			run.Reset()
		}
		for x := 0; x < cols; x++ {
			g := term.Cell(x, y)
			ch := g.Char
			if ch == 0 {
				ch = ' '
			}
			key := glyphKey(g, vis && cur.X == x && cur.Y == y)
			if key != runKey {
				flush()
				runKey = key
			}
			run.WriteRune(ch)
		}
		flush()
	}
	return b.String()
}

func glyphKey(g vt10x.Glyph, cursor bool) string {
	fg := colorToken(g.FG, false)
	bg := colorToken(g.BG, true)
	if cursor {
		return "rev|" + fg + "|" + bg
	}
	return fg + "|" + bg
}

func styleFromKey(key string) lipgloss.Style {
	st := lipgloss.NewStyle()
	parts := strings.Split(key, "|")
	if len(parts) == 3 && parts[0] == "rev" {
		st = st.Reverse(true)
		parts = parts[1:]
	}
	if len(parts) >= 1 && parts[0] != "" {
		st = st.Foreground(lipgloss.Color(parts[0]))
	}
	if len(parts) >= 2 && parts[1] != "" {
		st = st.Background(lipgloss.Color(parts[1]))
	}
	return st
}

func colorToken(c vt10x.Color, bg bool) string {
	if c == vt10x.DefaultFG || c == vt10x.DefaultCursor {
		if bg {
			return ""
		}
		return "#d7deea"
	}
	if c == vt10x.DefaultBG {
		return ""
	}
	if c < 16 {
		return ansi16[c]
	}
	if c < 256 {
		return strconv.Itoa(int(c))
	}
	return ""
}
