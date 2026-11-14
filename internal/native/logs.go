package native

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The Logs page (docs/ui-redesign.md §5.13): what Keel did on this machine, newest first. The words and rules are
// LogsPage and LogList in internal/ui, and logGroup, logFacts, logTarget, logBadgeClass and logTimeLabel in
// internal/ui/data.go. Entries carry counts, never content: nothing here could show a secret.
//
// The webview's chips are All, Sessions, Forwards, Vault and Agent (pages.templ LogsPage). §5.13 also lists
// Sharing; the webview has no such chip, so there is none here either.

const logsLimit = 200 // GET /api/logs' own default, asked for by name

// logsPage is the state of the Logs tab.
type logsPage struct {
	filter  string // the search box: kind and message
	kind    string // the chip: all, sessions, forwards, vault or agent
	entries []keelclient.LogEntry
	loaded  bool
	reading bool // a logsTick read is out
	err     string
	filterH ui.Handle
}

func (p *logsPage) load(w *workspace)                { w.loadLogs() }
func (p *logsPage) view(w *workspace, c *ui.Context) { w.logsView(c, p) }

func (p *logsPage) String() string {
	return fmt.Sprintf("logsPage{%d entries kind=%s}", len(p.entries), p.kind)
}

func (p *logsPage) GoString() string { return p.String() }

// logChips are the filter chips, in the webview's order.
var logChips = [][2]string{{"all", "All"}, {"sessions", "Sessions"}, {"forwards", "Forwards"}, {"vault", "Vault"}, {"agent", "Agent"}}

// logGroup is the chip a log kind belongs to (data.go logGroup).
func logGroup(kind string) string {
	k := strings.ToLower(kind)
	if k == "agent" || strings.HasPrefix(k, "agent.") {
		return "agent"
	}
	switch k {
	case "session":
		return "sessions"
	case "tunnel":
		return "forwards"
	case "vault", "host", "group", "identity", "snippet", "notebook":
		return "vault"
	}
	return "other"
}

// logRows are the entries the search box and the chip keep, in the server's order (newest first). The search
// matches the kind and the message, any case (data.go filterLogs).
func (p *logsPage) rows() []keelclient.LogEntry {
	q := strings.ToLower(strings.TrimSpace(p.filter))
	var out []keelclient.LogEntry
	for _, e := range p.entries {
		if p.kind != "all" && p.kind != "" && logGroup(e.Kind) != p.kind {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(e.Kind+" "+e.Message), q) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func logTimeLabel(e keelclient.LogEntry) string {
	if e.Time.IsZero() {
		return ""
	}
	return clockHMS(e.Time)
}

// logBadgeFailed marks failures with flare; every other kind stays neutral (data.go logBadgeClass).
func logBadgeFailed(kind string) bool {
	switch strings.ToLower(kind) {
	case "error", "fail", "denied":
		return true
	}
	return false
}

// logTarget names what an entry is about: its host, or a dash. The webview also names a forward; the native
// client reads no forwards yet (docs/native.md).
func logTarget(hosts []keelclient.Host, e keelclient.LogEntry) string {
	if e.HostID != "" {
		if h, ok := hostByID(hosts, e.HostID); ok {
			return h.Name
		}
	}
	return "—"
}


// logFacts lists the counts an agent entry carries, as short labels in this order (data.go logFacts).
func logFacts(e keelclient.LogEntry) []string {
	var out []string
	if e.Count > 1 {
		out = append(out, fmt.Sprintf("%d sent", e.Count))
	} else if e.Lines > 0 {
		out = append(out, countWord(e.Lines, "line", "lines"))
	}
	if e.Bytes > 0 {
		out = append(out, countWord(e.Bytes, "byte", "bytes"))
	}
	n := 0
	for _, v := range e.Redactions {
		n += v
	}
	if n > 0 {
		out = append(out, fmt.Sprintf("%d redacted", n))
	}
	if e.Source != "" {
		out = append(out, "via "+e.Source)
	}
	if e.ModelMs > 0 {
		out = append(out, fmt.Sprintf("model %d ms", e.ModelMs))
	}
	if e.JevMs > 0 {
		out = append(out, fmt.Sprintf("Jev %d ms", e.JevMs))
	}
	return out
}

// logFactsTitle spells out the redactions by kind for a tooltip.
func logFactsTitle(e keelclient.LogEntry) string {
	if len(e.Redactions) == 0 {
		return ""
	}
	kinds := make([]string, 0, len(e.Redactions))
	for k := range e.Redactions {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	parts := make([]string, len(kinds))
	for i, k := range kinds {
		parts[i] = fmt.Sprintf("%s %d", k, e.Redactions[k])
	}
	return "Redacted: " + strings.Join(parts, ", ")
}

// logsEvery is how often the Logs page reads GET /api/logs while it is the selected tab. The webview's Datastar stream
// pushes server-rendered HTML (LogList), which this client cannot consume as data; the endpoint has no since
// parameter, so a read is the newest logsLimit entries. 2 s is the forwards page's rate (forwardsEvery).
const logsEvery = 2 * time.Second

// logsTick is one tick of that read: it reads only while the Logs page is the selected tab and no read is out. A
// failed read is not news (the next tick tries again), and it leaves the entries and the error as they were.
func (w *workspace) logsTick() {
	if w.closed || w.gate != nil || w.first != nil {
		return
	}
	p := pageAs[*logsPage](w.tab())
	if p == nil || p.reading {
		return
	}
	w.readLogs(&p.reading, true)
}

// loadLogs reads the log again (the page opened or was selected, the vault changed, or Refresh).
func (w *workspace) loadLogs() { w.readLogs(new(bool), false) }

// readLogs is the one read of the log. busy is the in-flight flag a tick shares; quiet leaves a failed read
// unreported.
func (w *workspace) readLogs(busy *bool, quiet bool) {
	callOnce(w, busy, nil, func(c context.Context) ([]keelclient.LogEntry, error) { return w.be.Logs(c, logsLimit) }, func(entries []keelclient.LogEntry, err error) {
		t := w.pageTab("logs")
		switch {
		case t == nil:
		case err == nil:
			p := pageAs[*logsPage](t)
			p.err, p.loaded, p.entries = "", true, entries
		case !quiet:
			pageAs[*logsPage](t).err = "Could not read the log: " + hostError(err)
		}
	})
}
