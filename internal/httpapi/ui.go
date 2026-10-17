package httpapi

import (
	"errors"
	"log"
	"net/http"
	goruntime "runtime"

	"github.com/a-h/templ"
	"github.com/starfederation/datastar-go/datastar"

	"keel/internal/store"
	"keel/internal/tunnel"
	"keel/internal/ui"
)

// uiSignals are the Datastar signals the vault lists read. StreamID names
// the page's /ui/stream (uistream.go), so a filter request can update the
// filter that stream renders with.
type uiSignals struct {
	Query      string `json:"query"`
	LogFilter  string `json:"logFilter"`
	GroupFocus string `json:"groupFocus"`
	StreamID   string `json:"streamId"`
}

func (s *Server) pageProps(r *http.Request) ui.PageProps {
	var sig uiSignals
	_ = datastar.ReadSignals(r, &sig)
	return s.listProps(r, sig)
}

// listProps is pageProps with the signals already read: the stream renders
// with the filters its page last sent, not with those of the request that
// opened it.
func (s *Server) listProps(r *http.Request, sig uiSignals) ui.PageProps {
	q := r.URL.Query()
	p := ui.PageProps{
		Locked:       s.store.Locked(),
		NeedsSetup:   s.store.NeedsSetup(),
		KeychainName: ui.KeychainName(goruntime.GOOS),
		Titlebar:     q.Get("chrome") == "titlebar",
		Native:       q.Get("native") == "1",
		Name:         q.Get("name"),
	}
	if s.share != nil && s.share.Enabled() {
		p.SignalURL = s.share.URL()
	}
	p.Query = sig.Query
	p.LogFilter = sig.LogFilter
	p.GroupFocus = sig.GroupFocus
	if p.Locked {
		return p
	}
	p.Hosts, _ = s.store.ListHosts()
	p.Groups, _ = s.store.ListGroups()
	p.Identities, _ = s.store.ListIdentities()
	p.Snippets, _ = s.store.ListSnippets()
	p.Notebooks, _ = s.store.ListNotebooks()
	if s.tunnels != nil {
		p.Tunnels = s.tunnels.List()
	} else if saved, err := s.store.ListTunnels(); err == nil {
		for _, t := range saved {
			p.Tunnels = append(p.Tunnels, tunnel.Info{Tunnel: t, Status: tunnel.StatusStopped})
		}
	}
	if s.logs != nil {
		p.Logs = s.logs.Recent(200)
	}
	return p
}

func writeTempl(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.Render(r.Context(), w); err != nil {
		log.Printf("templ: %v", err)
	}
}

func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	writeTempl(w, r, ui.Page(s.pageProps(r)))
}

// uiHosts answers the sidebar filter (@get from the search box). The query
// is recorded against the caller's stream, so the next push after a change
// keeps the filtered tree.
func (s *Server) uiHosts(w http.ResponseWriter, r *http.Request) {
	var sig uiSignals
	_ = datastar.ReadSignals(r, &sig)
	s.streams.setFilter(sig.StreamID, sig)
	writeTempl(w, r, ui.HostList(s.listProps(r, sig)))
}

func (s *Server) uiIdentities(w http.ResponseWriter, r *http.Request) {
	writeTempl(w, r, ui.IdentityList(s.pageProps(r)))
}

func (s *Server) uiTunnels(w http.ResponseWriter, r *http.Request) {
	writeTempl(w, r, ui.TunnelList(s.pageProps(r)))
}

func (s *Server) uiSnippets(w http.ResponseWriter, r *http.Request) {
	writeTempl(w, r, ui.SnippetList(s.pageProps(r)))
}

func (s *Server) uiNotebooks(w http.ResponseWriter, r *http.Request) {
	writeTempl(w, r, ui.NotebookList(s.pageProps(r)))
}

// uiLogs answers the Logs page search (@get) and its Refresh button; like
// uiHosts it records the filter against the caller's stream.
func (s *Server) uiLogs(w http.ResponseWriter, r *http.Request) {
	var sig uiSignals
	_ = datastar.ReadSignals(r, &sig)
	s.streams.setFilter(sig.StreamID, sig)
	writeTempl(w, r, ui.LogList(s.listProps(r, sig)))
}

func (s *Server) uiVault(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	pass := r.FormValue("passphrase")
	var err error
	if s.store.NeedsSetup() {
		err = s.store.Setup(pass)
	} else {
		err = s.store.Unlock(pass)
	}
	if err != nil {
		if errors.Is(err, store.ErrPassphrase) {
			writeError(w, http.StatusForbidden, "incorrect passphrase")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.note("vault", "vault unlocked", "", "", "")
	sse := datastar.NewSSE(w, r)
	_ = sse.Redirect("/")
}
