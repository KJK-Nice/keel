package native

import (
	"regexp"
	"strconv"
	"strings"

	"keel/internal/cmdrule"
	"keel/internal/native/keelclient"
)

// Type without the approve dialog (docs/ui-redesign.md §6 rule 13): pure helpers, no window and no session.
// They are pinned by typedirect_test.go.
//
// keel serve decides whether a Type goes without the dialog (internal/httpapi/typedirect.go, agent.DirectSafe). These
// only skip the round trip when the answer is certainly "dialog", and name the button and the badge. A hint that says
// direct can still meet the dialog; one that says no never reaches keel serve. Nothing here types.

// The grammar of a direct type is cmdrule.DirectSafe, the rule keel serve applies; the window calls it as a hint (is the
// dialog certain?) so the two cannot differ.

// snippetLine is what a direct type of a snippet sends: its command without the newlines that would run it.
func snippetLine(command string) string { return strings.TrimRight(command, crlf) }

// snippetBadge is "Approve first" only on a snippet that will certainly open the dialog: one outside the grammar.
func snippetBadge(command string) string {
	if cmdrule.DirectSafe(snippetLine(command)) {
		return ""
	}
	return "Approve first"
}

// proposalDirect: an agent proposal that keel serve should type without the dialog, as far as the window knows.
func proposalDirect(p keelclient.Proposal) bool {
	return p.Risk == keelclient.RiskReadOnly && !p.NeedsShift && !p.Placeholders && !strings.Contains(p.Command, "{{") && cmdrule.DirectSafe(p.Command)
}

// typeWord adds the ellipsis when a dialog will follow the press.
func typeWord(label string, direct bool) string {
	if direct {
		return label
	}
	return label + "…"
}

// typedNote is the line shown after a direct type.
func typedNote(target string) string {
	if target == "" {
		target = "the pane"
	}
	return "Typed into " + target + ". Press Enter to run."
}

// ---- what the pane's terminal shows ----

// The terminal plugin has no call for the alternate screen or the cursor's line. Its Snapshot is the screen as escape
// sequences: the modes that are on (ESC [ ? 1049 h and the like), then ESC [ H, the scrollback and the screen row by
// row joined with CR LF, then ESC [ row ; col H for the cursor and ESC [ 0 m. screenOf reads those two facts from it.

var (
	snapModes  = regexp.MustCompile("\x1b\\[\\?([0-9;]+)h")
	snapCursor = regexp.MustCompile("\x1b\\[([0-9]+);([0-9]+)H(?:\x1b\\[0m)?$")
	snapEscape = regexp.MustCompile("\x1b\\[[0-9;:?]*[A-Za-z]|\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)|\x1b[^\\[\\]]")
)

// crlf is CR and LF. A snapshot puts them between rows, and snippetLine cuts them off a snippet. Nothing here ever adds
// one to what is typed (guard_test.go).
var crlf = string([]byte{13, 10})

// promptLineMax is the longest line sent.
const promptLineMax = 512

// screenOf reads the pane's alternate screen and cursor line from a terminal Snapshot, with rows the terminal's height.
// It never guesses "normal": a snapshot it cannot read (empty, no home sequence) is the alternate screen, which sends
// the Type to the dialog. The line is the one the cursor is on, or when the snapshot does not place it, the last line
// with text; keel serve checks its own view of the output as well and only ever adds a dialog.
func screenOf(snap string, rows int) (altScreen bool, promptLine string) {
	alt, lines, top, row := readSnapshot(snap, rows)
	if alt {
		return true, ""
	}
	line := ""
	if row > 0 && top+row-1 < len(lines) {
		line = lines[top+row-1]
	}
	if line == "" { // blank or unplaced: the last line with text is where a prompt would be
		for i := len(lines) - 1; i >= 0 && line == ""; i-- {
			line = lines[i]
		}
	}
	if r := []rune(line); len(r) > promptLineMax {
		line = string(r[:promptLineMax])
	}
	return false, line
}

// readSnapshot is the reading under screenOf: whether the alternate screen shows, the text of every row (escapes and
// trailing spaces off), the index in lines of the screen's first row, and the cursor's row and column on the screen
// (1-based; 0 when the snapshot does not place it).
func readSnapshot(snap string, rows int) (alt bool, lines []string, top, row int) {
	home := strings.Index(snap, "\x1b[H")
	if home < 0 {
		return true, nil, 0, 0
	}
	for _, m := range snapModes.FindAllStringSubmatch(snap[:home], -1) {
		for _, n := range strings.Split(m[1], ";") {
			if n == "47" || n == "1047" || n == "1049" {
				return true, nil, 0, 0
			}
		}
	}
	body := snap[home+len("\x1b[H"):]
	if m := snapCursor.FindStringSubmatchIndex(body); m != nil {
		row, _ = strconv.Atoi(body[m[2]:m[3]])
		body = body[:m[0]]
	}
	for _, l := range strings.Split(body, crlf) {
		lines = append(lines, strings.TrimRight(snapEscape.ReplaceAllString(l, ""), " "))
	}
	return false, lines, max(0, len(lines)-rows), row
}
