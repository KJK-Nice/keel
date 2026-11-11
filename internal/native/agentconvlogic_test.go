package native

import (
	"reflect"
	"testing"
	"time"

	"keel/internal/native/keelclient"
)

// The cases of cmd/keel/web/js/test/agent.test.mjs for saved conversations and the Agent page, in Go.

func lt(mo time.Month, d, h, m int) time.Time { return time.Date(2026, mo, d, h, m, 0, 0, time.Local) }

func TestSavedConversationsGroupByLocalDayAndFilterByTitleOrHost(t *testing.T) {
	now := lt(time.October, 18, 10, 0)
	list := []keelclient.ConversationSummary{
		{ID: "a", Title: "Why is pg-2 lagging?", Hosts: []string{"prod-db-01"}, UpdatedAt: lt(time.October, 18, 9, 14)},
		{ID: "b", Title: "TLS cert", Hosts: []string{"ceph-rgw-1"}, UpdatedAt: lt(time.October, 17, 17, 40)},
		{ID: "c", Title: "Disk full", Hosts: []string{"gitlab-runner-3"}, UpdatedAt: lt(time.October, 14, 16, 2)},
	}
	shape := func(gs []sessionGroup) (out [][2]any) {
		for _, g := range gs {
			var ids []string
			for _, r := range g.rows {
				ids = append(ids, r.ID)
			}
			out = append(out, [2]any{g.label, ids})
		}
		return out
	}
	want := [][2]any{{"Today", []string{"a"}}, {"Yesterday", []string{"b"}}, {"Older", []string{"c"}}}
	if got := shape(sessionGroups(list, "", now, "")); !reflect.DeepEqual(got, want) {
		t.Errorf("groups %v, want %v", got, want)
	}
	if got := sessionGroups(list, "RUNNER", now, ""); len(got) != 1 || got[0].label != "Older" {
		t.Errorf("a host search: %v", got)
	}
	if got := sessionGroups(list, "pg-2", now, ""); got[0].rows[0].ID != "a" {
		t.Errorf("a title search: %v", got)
	}
	if got := sessionGroups(list, "nothing", now, ""); len(got) != 0 {
		t.Errorf("no match: %v", got)
	}
	for _, c := range []struct{ got, want string }{
		{sessionWhen(list[0].UpdatedAt, now), "09:14"},
		{sessionWhen(list[1].UpdatedAt, now), "17:40"},
		{sessionWhen(list[2].UpdatedAt, now), "Oct 14"},
		{stampLabel(list[2].UpdatedAt, now), "Oct 14, 16:02"},
		{agoLabel(list[2].UpdatedAt, now), "4 days ago"},
		{agoLabel(list[1].UpdatedAt, now), "yesterday"},
		{agoLabel(lt(time.October, 18, 9, 55), now), "5 minutes ago"},
		{agoLabel(lt(time.October, 18, 7, 0), now), "3 hours ago"},
		{agoLabel(now, now), "just now"},
		{agoLabel(time.Time{}, now), "earlier"},
		{proposalsLabel(0), "no proposals"},
		{proposalsLabel(1), "1 proposal"},
		{proposalsLabel(2), "2 proposals"},
		{savedLabel(30, ""), "Saved in the vault"},
		{savedLabel(0, ""), "Not saved · kept while open"},
		{savedLabel(30, "the vault is locked"), "Not saved"},
		{retentionNote(30), "Kept 30 days"},
		{retentionNote(1), "Kept 1 day"},
		{retentionNote(365), "Kept 1 year"},
		{retentionNote(0), "History is off"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

func TestAgentPageHostChipsHostFilterAndContextColumn(t *testing.T) {
	now := lt(time.October, 18, 10, 0)
	list := []keelclient.ConversationSummary{
		{ID: "a", Title: "pg lag", Hosts: []string{"prod-db-01", "prod-db-02"}, UpdatedAt: lt(time.October, 18, 9, 14)},
		{ID: "b", Title: "502", Hosts: []string{"prod-api-01", "prod-db-01"}, UpdatedAt: lt(time.October, 18, 8, 2)},
		{ID: "c", Title: "disk", Hosts: []string{"gitlab-runner-3"}, UpdatedAt: lt(time.October, 14, 16, 2)},
	}
	if got := hostFilters(list, 6); !reflect.DeepEqual(got, []string{"prod-db-01", "prod-db-02", "prod-api-01", "gitlab-runner-3"}) {
		t.Errorf("chips %v", got)
	}
	if got := hostFilters(list, 2); !reflect.DeepEqual(got, []string{"prod-db-01", "prod-db-02"}) {
		t.Errorf("at most 2: %v", got)
	}
	if got := hostFilters(nil, 6); len(got) != 0 {
		t.Errorf("no chips: %v", got)
	}
	if g := sessionGroups(list, "", now, "prod-db-01"); len(g) != 1 || len(g[0].rows) != 2 || g[0].rows[0].ID != "a" || g[0].rows[1].ID != "b" {
		t.Errorf("the host filter: %v", g)
	}
	if g := sessionGroups(list, "502", now, "prod-db-02"); len(g) != 0 {
		t.Errorf("a search and a host that do not meet: %v", g)
	}

	turns := []convTurn{
		{trace: []keelclient.TraceStep{{Host: "prod-db-01", Status: "read", Lines: 120}, {Host: "prod-db-02", Status: "read", Lines: 64, Redactions: map[string]int{"secret": 1}},
			{Host: "secret-box", Status: "skipped", Reason: "not-readable"}}, proposals: []keelclient.Proposal{{ID: "p1", Host: "prod-db-01", Command: "psql"}}},
		{trace: []keelclient.TraceStep{{Host: "prod-db-01", Status: "read", Lines: 1}}},
		{again: true, proposals: []keelclient.Proposal{{ID: "p2", Host: "web-1", Command: "psql"}}},
	}
	if got := conversationHosts(turns); !reflect.DeepEqual(got, []string{"prod-db-01", "prod-db-02", "web-1"}) {
		t.Errorf("hosts %v", got)
	}
	var rows [][3]string
	for _, r := range readSummary(turns) {
		rows = append(rows, [3]string{r.host, r.linesLabel, r.redactLabel})
	}
	if want := [][3]string{{"prod-db-01", "121 lines", "0 redacted"}, {"prod-db-02", "64 lines", "1 redacted"}}; !reflect.DeepEqual(rows, want) {
		t.Errorf("read so far %v, want %v", rows, want)
	}
	if len(readSummary(nil)) != 0 {
		t.Error("nothing read")
	}

	at := time.Now()
	for _, c := range []struct {
		p           keelclient.Proposal
		typed       bool
		label, kind string
	}{
		{keelclient.Proposal{Expired: true}, false, "Expired", ""},
		{keelclient.Proposal{Expired: true, TypedAt: &at}, false, "Typed", "brass"},
		{keelclient.Proposal{Edited: true}, true, "Edited, typed", "brass"},
		{keelclient.Proposal{}, false, "Not typed", "outline"},
	} {
		if l, k := proposalState(c.p, c.typed); l != c.label || k != c.kind {
			t.Errorf("%+v: %q %q, want %q %q", c.p, l, k, c.label, c.kind)
		}
	}

	for _, c := range []struct {
		when time.Time
		n    int
		want string
	}{
		{lt(time.October, 18, 9, 14), 2, "Today 09:14 · 2 questions"},
		{lt(time.October, 17, 9, 14), 1, "Yesterday 09:14 · 1 question"},
		{lt(time.October, 14, 16, 2), 3, "Oct 14 16:02 · 3 questions"},
		{time.Time{}, 0, "0 questions"},
	} {
		if got := startedLabel(c.when, c.n, now); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}

func TestAListWithNoRowsSaysWhy(t *testing.T) {
	zero, thirty := 0, 30
	one := []keelclient.ConversationSummary{{ID: "a"}}
	for _, c := range []struct {
		name   string
		s      convListState
		groups int
		want   string
	}{
		{"rows", convListState{sessions: one, historyDays: &thirty}, 1, ""},
		{"no match", convListState{sessions: one, historyDays: &thirty}, 0, "No conversation matches."},
		{"history off", convListState{historyDays: &zero}, 0, "History is off: conversations are kept only while the panel is open. Turn it on in Settings · Agent."},
		{"none yet", convListState{historyDays: &thirty}, 0, "No saved conversations."},
		{"locked", convListState{err: "Unlock the vault to see saved conversations."}, 0, "Unlock the vault to see saved conversations."},
		{"unreadable", convListState{unreadable: true}, 0, "Saved conversations cannot be opened: keel.chats cannot be read. New ones are not saved until Delete all starts a fresh file."},
	} {
		if got := sessionsEmptyText(c.s, c.groups); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
