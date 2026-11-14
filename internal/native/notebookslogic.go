package native

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui/markdown/parser"

	"keel/internal/cmdrule"
	"keel/internal/native/keelclient"
)

// The pure rules of notebooks (docs/ui-redesign.md §18): the words and states the screens show, pinned by
// notebookslogic_test.go. Nothing here talks to keel serve, draws or types.

const (
	placeholderRefused = "Not typed: this block holds a redacted placeholder such as [secret:1], which a shell would read as a pattern. Edit it to the real value or an environment variable first."
	agentEditReview    = "The agent edited this notebook. Check what changed before you type a command from it: Keel kept the version before the edit."
)

// byAgent is Notebook.LastBy and Notebook.Source of the agent's work (a constant: guard_test.go keeps the quoted key).
const byAgent = "agent"

// privateLabel is the meta line's toggle.
func privateLabel(nb keelclient.Notebook) string {
	if nb.Private {
		return "Private"
	}
	return "Agent can read"
}

// ---- sorting and previews ----

// sortedNotebooks is the list the sidebar and the page show: pinned first, then newest, then name.
func sortedNotebooks(nbs []keelclient.Notebook) []keelclient.Notebook {
	out := slices.Clone(nbs)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Pinned != out[j].Pinned {
			return out[i].Pinned
		}
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// notebookPreview is a row's second line: the first non-empty line of the text.
func notebookPreview(nb keelclient.Notebook) string {
	for _, cell := range nb.Cells {
		line := strings.TrimSpace(cell.Body)
		if line == "" {
			continue
		}
		if i := strings.IndexByte(line, '\n'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line != "" {
			return line
		}
	}
	return "Empty notebook"
}

// notebookName is a notebook's name as a row shows it.
func notebookName(nb keelclient.Notebook) string {
	if nb.Name == "" {
		return "Notebook"
	}
	return nb.Name
}

// ---- fences ----

// fence is a fenced block of a notebook's Markdown as keel serve will see it: parsed with MyGo's Markdown parser, never
// from a rich text model, so two fences next to each other are two fences.
type fence struct {
	Index int    // the block's index in the parsed document, which markdown.View reports as CodeBlockView.Index
	Lang  string // the first word of the info string, lower case
	Code  string // the lines joined with "\n", with no newline at the end
	Shell bool   // sh, bash or shell: only these get Type; every fence gets Copy
}

// markdownExt is the syntax markdown.View reads when Options.Extensions is zero: fences are parsed the same way, so a
// block has the same index in both.
const markdownExt = parser.Default

// shellLangs are the fence languages that offer Type.
var shellLangs = []string{"sh", "bash", "shell"}

// fences lists the code blocks of md in document order, those in a quote or a list item too.
func fences(md string) (out []fence) {
	d := parser.New(markdownExt)
	d.Set(md)
	var walk func(i int)
	walk = func(i int) {
		for k := range d.Children(i) {
			b := d.Block(k)
			if b.Kind != parser.CodeBlock {
				walk(k)
				continue
			}
			lang := strings.ToLower(b.Language())
			code := strings.TrimSuffix(d.Lines(k), "\n")
			out = append(out, fence{Index: k, Lang: lang, Code: code, Shell: slices.Contains(shellLangs, lang)})
		}
	}
	walk(0)
	return out
}

// fenceAt is the fence with this block index.
func fenceAt(fs []fence, index int) (fence, bool) {
	for _, f := range fs {
		if f.Index == index {
			return f, true
		}
	}
	return fence{}, false
}

// directable reports whether a fence may be typed with no dialog: a shell fence of exactly one line that holds no
// placeholder (keel serve decides again).
func directable(f fence) bool {
	line := strings.TrimSpace(f.Code)
	return f.Shell && line != "" && !strings.Contains(line, "\n") && !cmdrule.HasPlaceholder(line)
}

// ---- autosave ----

// autosave decides when an edited notebook saves: idle after the last edit, and at least every maxWait while edits keep
// coming, one save at a time, and an edit made during a save runs one more right after it, so an older text never lands
// after a newer one. It has no timers: the caller asks due with the frame's time and asks again at wake.
type autosave struct {
	idle, maxWait time.Duration
	dirty         bool
	running       bool
	last, first   time.Time // the last edit, and the first since the last save began
}

const (
	autosaveIdle = 1000 * time.Millisecond
	autosaveMax  = 5000 * time.Millisecond
)

func newAutosave() *autosave { return &autosave{idle: autosaveIdle, maxWait: autosaveMax} }

// edit records an edit.
func (a *autosave) edit(now time.Time) {
	a.dirty = true
	a.last = now
	if a.first.IsZero() {
		a.first = now
	}
}

// pending is true while an edit is not saved or a save is running.
func (a *autosave) pending() bool { return a.dirty || a.running }

// due reports whether a save should begin now: an edit waits, no save runs, and the edits have been quiet for the idle
// time or have gone on for the maximum.
func (a *autosave) due(now time.Time) bool {
	if !a.dirty || a.running {
		return false
	}
	return now.Sub(a.last) >= a.idle || now.Sub(a.first) >= a.maxWait
}

// wake is how long until due, or 0 when nothing waits.
func (a *autosave) wake(now time.Time) time.Duration {
	if !a.dirty || a.running {
		return 0
	}
	return max(min(a.last.Add(a.idle).Sub(now), a.first.Add(a.maxWait).Sub(now)), time.Millisecond)
}

// begin starts a save, and reports whether there was one to start (flush uses it: nothing when clean, nothing while one
// runs, which will run once more if an edit came meanwhile).
func (a *autosave) begin() bool {
	if !a.dirty || a.running {
		return false
	}
	a.dirty, a.running = false, true
	a.first = time.Time{}
	return true
}

// end finishes a save. It reports whether another must start now: an edit came during it.
func (a *autosave) end() (again bool) {
	a.running = false
	return a.dirty
}

// ---- History ----

var eventLabel = map[string]string{
	"before-agent-edit": "Before the agent's edit",
	"agent-version":     "The agent's edit",
	"session":           "Your version",
	"before-restore":    "Before a restore",
	"deleted":           "Deleted",
}

// versionRow is a History row: who, what, when, and the agent's summary under it.
type versionRow struct{ Who, What, When, Summary string }

// versionLabel is a version's who and what.
func versionLabel(v keelclient.NotebookVersion, now time.Time) versionRow {
	who := "You"
	if v.By == byAgent {
		who = "Agent"
	}
	what := eventLabel[v.Event]
	if what == "" {
		what = "Earlier version"
	}
	r := versionRow{Who: who, What: what, When: whenText(v.At, now)}
	if v.Event == "before-agent-edit" && v.Summary != "" {
		r.Summary = "Then: " + v.Summary
	}
	return r
}

// whenText is "just now", "2 min ago", "today 14:05", "Oct 19 14:05".
func whenText(at, now time.Time) string {
	if at.IsZero() {
		return ""
	}
	mins := int(now.Sub(at).Minutes() + 0.5) // rounded
	if mins < 1 {
		return "just now"
	}
	if mins < 60 {
		return fmt.Sprintf("%d min ago", mins)
	}
	at, now = at.Local(), now.Local()
	if at.YearDay() == now.YearDay() && at.Year() == now.Year() {
		return "today " + clockHM(at)
	}
	return clockMD(at) + " " + clockHM(at)
}

// versionText is a version's text.
func versionText(v keelclient.NotebookVersion) string { return v.Markdown() }

// diffLine is a line of a diff: op is "same", "add" or "del".
type diffLine struct{ Op, Text string }

// diffLines compares two texts line by line (longest common subsequence): b's lines are added, a's removed. Past maxCells
// line pairs it gives up on alignment and shows a as removed and b as added.
func diffLines(a, b string, maxCells int) []diffLine {
	x, y := strings.Split(a, "\n"), strings.Split(b, "\n")
	head := 0
	for head < len(x) && head < len(y) && x[head] == y[head] {
		head++
	}
	tail := 0
	for tail < len(x)-head && tail < len(y)-head && x[len(x)-1-tail] == y[len(y)-1-tail] {
		tail++
	}
	xs, ys := x[head:len(x)-tail], y[head:len(y)-tail]
	var out []diffLine
	for _, t := range x[:head] {
		out = append(out, diffLine{"same", t})
	}
	if len(xs)*len(ys) > maxCells {
		for _, t := range xs {
			out = append(out, diffLine{"del", t})
		}
		for _, t := range ys {
			out = append(out, diffLine{"add", t})
		}
	} else {
		n, m := len(xs), len(ys)
		L := make([][]uint32, n+1)
		for i := range L {
			L[i] = make([]uint32, m+1)
		}
		for i := n - 1; i >= 0; i-- {
			for j := m - 1; j >= 0; j-- {
				if xs[i] == ys[j] {
					L[i][j] = L[i+1][j+1] + 1
				} else {
					L[i][j] = max(L[i+1][j], L[i][j+1])
				}
			}
		}
		i, j := 0, 0
		for i < n && j < m {
			switch {
			case xs[i] == ys[j]:
				out = append(out, diffLine{"same", xs[i]})
				i++
				j++
			case L[i+1][j] >= L[i][j+1]:
				out = append(out, diffLine{"del", xs[i]})
				i++
			default:
				out = append(out, diffLine{"add", ys[j]})
				j++
			}
		}
		for ; i < n; i++ {
			out = append(out, diffLine{"del", xs[i]})
		}
		for ; j < m; j++ {
			out = append(out, diffLine{"add", ys[j]})
		}
	}
	for _, t := range x[len(x)-tail:] {
		out = append(out, diffLine{"same", t})
	}
	return out
}

const diffMaxCells = 250000

// diffSummary counts a diff's added and removed lines.
func diffSummary(d []diffLine) string {
	add, del := 0, 0
	for _, l := range d {
		switch l.Op {
		case "add":
			add++
		case "del":
			del++
		}
	}
	if add == 0 && del == 0 {
		return "No changes"
	}
	return fmt.Sprintf("%s added · %d removed", countWord(add, "line", "lines"), del)
}

// diffRow is a row of diffView: a changed or context line, or a gap of Count unchanged lines (Op "gap").
type diffRow struct {
	Op, Text string
	Count    int
}

// diffView keeps the changed lines and up to context lines around each, with a gap for what it leaves out.
func diffView(d []diffLine, context int) (out []diffRow) {
	keep := make([]bool, len(d))
	for i, l := range d {
		if l.Op != "same" {
			for k := max(0, i-context); k <= min(len(d)-1, i+context); k++ {
				keep[k] = true
			}
		}
	}
	gap := 0
	for i, l := range d {
		if keep[i] {
			if gap > 0 {
				out = append(out, diffRow{Op: "gap", Count: gap})
			}
			gap = 0
			out = append(out, diffRow{Op: l.Op, Text: l.Text})
		} else {
			gap++
		}
	}
	if gap > 0 {
		out = append(out, diffRow{Op: "gap", Count: gap})
	}
	return out
}
