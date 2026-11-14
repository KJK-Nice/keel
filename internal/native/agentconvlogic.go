package native

import (
	"fmt"
	"strings"
	"time"

	"keel/internal/native/keelclient"
)

// The pure rules of saved conversations (docs/ui-redesign.md §5.4.1, §5.4.2): the groups of the list, its labels, the
// Agent page's host chips and context column. They are cmd/keel/web/js/lib/agent.js (pinned there by test/agent.test.mjs
// and here by agentconvlogic_test.go). Times are the local ones, as the app shows them.

// dayDiff is how many calendar days a is after b, by local date.
func dayDiff(a, b time.Time) int {
	a, b = a.Local(), b.Local()
	ay := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, time.UTC)
	by := time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, time.UTC)
	return int(ay.Sub(by).Hours() / 24)
}

func hhmm(t time.Time) string { return clockHM(t) }

func monthDay(t time.Time) string { return clockMD(t) }

// dayGroup puts a time in "Today", "Yesterday" or "Older" by local date. A time that is not set is "Older".
func dayGroup(when, now time.Time) string {
	if when.IsZero() {
		return "Older"
	}
	switch d := dayDiff(now, when); {
	case d <= 0:
		return "Today"
	case d == 1:
		return "Yesterday"
	}
	return "Older"
}

// sessionWhen is the list's time: 16:02 today and yesterday, Oct 14 before.
func sessionWhen(when, now time.Time) string {
	switch {
	case when.IsZero():
		return ""
	case dayGroup(when, now) == "Older":
		return monthDay(when)
	}
	return hhmm(when)
}

// stampLabel is a message's time in a transcript: 16:02 today, Oct 14, 16:02 before.
func stampLabel(when, now time.Time) string {
	switch {
	case when.IsZero():
		return ""
	case dayGroup(when, now) == "Today":
		return hhmm(when)
	}
	return monthDay(when) + ", " + hhmm(when)
}

// agoLabel says how long ago a conversation was last used (the Resumed divider).
func agoLabel(when, now time.Time) string {
	if when.IsZero() {
		return "earlier"
	}
	mins := int(now.Sub(when).Minutes())
	switch {
	case mins < 1:
		return "just now"
	case mins < 60:
		return countWord(mins, "minute", "minutes") + " ago"
	case dayGroup(when, now) == "Today":
		h := mins / 60
		return countWord(h, "hour", "hours") + " ago"
	}
	days := dayDiff(now, when)
	if days <= 1 {
		return "yesterday"
	}
	return fmt.Sprintf("%d days ago", days)
}

func proposalsLabel(n int) string {
	if n <= 0 {
		return "no proposals"
	}
	return countWord(n, "proposal", "proposals")
}

// sessionGroup is a heading of the list and its rows.
type sessionGroup struct {
	label string
	rows  []keelclient.ConversationSummary
}

// sessionGroups filters the saved conversations by the search text (title or host, any case) and groups them Today /
// Yesterday / Older, keeping keel serve's order (most recent first). Empty groups are left out. host, when set, keeps only
// conversations that read or proposed for that host (the Agent page's host filter).
func sessionGroups(sessions []keelclient.ConversationSummary, query string, now time.Time, host string) []sessionGroup {
	q := strings.ToLower(strings.TrimSpace(query))
	match := func(s keelclient.ConversationSummary) bool {
		if host != "" && !contains(s.Hosts, host) {
			return false
		}
		if q == "" || strings.Contains(strings.ToLower(s.Title), q) {
			return true
		}
		for _, h := range s.Hosts {
			if strings.Contains(strings.ToLower(h), q) {
				return true
			}
		}
		return false
	}
	groups := []sessionGroup{{label: "Today"}, {label: "Yesterday"}, {label: "Older"}}
	for _, s := range sessions {
		if s.ID == "" || !match(s) {
			continue
		}
		for i := range groups {
			if groups[i].label == dayGroup(s.UpdatedAt, now) {
				groups[i].rows = append(groups[i].rows, s)
			}
		}
	}
	var out []sessionGroup
	for _, g := range groups {
		if len(g.rows) > 0 {
			out = append(out, g)
		}
	}
	return out
}

// savedLabel is the sub row's note on where the conversation lives. A save error wins: the panel must not say "Saved"
// when it is not.
func savedLabel(historyDays int, saveError string) string {
	switch {
	case historyDays <= 0:
		return "Not saved · kept while open"
	case saveError != "":
		return "Not saved"
	}
	return savedInVault
}

// retentionNote is the session list's footnote.
func retentionNote(historyDays int) string {
	switch {
	case historyDays <= 0:
		return "History is off"
	case historyDays == 365:
		return "Kept 1 year"
	}
	return "Kept " + countWord(historyDays, "day", "days")
}

// hostFilters are the Agent page's host chips: the hosts the saved conversations read or proposed for, most recent
// conversation first, at most max of them.
func hostFilters(sessions []keelclient.ConversationSummary, max int) []string {
	var out []string
	for _, s := range sessions {
		for _, h := range s.Hosts {
			if len(out) >= max {
				return out
			}
			if h != "" && !contains(out, h) {
				out = append(out, h)
			}
		}
	}
	return out
}

// convTurn is a turn as the context column reads it: what was read and what was proposed.
type convTurn struct {
	trace     []keelclient.TraceStep
	proposals []keelclient.Proposal
	again     bool
}

// conversationHosts are the hosts one conversation read or proposed for, in first seen order.
func conversationHosts(turns []convTurn) []string {
	var out []string
	add := func(h string) {
		if h != "" && !contains(out, h) {
			out = append(out, h)
		}
	}
	for _, t := range turns {
		for _, st := range t.trace {
			if st.Status == "read" {
				add(st.Host)
			}
		}
		for _, p := range t.proposals {
			add(p.Host)
		}
	}
	return out
}

// readRow is one host of the page's "Read so far".
type readRow struct {
	host                    string
	lines, redacted         int
	linesLabel, redactLabel string
}

// readSummary adds up what a conversation read, per host: lines and how many values were redacted. Skipped panes are
// left out.
func readSummary(turns []convTurn) []readRow {
	var out []readRow
	for _, t := range turns {
		for _, st := range t.trace {
			if st.Status != "read" {
				continue
			}
			host := orElse(st.Host, "a pane")
			i := -1
			for j := range out {
				if out[j].host == host {
					i = j
				}
			}
			if i < 0 {
				out, i = append(out, readRow{host: host}), len(out)
			}
			out[i].lines += st.Lines
			for _, n := range st.Redactions {
				out[i].redacted += n
			}
		}
	}
	for i := range out {
		out[i].linesLabel = countWord(out[i].lines, "line", "lines")
		out[i].redactLabel = fmt.Sprintf("%d redacted", out[i].redacted)
	}
	return out
}

// proposalState is a proposal's badge in the page's Proposals list. typed is true when it was typed in this window.
func proposalState(p keelclient.Proposal, typed bool) (label, kind string) {
	switch {
	case typed || p.TypedAt != nil:
		if p.Edited {
			return "Edited, typed", "accent"
		}
		return "Typed", "accent"
	case p.Expired:
		return "Expired", ""
	}
	return "Not typed", "outline"
}

// startedLabel is the page header's "Today 09:14 · 2 questions".
func startedLabel(when time.Time, questions int, now time.Time) string {
	count := countWord(questions, "question", "questions")
	if when.IsZero() {
		return count
	}
	day := dayGroup(when, now)
	if day == "Older" {
		day = monthDay(when)
	}
	return day + " " + hhmm(when) + " · " + count
}

// sessionsEmptyText says why a list shows no rows, or "" when it has some: the panel and the page say the same.
func sessionsEmptyText(c convListState, groups int) string {
	switch {
	case c.err != "":
		return c.err
	case c.unreadable:
		return "Saved conversations cannot be opened: keel.chats cannot be read. New ones are not saved until Delete all starts a fresh file."
	case groups > 0:
		return ""
	case len(c.sessions) > 0:
		return "No conversation matches."
	case c.historyDays != nil && *c.historyDays == 0:
		return "History is off: conversations are kept only while the panel is open. Turn it on in Settings · Agent."
	}
	return "No saved conversations."
}

// convListState is the last session list as the panel and the page share it.
type convListState struct {
	sessions    []keelclient.ConversationSummary
	historyDays *int // nil until read
	saveError   string
	unreadable  bool
	err         string // why the last list could not be read
}
