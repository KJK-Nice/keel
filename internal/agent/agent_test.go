package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"keel/internal/agent/redact"
	"keel/internal/applog"
	"keel/internal/store"
)

type memLog struct {
	mu      sync.Mutex
	entries []applog.Entry
}

func (m *memLog) Append(e applog.Entry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, e)
}

func (m *memLog) all() []applog.Entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]applog.Entry(nil), m.entries...)
}

type fakeScrollback map[string]string

func (f fakeScrollback) ReadScrollback(id string) ([]byte, error) {
	s, ok := f[id]
	if !ok {
		return nil, errors.New("session not found")
	}
	return []byte(s), nil
}

type fakeSecrets []string

func (f fakeSecrets) KnownSecretValues() ([]string, error) { return f, nil }

func tptr(b bool) *bool { return &b }

func testHosts() []Host {
	return HostsFrom([]store.Host{
		{ID: "web", Name: "web-1", Address: "web.example.com", AgentReadable: tptr(true)},
		{ID: "db", Name: "prod-db-01", Address: "10.0.0.9", AgentReadable: tptr(false), Production: tptr(true)},
		{ID: "legacy", Name: "old", Address: "old.example.com"}, // never set: readable
	})
}

func TestHostsFromCopiesNoSecrets(t *testing.T) {
	hs := HostsFrom([]store.Host{{ID: "a", Name: "a", Address: "a.example.com", Password: "pw", PrivateKey: "k", Passphrase: "pp", Production: tptr(true)}})
	if len(hs) != 1 || hs[0] != (Host{ID: "a", Name: "a", Address: "a.example.com", AgentReadable: true, Production: true}) {
		t.Fatalf("HostsFrom = %+v", hs)
	}
}

// §6.2: only hosts with "Agent can read" on are in scope.
func TestScopeExcludesUnreadableHosts(t *testing.T) {
	scope := NewScope(testHosts())
	panes := []Pane{
		{SessionID: "s1", HostID: "web"},
		{SessionID: "s2", HostID: "db"},
		{SessionID: "s3", HostID: "legacy"},
		{SessionID: "s4", HostID: "deleted-host"},
		{SessionID: "s5"},
	}
	in, out := scope.Filter(panes)
	if len(in) != 2 || in[0].SessionID != "s1" || in[1].SessionID != "s3" {
		t.Fatalf("in = %+v", in)
	}
	if len(out) != 3 || out[0].HostID != "db" {
		t.Fatalf("out = %+v", out)
	}
}

func TestReadPaneRefusesOutOfScopeWithoutReading(t *testing.T) {
	log := &memLog{}
	a, _ := NewAuditor(log)
	src := &countingSource{}
	_, err := a.ReadPane(src, NewScope(testHosts()), Pane{SessionID: "s2", HostID: "db"}, 200, redact.New(redact.Options{}))
	if !errors.Is(err, ErrNotReadable) {
		t.Fatalf("err = %v", err)
	}
	if src.n != 0 {
		t.Fatal("an out-of-scope pane was read")
	}
	if len(log.all()) != 0 {
		t.Fatal("nothing was read, nothing to log")
	}
}

type countingSource struct{ n int }

func (c *countingSource) ReadScrollback(string) ([]byte, error) {
	c.n++
	return []byte("x"), nil
}

// §6.7 and §6.3/§6.4: a read is logged with counts, never content, and
// what it returns is redacted, vault values included.
func TestReadPaneRedactsAndLogs(t *testing.T) {
	log := &memLog{}
	a, err := NewAuditor(log)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for i := 0; i < 300; i++ {
		b.WriteString("line\r\n")
	}
	b.WriteString("\x1b[32m$\x1b[0m echo vault-pass-123 password=abc999\r\n")
	b.WriteString("ping web.example.com\r\n")
	src := fakeScrollback{"s1": b.String()}
	hosts := testHosts()
	r, err := NewRedactor(fakeSecrets{"vault-pass-123"}, store.AgentRedaction{Secrets: true, Hostnames: true}, hosts)
	if err != nil {
		t.Fatal(err)
	}
	ex, err := a.ReadPane(src, NewScope(hosts), Pane{SessionID: "s1", HostID: "web", HostName: "web-1", Role: RoleDriver}, 120, r)
	if err != nil {
		t.Fatal(err)
	}
	text := ex.Text.String()
	if ex.Lines != 120 || strings.Count(text, "\n") != 119 {
		t.Fatalf("lines = %d / %d", ex.Lines, strings.Count(text, "\n")+1)
	}
	for _, leak := range []string{"vault-pass-123", "abc999", "web.example.com", "\x1b"} {
		if strings.Contains(text, leak) {
			t.Fatalf("%q in excerpt", leak)
		}
	}
	if !strings.Contains(text, "$ echo [secret:1] password=[secret:2]") || !strings.Contains(text, "ping [host:1]") {
		t.Fatalf("excerpt tail = %q", text[len(text)-80:])
	}
	entries := log.all()
	if len(entries) != 1 {
		t.Fatalf("want 1 log entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Kind != EventRead || e.SessionID != "s1" || e.HostID != "web" || e.Lines != 120 {
		t.Fatalf("entry = %+v", e)
	}
	if e.Redactions["known_secret"] != 1 || e.Redactions["assignment"] != 1 || e.Redactions["hostname"] != 1 {
		t.Fatalf("redactions = %v", e.Redactions)
	}
	if strings.Contains(e.Message, "vault-pass") || strings.Contains(e.Message, "abc999") || strings.Contains(e.Message, "echo") {
		t.Fatalf("log carries content: %q", e.Message)
	}
	if !strings.Contains(e.Message, "web-1") || !strings.Contains(e.Message, "last 120 lines") || !strings.Contains(e.Message, "3 redactions") {
		t.Fatalf("message = %q", e.Message)
	}
}

func TestReadPaneClampsLines(t *testing.T) {
	log := &memLog{}
	a, _ := NewAuditor(log)
	src := fakeScrollback{"s1": strings.Repeat("l\n", 2000)}
	for _, c := range []struct{ ask, want int }{{5, 50}, {0, 200}, {5000, 1000}} {
		ex, err := a.ReadPane(src, NewScope(testHosts()), Pane{SessionID: "s1", HostID: "web"}, c.ask, redact.New(redact.Options{}))
		if err != nil || ex.Lines != c.want {
			t.Fatalf("ask %d → %d (%v), want %d", c.ask, ex.Lines, err, c.want)
		}
	}
}

func TestNoAuditorWithoutLog(t *testing.T) {
	if _, err := NewAuditor(nil); !errors.Is(err, ErrNoAuditLog) {
		t.Fatal("an auditor without a log must not exist")
	}
}

// §6.6: a follower cannot insert; §6.7: every insert/type is logged.
func TestInsertAndTypeRequireDriverAndLog(t *testing.T) {
	log := &memLog{}
	a, _ := NewAuditor(log)
	for _, role := range []Role{RoleFollower, RoleOwner, ""} {
		p := Pane{SessionID: "s1", HostID: "web", Role: role}
		if err := a.Insert(p, "ls\n"); err == nil {
			t.Fatalf("insert allowed for role %q", role)
		}
		if err := a.Type(p, "ls\n"); err == nil {
			t.Fatalf("type allowed for role %q", role)
		}
	}
	if err := CanInsert(RoleFollower); !errors.Is(err, ErrFollower) {
		t.Fatal("follower must get ErrFollower")
	}
	if len(log.all()) != 0 {
		t.Fatal("refused inserts wrote nothing to a pane and log nothing")
	}
	p := Pane{SessionID: "s1", HostID: "web", HostName: "web-1", Role: RoleDriver}
	if err := a.Insert(p, "rm -rf ./build"); err != nil {
		t.Fatal(err)
	}
	if err := a.Type(p, "cd /srv\nmake deploy\n"); err != nil {
		t.Fatal(err)
	}
	a.Suggest(p, redact.Report{Counts: map[redact.Kind]int{redact.KindIP: 2}}, "Destructive")
	got := log.all()
	if len(got) != 3 {
		t.Fatalf("want 3 entries, got %d", len(got))
	}
	if got[0].Kind != EventInsert || got[0].Bytes != len("rm -rf ./build") || got[0].Lines != 1 {
		t.Fatalf("insert entry = %+v", got[0])
	}
	if got[1].Kind != EventType || got[1].Lines != 2 || got[1].Bytes != len("cd /srv\nmake deploy\n") {
		t.Fatalf("type entry = %+v", got[1])
	}
	if got[2].Kind != EventSuggest || got[2].Redactions["ip"] != 2 || !strings.Contains(got[2].Message, "Destructive") {
		t.Fatalf("suggest entry = %+v", got[2])
	}
	for _, e := range got {
		if strings.Contains(e.Message, "rm -rf") || strings.Contains(e.Message, "make deploy") {
			t.Fatalf("command content logged: %q", e.Message)
		}
	}
}

func TestLastLines(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		want string
		got  int
	}{
		{"a\nb\nc\n", 2, "b\nc", 2},
		{"a\nb", 5, "a\nb", 2},
		{"", 3, "", 0},
		{"\n\n", 3, "", 0},
		{"a", 0, "", 0},
	} {
		s, n := LastLines(c.in, c.n)
		if s != c.want || n != c.got {
			t.Errorf("LastLines(%q,%d) = %q,%d", c.in, c.n, s, n)
		}
	}
}

func TestNewRedactorUsesSettings(t *testing.T) {
	hosts := testHosts()
	r, _ := NewRedactor(fakeSecrets{}, store.AgentRedaction{Secrets: true}, hosts)
	if got := r.Text("ssh 10.0.0.9 web.example.com").String(); got != "ssh 10.0.0.9 web.example.com" {
		t.Fatalf("ips/hosts redacted with options off: %q", got)
	}
	r, _ = NewRedactor(fakeSecrets{}, store.AgentRedaction{Secrets: true, IPs: true}, hosts)
	if got := r.Text("ssh 10.0.0.9 web.example.com").String(); got != "ssh [ip:1] web.example.com" {
		t.Fatalf("got %q", got)
	}
}

// §6.7 end to end: each helper writes one line to keel.log, with counts
// and never the content.
func TestEveryHelperWritesKeelLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keel.log")
	book := applog.New(path)
	a, err := NewAuditor(book)
	if err != nil {
		t.Fatal(err)
	}
	hosts := testHosts()
	p := Pane{SessionID: "s1", HostID: "web", HostName: "web-1", Role: RoleDriver}
	r, _ := NewRedactor(fakeSecrets{"topsecret-1"}, store.AgentRedaction{Secrets: true}, hosts)
	if _, err := a.ReadPane(fakeScrollback{"s1": "cat .env\nAPI_TOKEN=topsecret-1\n"}, NewScope(hosts), p, 50, r); err != nil {
		t.Fatal(err)
	}
	a.Suggest(p, r.Report(), "Read-only")
	if err := a.Insert(p, "grep -r TODO ."); err != nil {
		t.Fatal(err)
	}
	if err := a.Type(p, "ls -la\n"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	want := []string{EventRead, EventSuggest, EventInsert, EventType}
	if len(lines) != len(want) {
		t.Fatalf("keel.log has %d lines, want %d:\n%s", len(lines), len(want), b)
	}
	for i, line := range lines {
		var e applog.Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatal(err)
		}
		if e.Kind != want[i] || e.SessionID != "s1" || e.HostID != "web" {
			t.Fatalf("line %d = %+v", i, e)
		}
	}
	for _, leak := range []string{"topsecret-1", "API_TOKEN", "grep -r", "ls -la", "cat .env"} {
		if strings.Contains(string(b), leak) {
			t.Fatalf("keel.log carries content %q", leak)
		}
	}
}
