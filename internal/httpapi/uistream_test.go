package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"keel/internal/applog"
	"keel/internal/store"
)

// sseReader collects Datastar SSE events from a /ui/stream response.
type sseReader struct {
	events chan string
}

func openStream(t *testing.T, srvURL, signals string) (*sseReader, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srvURL+"/ui/stream?datastar="+url.QueryEscape(signals), nil)
	req.Header.Set("Datastar-Request", "true")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("stream status %d", res.StatusCode)
	}
	r := &sseReader{events: make(chan string, 64)}
	go func() {
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		var ev strings.Builder
		for sc.Scan() {
			line := sc.Text()
			if line == "" {
				if ev.Len() > 0 {
					r.events <- ev.String()
					ev.Reset()
				}
				continue
			}
			ev.WriteString(line)
			ev.WriteByte('\n')
		}
		close(r.events)
	}()
	return r, cancel
}

// next returns the next event containing want, failing after a timeout.
func (r *sseReader) next(t *testing.T, want string) string {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev, ok := <-r.events:
			if !ok {
				t.Fatalf("stream closed waiting for %q", want)
			}
			if strings.Contains(ev, want) {
				return ev
			}
		case <-deadline:
			t.Fatalf("no event with %q", want)
		}
	}
}

var streamIDRe = regexp.MustCompile(`"streamId":"([0-9a-f]+)"`)

func TestUIStreamPushesListsOnChange(t *testing.T) {
	srv, _, st, _ := testServer(t)
	stream, cancel := openStream(t, srv.URL, `{"query":""}`)
	defer cancel()

	sig := stream.next(t, "datastar-patch-signals")
	if !streamIDRe.MatchString(sig) {
		t.Fatalf("no streamId in %q", sig)
	}
	stream.next(t, `id="host-list"`)

	if _, err := st.UpsertHost(store.Host{
		Name: "gpu-box", Address: "10.0.0.9", Port: 22, Username: "u",
		AuthType: "password", Password: "x",
	}); err != nil {
		t.Fatal(err)
	}
	ev := stream.next(t, "gpu-box")
	if !strings.Contains(ev, "datastar-patch-elements") || !strings.Contains(ev, `id="host-list"`) {
		t.Fatalf("store change did not push the host list: %q", ev)
	}
	// The same push tells term.js to refresh its JSON state.
	stream.next(t, "keel-vault-changed")
}

// A filter request names its stream; later pushes keep that filter.
func TestUIStreamKeepsFilterFromHostsRequest(t *testing.T) {
	srv, _, st, _ := testServer(t)
	for _, name := range []string{"web-a", "db-b"} {
		if _, err := st.UpsertHost(store.Host{
			Name: name, Address: "10.0.0.1", Port: 22, Username: "u", AuthType: "password", Password: "x",
		}); err != nil {
			t.Fatal(err)
		}
	}
	stream, cancel := openStream(t, srv.URL, `{"query":""}`)
	defer cancel()
	id := streamIDRe.FindStringSubmatch(stream.next(t, "streamId"))[1]
	stream.next(t, `id="host-list"`)

	res, err := http.Get(srv.URL + "/ui/hosts?datastar=" + url.QueryEscape(`{"query":"web","streamId":"`+id+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	if _, err := st.UpsertHost(store.Host{
		Name: "web-c", Address: "10.0.0.2", Port: 22, Username: "u", AuthType: "password", Password: "x",
	}); err != nil {
		t.Fatal(err)
	}
	ev := stream.next(t, "web-c")
	if strings.Contains(ev, "db-b") {
		t.Fatalf("push after the filter request ignored the filter: %q", ev)
	}
	if !strings.Contains(ev, "web-a") {
		t.Fatalf("filtered push lost a matching host: %q", ev)
	}
}

// New log entries reach an open stream (they replace the Logs page poll),
// at most once a second.
func TestUIStreamPushesLogs(t *testing.T) {
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	st, err := store.New(t.TempDir() + "/keel.json")
	if err != nil {
		t.Fatal(err)
	}
	logs := applog.New(t.TempDir() + "/keel.log")
	srv := httptest.NewServer(New(st, nil, fstest.MapFS{}, nil).WithLogs(logs).Handler())
	t.Cleanup(srv.Close)

	stream, cancel := openStream(t, srv.URL, `{}`)
	defer cancel()
	stream.next(t, `id="log-list"`)

	logs.Append(applog.Entry{Kind: "vault", Message: "first-event"})
	ev := stream.next(t, "first-event")
	if strings.Contains(ev, `id="host-list"`) {
		t.Fatalf("a log entry re-sent the other lists: %q", ev)
	}
	start := time.Now()
	logs.Append(applog.Entry{Kind: "vault", Message: "second-event"})
	stream.next(t, "second-event")
	if time.Since(start) < uiLogPushEvery/2 {
		t.Fatalf("log pushes not throttled: second came after %v", time.Since(start))
	}
}

var vaultRevRe = regexp.MustCompile(`keel-vault-changed", \{detail: \{"rev":"([^"]*)"\}\}`)

// The first push of a stream carries the rev the stream opened at: the
// value GET /api/vault reports while nothing changes, so term.js can skip
// the refresh its boot loadAll already did (issue #54). Later pushes carry
// no rev, and a change moves the rev, so a stream opened after it differs.
func TestUIStreamFirstPushCarriesVaultRev(t *testing.T) {
	srv, _, st, _ := testServer(t)
	vaultRev := func() string {
		t.Helper()
		res, err := http.Get(srv.URL + "/api/vault")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out struct{ Rev string }
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		if out.Rev == "" {
			t.Fatal("GET /api/vault has no rev")
		}
		return out.Rev
	}
	revOf := func(ev string) []string {
		t.Helper()
		m := vaultRevRe.FindStringSubmatch(ev)
		if m == nil {
			t.Fatalf("no keel-vault-changed detail in %q", ev)
		}
		return m
	}
	before := vaultRev()

	stream, cancel := openStream(t, srv.URL, `{"query":""}`)
	defer cancel()
	if got := revOf(stream.next(t, "keel-vault-changed"))[1]; got != before {
		t.Fatalf("first push rev = %q, want %q", got, before)
	}

	if _, err := st.UpsertHost(store.Host{
		Name: "gpu-box", Address: "10.0.0.9", Port: 22, Username: "u", AuthType: "password", Password: "x",
	}); err != nil {
		t.Fatal(err)
	}
	if got := revOf(stream.next(t, "keel-vault-changed"))[1]; got != "" {
		t.Fatalf("a push after a change carries rev %q, want none", got)
	}
	after := vaultRev()
	if after == before {
		t.Fatalf("rev stayed %q after a store change", after)
	}

	again, cancel2 := openStream(t, srv.URL, `{"query":""}`)
	defer cancel2()
	if got := revOf(again.next(t, "keel-vault-changed"))[1]; got != after {
		t.Fatalf("a stream opened after the change has rev %q, want %q", got, after)
	}
}
