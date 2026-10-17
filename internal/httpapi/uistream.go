package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/a-h/templ"
	"github.com/starfederation/datastar-go/datastar"

	"keel/internal/ui"
)

// The vault lists (host tree, keychain, forwards, snippets, notebooks) reach
// the page over one long-lived Datastar stream, GET /ui/stream, opened by
// data-init on the page (page.templ). Whenever the store saves or a tunnel
// changes state, every open stream re-renders the lists and patches them
// into the page, morphed by element id. Filters stay request/response: the
// sidebar search calls @get('/ui/hosts'), which answers the filtered list
// and records the query against the caller's stream (its streamId signal),
// so a later push keeps the same filter.

// uiStreamCoalesce batches bursts of changes (a group delete touches many
// hosts; a tunnel goes connecting then ready) into one push.
const uiStreamCoalesce = 40 * time.Millisecond

type uiStreams struct {
	mu   sync.Mutex
	open map[string]*uiStream
}

type uiStream struct {
	mu   sync.Mutex
	sig  uiSignals
	kick chan struct{}
}

func newUIStreams() *uiStreams {
	return &uiStreams{open: map[string]*uiStream{}}
}

func (h *uiStreams) add(sig uiSignals) (string, *uiStream) {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	st := &uiStream{sig: sig, kick: make(chan struct{}, 1)}
	h.mu.Lock()
	h.open[id] = st
	h.mu.Unlock()
	return id, st
}

func (h *uiStreams) remove(id string) {
	h.mu.Lock()
	delete(h.open, id)
	h.mu.Unlock()
}

// kickAll asks every stream to re-render. It never blocks: it runs from the
// store's and the tunnel runtime's change hooks, under their locks.
func (h *uiStreams) kickAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, st := range h.open {
		select {
		case st.kick <- struct{}{}:
		default:
		}
	}
}

// setFilter records the filters a stream's page last asked for.
func (h *uiStreams) setFilter(id string, sig uiSignals) {
	if id == "" {
		return
	}
	h.mu.Lock()
	st := h.open[id]
	h.mu.Unlock()
	if st == nil {
		return
	}
	st.mu.Lock()
	st.sig = sig
	st.mu.Unlock()
}

func (st *uiStream) signals() uiSignals {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.sig
}

func (s *Server) uiStream(w http.ResponseWriter, r *http.Request) {
	var sig uiSignals
	_ = datastar.ReadSignals(r, &sig)
	id, st := s.streams.add(sig)
	defer s.streams.remove(id)

	sse := datastar.NewSSE(w, r)
	if err := sse.MarshalAndPatchSignals(map[string]any{"streamId": id}); err != nil {
		return
	}
	// The page was rendered with these lists, but a reconnect (keel serve
	// restarted, the laptop slept) must not keep showing stale ones.
	if err := s.pushLists(sse, r, st.signals()); err != nil {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-st.kick:
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(uiStreamCoalesce):
		}
		select {
		case <-st.kick:
		default:
		}
		if err := s.pushLists(sse, r, st.signals()); err != nil {
			return
		}
	}
}

func (s *Server) pushLists(sse *datastar.ServerSentEventGenerator, r *http.Request, sig uiSignals) error {
	p := s.listProps(r, sig)
	for _, c := range []templ.Component{
		ui.HostList(p),
		ui.IdentityList(p),
		ui.TunnelList(p),
		ui.SnippetList(p),
		ui.NotebookList(p),
	} {
		if err := sse.PatchElementTempl(c); err != nil {
			return err
		}
	}
	return nil
}
