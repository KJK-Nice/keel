package httpapi

import (
	"bufio"
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

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
