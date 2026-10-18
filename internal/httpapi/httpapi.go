package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"keel/components"
	"keel/internal/agent/service"
	"keel/internal/applog"
	"keel/internal/inject"
	"keel/internal/runtime"
	"keel/internal/session"
	"keel/internal/share"
	"keel/internal/store"
	"keel/internal/tunnel"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  32 * 1024,
	WriteBufferSize: 32 * 1024,
	CheckOrigin:     allowLocalOrigin,
}

type Server struct {
	store   *store.Store
	rt      *runtime.Runtime
	web     fs.FS
	share   *share.Hub
	tunnels *tunnel.Runtime
	logs    *applog.Book
	onStop  func()
	// started, addr and dataDir describe this keel serve for GET /api/runtime.
	started time.Time
	addr    string
	dataDir string
	// authToken, when set, is required on privileged routes (auth.go).
	authToken string
	build     string
	// sshDir is ~/.ssh (KEEL_SSH_DIR), read by the first-run host import.
	sshDir string
	// The agent panel (agentask.go). agent is nil without a log.
	agent        *service.Service
	agentFactory service.Factory
	agentTest    bool
	// agentJudge builds the detector (Jev) for in-pane suggestions.
	agentJudge        service.JudgeFactory
	agentDetectWindow time.Duration
	// streams are the open /ui/stream connections (uistream.go).
	streams *uiStreams
}

func New(st *store.Store, rt *runtime.Runtime, web fs.FS, sh *share.Hub) *Server {
	s := &Server{store: st, rt: rt, web: web, share: sh, started: time.Now(), streams: newUIStreams()}
	if st != nil {
		st.OnChange(s.streams.kickAll)
	}
	return s
}

// WithRuntimeInfo sets the listen address and data directory that GET
// /api/runtime reports (Settings · Runtime).
func (s *Server) WithRuntimeInfo(addr, dataDir string) *Server {
	s.addr = addr
	s.dataDir = dataDir
	return s
}

func (s *Server) WithTunnels(t *tunnel.Runtime) *Server {
	s.tunnels = t
	if t != nil {
		t.OnChange(s.streams.kickAll)
	}
	return s
}

func (s *Server) WithLogs(l *applog.Book) *Server {
	s.logs = l
	if l != nil {
		l.OnAppend(s.streams.kickLogs)
	}
	if s.rt != nil {
		s.rt.SetNotify(func(kind, msg, hostID, sessionID string) {
			s.note(kind, msg, hostID, sessionID, "")
		})
	}
	return s
}

// WithBuild sets the build id /api/health reports (see internal/buildid).
func (s *Server) WithBuild(id string) *Server {
	s.build = id
	return s
}

func (s *Server) WithStop(fn func()) *Server {
	s.onStop = fn
	return s
}

func (s *Server) Handler() http.Handler {
	s.initAgent()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/runtime", s.runtimeInfo)
	mux.HandleFunc("POST /api/runtime/stop", s.stopRuntime)
	mux.HandleFunc("POST /api/vault/unlock", s.vaultUnlock)
	mux.HandleFunc("POST /api/vault/setup", s.vaultSetup)
	mux.HandleFunc("GET /api/vault", s.vaultState)
	mux.HandleFunc("POST /api/first-run/done", s.finishFirstRun)
	mux.HandleFunc("GET /api/ssh-config/preview", s.sshConfigPreview)
	mux.HandleFunc("POST /api/ssh-config/import", s.sshConfigImport)
	mux.HandleFunc("POST /api/share", s.shareSession)
	mux.HandleFunc("DELETE /api/share/{id}", s.stopShare)
	mux.HandleFunc("POST /api/share/{id}/admit", s.admitShare)
	mux.HandleFunc("GET /api/hosts", s.listHosts)
	mux.HandleFunc("POST /api/hosts", s.upsertHost)
	mux.HandleFunc("PUT /api/hosts/{id}", s.updateHost)
	mux.HandleFunc("POST /api/hosts/{id}/duplicate", s.duplicateHost)
	mux.HandleFunc("POST /api/hosts/move", s.moveHosts)
	mux.HandleFunc("POST /api/hosts/test", s.testHost)
	mux.HandleFunc("GET /api/hosts/{id}/host-key", s.getHostKey)
	mux.HandleFunc("DELETE /api/hosts/{id}/host-key", s.forgetHostKey)
	mux.HandleFunc("DELETE /api/hosts/{id}", s.deleteHost)
	mux.HandleFunc("GET /api/groups", s.listGroups)
	mux.HandleFunc("POST /api/groups", s.upsertGroup)
	mux.HandleFunc("PUT /api/groups/{id}", s.updateGroup)
	mux.HandleFunc("DELETE /api/groups/{id}", s.deleteGroup)
	mux.HandleFunc("GET /api/snippets", s.listSnippets)
	mux.HandleFunc("POST /api/snippets", s.upsertSnippet)
	mux.HandleFunc("PUT /api/snippets/{id}", s.updateSnippet)
	mux.HandleFunc("DELETE /api/snippets/{id}", s.deleteSnippet)
	mux.HandleFunc("GET /api/notebooks", s.listNotebooks)
	mux.HandleFunc("POST /api/notebooks", s.upsertNotebook)
	mux.HandleFunc("PUT /api/notebooks/{id}", s.updateNotebook)
	mux.HandleFunc("DELETE /api/notebooks/{id}", s.deleteNotebook)
	mux.HandleFunc("GET /api/identities", s.listIdentities)
	mux.HandleFunc("POST /api/identities", s.upsertIdentity)
	mux.HandleFunc("PUT /api/identities/{id}", s.updateIdentity)
	mux.HandleFunc("DELETE /api/identities/{id}", s.deleteIdentity)
	mux.HandleFunc("GET /api/tunnels", s.listTunnels)
	mux.HandleFunc("POST /api/tunnels", s.upsertTunnel)
	mux.HandleFunc("PUT /api/tunnels/{id}", s.updateTunnel)
	mux.HandleFunc("DELETE /api/tunnels/{id}", s.deleteTunnel)
	mux.HandleFunc("POST /api/tunnels/{id}/start", s.startTunnel)
	mux.HandleFunc("POST /api/tunnels/{id}/stop", s.stopTunnel)
	mux.HandleFunc("GET /api/logs", s.listLogs)
	mux.HandleFunc("GET /api/agent/settings", s.getAgentSettings)
	mux.HandleFunc("PUT /api/agent/settings", s.putAgentSettings)
	mux.HandleFunc("GET /api/agent/providers", s.listAgentProviders)
	mux.HandleFunc("PUT /api/agent/keys/{provider}", s.putAgentKey)
	mux.HandleFunc("DELETE /api/agent/keys/{provider}", s.deleteAgentKey)
	mux.HandleFunc("POST /api/agent/ask", s.agentAsk)
	mux.HandleFunc("PUT /api/agent/conversations/{id}/proposals/{pid}", s.agentEditProposal)
	mux.HandleFunc("GET /api/agent/conversations", s.agentSessions)
	mux.HandleFunc("GET /api/agent/conversations/{id}", s.agentTranscript)
	mux.HandleFunc("PATCH /api/agent/conversations/{id}", s.agentRename)
	mux.HandleFunc("POST /api/agent/conversations/{id}/proposals/{pid}/again", s.agentProposeAgain)
	mux.HandleFunc("DELETE /api/agent/conversations/{id}", s.agentReset)
	mux.HandleFunc("DELETE /api/agent/conversations", s.agentResetAll)
	mux.HandleFunc("POST /api/agent/conversations/{id}/notes/{nid}", s.agentSaveNoteProposal)
	mux.HandleFunc("DELETE /api/agent/conversations/{id}/notes/{nid}", s.agentDismissNoteProposal)
	mux.HandleFunc("POST /api/agent/conversations/{id}/notes/{nid}/undo", s.agentUndoNoteProposal)
	mux.HandleFunc("GET /api/agent/notes", s.agentNotes)
	mux.HandleFunc("POST /api/agent/notes", s.agentAddNote)
	mux.HandleFunc("PUT /api/agent/notes/{id}", s.agentEditNote)
	mux.HandleFunc("DELETE /api/agent/notes/{id}", s.agentDeleteNote)
	mux.HandleFunc("DELETE /api/agent/notes", s.agentDeleteAllNotes)
	mux.HandleFunc("GET /api/agent/runbooks", s.agentRunbooks)
	mux.HandleFunc("POST /api/agent/runbooks", s.agentAddRunbook)
	mux.HandleFunc("POST /api/agent/runbooks/draft", s.agentRunbookDraft)
	mux.HandleFunc("PUT /api/agent/runbooks/{id}", s.agentUpdateRunbook)
	mux.HandleFunc("PUT /api/agent/runbooks/{id}/pick", s.agentRunbookPick)
	mux.HandleFunc("DELETE /api/agent/runbooks/{id}", s.agentDeleteRunbook)
	mux.HandleFunc("POST /api/agent/conversations/{id}/runbooks/{rid}", s.agentSaveRunbookProposal)
	mux.HandleFunc("DELETE /api/agent/conversations/{id}/runbooks/{rid}", s.agentDismissRunbookProposal)
	mux.HandleFunc("POST /api/agent/conversations/{id}/runbooks/{rid}/undo", s.agentUndoRunbookProposal)
	mux.HandleFunc("GET /api/agent/conversations/{id}/runbook", s.agentRunState)
	mux.HandleFunc("POST /api/agent/conversations/{id}/runbook/steps/{sid}", s.agentStepAction)
	mux.HandleFunc("DELETE /api/agent/conversations/{id}/runbook", s.agentStopRunbook)
	mux.HandleFunc("POST /api/agent/conversations/{id}/runbook/move", s.agentMoveRunbook)
	mux.HandleFunc("POST /api/known-hosts", s.trustHostKey)
	mux.HandleFunc("GET /api/sessions", s.listSessions)
	mux.HandleFunc("POST /api/sessions", s.spawnSession)
	mux.HandleFunc("GET /api/sessions/{id}/scrollback", s.readScrollback)
	mux.HandleFunc("POST /api/sessions/{id}/reconnect", s.reconnectSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", s.killSession)
	mux.HandleFunc("GET /ws/session", s.sessionWS)
	mux.HandleFunc("GET /ws/herd", s.herdWS)
	mux.HandleFunc("GET /{$}", s.page)
	mux.HandleFunc("GET /ui/stream", s.uiStream)
	mux.HandleFunc("POST /ui/forms/{kind}", s.uiForm)
	mux.HandleFunc("GET /ui/hosts", s.uiHosts)
	mux.HandleFunc("GET /ui/logs", s.uiLogs)
	mux.Handle("GET /components/{bundle}", components.ScriptsHandler())
	mux.Handle("/", newStaticHandler(s.web))
	return withLogging(withLoopbackHost(withLocalOrigin(s.withAuth(s.withVault(mux)))))
}

// allowLocalOrigin blocks cross-site use of the privileged loopback API.
// Missing Origin is allowed (native clients and tests). A present Origin must
// be http(s) on a loopback host and the same host:port as this request, so a
// page on the public internet — or another local port — cannot drive WebSockets
// or mutate the vault.
func allowLocalOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	if !isLoopbackHost(u.Hostname()) {
		return false
	}
	return originHostPort(u) == requestHostPort(r)
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func originHostPort(u *url.URL) string {
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return net.JoinHostPort(host, port)
}

func requestHostPort(r *http.Request) string {
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
		if r.TLS != nil {
			port = "443"
		} else {
			port = "80"
		}
	}
	return net.JoinHostPort(host, port)
}

func withLocalOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws/") || strings.HasPrefix(r.URL.Path, "/ui/") {
			if !allowLocalOrigin(r) {
				writeError(w, http.StatusForbidden, "origin not allowed")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withVault(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" || r.URL.Path == "/api/runtime/stop" || r.URL.Path == "/api/vault/unlock" || r.URL.Path == "/api/vault/setup" || r.URL.Path == "/api/vault" {
			next.ServeHTTP(w, r)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/ws/") && !strings.HasPrefix(r.URL.Path, "/ui/") {
			next.ServeHTTP(w, r)
			return
		}
		if s.store.Locked() {
			writeError(w, http.StatusLocked, "vault locked")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{
		"status":     "ok",
		"role":       "runtime",
		"sessions":   s.rt.Count(),
		"pid":        os.Getpid(),
		"locked":     s.store.Locked(),
		"needsSetup": s.store.NeedsSetup(),
		"signal":     s.share != nil && s.share.Enabled(),
	}
	if s.build != "" {
		out["build"] = s.build
	}
	if s.share != nil && s.share.Enabled() {
		out["signalURL"] = s.share.URL()
	}
	if proof, ok := s.healthProof(r); ok {
		out["proof"] = proof
	}
	writeJSON(w, http.StatusOK, out)
}

// runtimeInfo describes this keel serve for Settings · Runtime. Behind the
// auth token like the rest of /api/ (unlike /api/health). It holds no
// secret: paths, counts and times only.
func (s *Server) runtimeInfo(w http.ResponseWriter, _ *http.Request) {
	out := map[string]any{
		"build":     s.build,
		"pid":       os.Getpid(),
		"addr":      s.addr,
		"dataDir":   s.dataDir,
		"sessions":  s.rt.Count(),
		"startedAt": s.started.UTC().Format(time.RFC3339),
		"uptimeSec": int64(time.Since(s.started).Seconds()),
		"canStop":   s.onStop != nil,
		"signalURL": "",
	}
	if s.share != nil && s.share.Enabled() {
		out["signalURL"] = s.share.URL()
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) stopRuntime(w http.ResponseWriter, _ *http.Request) {
	if s.onStop == nil {
		writeError(w, http.StatusServiceUnavailable, "runtime stop is not enabled")
		return
	}
	w.WriteHeader(http.StatusAccepted)
	go s.onStop()
}

func (s *Server) vaultUnlock(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	if err := s.store.Unlock(req.Passphrase); err != nil {
		if errors.Is(err, store.ErrPassphrase) {
			writeError(w, http.StatusForbidden, "incorrect passphrase")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.note("vault", "vault unlocked", "", "", "")
	writeJSON(w, http.StatusOK, map[string]any{"locked": false})
}

func (s *Server) vaultSetup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	if err := s.store.Setup(req.Passphrase); err != nil {
		if errors.Is(err, store.ErrVaultExists) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.note("vault", "vault created", "", "", "")
	writeJSON(w, http.StatusOK, map[string]any{"locked": false})
}

func (s *Server) shareSession(w http.ResponseWriter, r *http.Request) {
	if s.share == nil || !s.share.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "set KEEL_SIGNAL to a public signaling server")
		return
	}
	var req struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SessionID == "" {
		writeError(w, http.StatusBadRequest, "sessionId is required")
		return
	}
	join, err := s.share.Share(req.SessionID)
	if err != nil {
		if errors.Is(err, runtime.ErrNotFound) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	// WKWebView clipboard dies after await fetch (user gesture gone).
	pasteboardWrite(join)
	writeJSON(w, http.StatusOK, map[string]string{"url": join})
}

// admitShare answers a share joiner's request to watch:
// {"peerId": "…", "allow": true}.
func (s *Server) admitShare(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PeerID string `json:"peerId"`
		Allow  bool   `json:"allow"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PeerID == "" {
		writeError(w, http.StatusBadRequest, "peerId is required")
		return
	}
	if s.share == nil {
		writeError(w, http.StatusNotFound, "session is not shared")
		return
	}
	if err := s.share.Admit(r.PathValue("id"), req.PeerID, req.Allow); err != nil {
		status := http.StatusConflict // the request expired or was already answered
		switch {
		case errors.Is(err, share.ErrNotShared):
			status = http.StatusNotFound
		case errors.Is(err, share.ErrNotResponding):
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// stopShare ends sharing for a session: viewers are disconnected and the
// join link stops working. Stopping a session that isn't shared is a no-op.
func (s *Server) stopShare(w http.ResponseWriter, r *http.Request) {
	if s.share != nil {
		s.share.Stop(r.PathValue("id"))
	}
	w.WriteHeader(http.StatusNoContent)
}

func pasteboardWrite(s string) {
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "windows":
		cmd = exec.Command("cmd", "/c", "clip")
	default:
		if _, err := exec.LookPath("wl-copy"); err == nil {
			cmd = exec.Command("wl-copy")
		} else {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		}
	}
	cmd.Stdin = strings.NewReader(s)
	if err := cmd.Run(); err != nil {
		log.Printf("clipboard: %v", err)
	}
}

func (s *Server) listHosts(w http.ResponseWriter, _ *http.Request) {
	hosts, err := s.store.ListHosts()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	out := make([]store.Host, len(hosts))
	for i, h := range hosts {
		out[i] = redactHost(h)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) upsertHost(w http.ResponseWriter, r *http.Request) {
	var h store.Host
	if err := json.NewDecoder(r.Body).Decode(&h); err != nil {
		writeError(w, http.StatusBadRequest, "invalid host payload")
		return
	}
	h.ID = ""
	saved, err := s.saveHost(h)
	if err != nil {
		writeSaveErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, redactHost(saved))
}

func (s *Server) updateHost(w http.ResponseWriter, r *http.Request) {
	var h store.Host
	if err := json.NewDecoder(r.Body).Decode(&h); err != nil {
		writeError(w, http.StatusBadRequest, "invalid host payload")
		return
	}
	h.ID = r.PathValue("id")
	saved, err := s.saveHost(h)
	if err != nil {
		writeSaveErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, redactHost(saved))
}

func (s *Server) duplicateHost(w http.ResponseWriter, r *http.Request) {
	saved, err := s.store.DuplicateHost(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "host not found")
			return
		}
		if strings.Contains(err.Error(), "cannot be duplicated") {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, redactHost(saved))
	s.note("host", "host duplicated: "+saved.Name, saved.ID, "", "")
}

type moveHostsReq struct {
	IDs     []string `json:"ids"`
	GroupID string   `json:"groupId"`
}

func (s *Server) moveHosts(w http.ResponseWriter, r *http.Request) {
	var req moveHostsReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid move payload")
		return
	}
	if err := s.store.AssignHostsGroup(req.IDs, req.GroupID); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
	s.note("group", "hosts moved", "", "", "")
}

func (s *Server) listGroups(w http.ResponseWriter, _ *http.Request) {
	groups, err := s.store.ListGroups()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, groups)
}

func (s *Server) upsertGroup(w http.ResponseWriter, r *http.Request) {
	var g store.Group
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		writeError(w, http.StatusBadRequest, "invalid group payload")
		return
	}
	g.ID = ""
	if strings.TrimSpace(g.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	saved, err := s.store.UpsertGroup(g)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, saved)
	s.note("group", "group created: "+saved.Name, "", "", "")
}

func (s *Server) updateGroup(w http.ResponseWriter, r *http.Request) {
	var g store.Group
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		writeError(w, http.StatusBadRequest, "invalid group payload")
		return
	}
	g.ID = r.PathValue("id")
	if strings.TrimSpace(g.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	saved, err := s.store.UpsertGroup(g)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
	s.note("group", "group updated: "+saved.Name, "", "", "")
}

func (s *Server) deleteGroup(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteGroup(r.PathValue("id")); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
	s.note("group", "group deleted", "", "", "")
}

func (s *Server) deleteHost(w http.ResponseWriter, r *http.Request) {
	host, err := s.store.GetHost(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "host not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if host.AuthType == "local" {
		writeError(w, http.StatusBadRequest, "the local shell host cannot be removed")
		return
	}
	if err := s.store.DeleteHost(r.PathValue("id")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "host not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
	s.note("host", "host deleted: "+host.Name, host.ID, "", "")
}

func (s *Server) listSnippets(w http.ResponseWriter, _ *http.Request) {
	snips, err := s.store.ListSnippets()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, snips)
}

// snippetPayload is a snippet as a client sends it. AgentUsable is a
// pointer: a client that does not send it (the old edit modal) leaves the
// flag as it is, so a stale list can never clear it by accident.
type snippetPayload struct {
	Name        string `json:"name"`
	Command     string `json:"command"`
	AgentUsable *bool  `json:"agentUsable"`
}

// noteSnippetFlag logs a change of "Let the agent suggest this snippet"
// as an agent event: the snippet's name, never its command.
func (s *Server) noteSnippetFlag(name string, before, after bool) {
	if before == after {
		return
	}
	msg := "snippet \"" + name + "\" may no longer be suggested by the agent"
	if after {
		msg = "snippet \"" + name + "\" may be suggested by the agent; its command is sent, redacted, to TypeSafe Jev"
	}
	s.note("agent", msg, "", "", "")
}

func (s *Server) upsertSnippet(w http.ResponseWriter, r *http.Request) {
	var p snippetPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid snippet payload")
		return
	}
	saved, err := s.saveSnippet("", p.Name, p.Command, p.AgentUsable)
	if err != nil {
		writeSaveErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, saved)
}

func (s *Server) updateSnippet(w http.ResponseWriter, r *http.Request) {
	var p snippetPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid snippet payload")
		return
	}
	saved, err := s.saveSnippet(r.PathValue("id"), p.Name, p.Command, p.AgentUsable)
	if err != nil {
		writeSaveErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) deleteSnippet(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteSnippet(r.PathValue("id")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "snippet not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
	s.note("snippet", "snippet deleted", "", "", "")
}

func (s *Server) listNotebooks(w http.ResponseWriter, _ *http.Request) {
	nbs, err := s.store.ListNotebooks()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nbs)
}

func (s *Server) upsertNotebook(w http.ResponseWriter, r *http.Request) {
	var nb store.Notebook
	if err := json.NewDecoder(r.Body).Decode(&nb); err != nil {
		writeError(w, http.StatusBadRequest, "invalid notebook payload")
		return
	}
	nb.ID = ""
	saved, err := s.store.UpsertNotebook(nb)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, saved)
	s.note("notebook", "notebook created: "+saved.Name, "", "", "")
}

func (s *Server) updateNotebook(w http.ResponseWriter, r *http.Request) {
	var nb store.Notebook
	if err := json.NewDecoder(r.Body).Decode(&nb); err != nil {
		writeError(w, http.StatusBadRequest, "invalid notebook payload")
		return
	}
	nb.ID = r.PathValue("id")
	saved, err := s.store.UpsertNotebook(nb)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, saved)
	s.note("notebook", "notebook updated: "+saved.Name, "", "", "")
}

func (s *Server) deleteNotebook(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteNotebook(r.PathValue("id")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "notebook not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
	s.note("notebook", "notebook deleted", "", "", "")
}

func (s *Server) listSessions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.rt.List())
}

func (s *Server) spawnSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		HostID string `json:"hostId"`
		Cols   int    `json:"cols"`
		Rows   int    `json:"rows"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid session payload")
		return
	}
	info, err := s.rt.Spawn(req.HostID, req.Cols, req.Rows)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "host not found")
			return
		}
		if errors.Is(err, store.ErrHostField) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.note("session", "session spawned: "+info.Title, info.HostID, info.ID, "")
	writeJSON(w, http.StatusCreated, info)
}

// readScrollback is the inspection path for clients (and later AI). It always
// returns redacted history. Live UI replay uses Subscribe snapshots on
// /ws/session, which stay raw. There is no raw= query bypass.
func (s *Server) readScrollback(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	text, err := s.rt.ReadScrollback(id)
	if err != nil {
		if errors.Is(err, runtime.ErrNotFound) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":       id,
		"redacted": true,
		"text":     string(text),
	})
}

func (s *Server) reconnectSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	info, err := s.rt.Reconnect(id)
	if err != nil {
		if errors.Is(err, runtime.ErrNotFound) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.note("session", "session reconnect: "+info.Title, info.HostID, info.ID, "")
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) killSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.rt.Kill(id); err != nil {
		if errors.Is(err, runtime.ErrNotFound) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.share != nil {
		s.share.Stop(id)
	}
	s.note("session", "session killed", "", id, "")
	w.WriteHeader(http.StatusNoContent)
}

type wsConn struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

const (
	wsPingPeriod = 20 * time.Second
	wsPongWait   = 60 * time.Second
)

func (w *wsConn) writeJSON(v any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteJSON(v)
}

func (w *wsConn) writeBinary(p []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteMessage(websocket.BinaryMessage, p)
}

func (w *wsConn) writePing() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
}

func (w *wsConn) startPings(done <-chan struct{}) {
	w.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	w.conn.SetPongHandler(func(string) error {
		return w.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	})
	go func() {
		t := time.NewTicker(wsPingPeriod)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if err := w.writePing(); err != nil {
					_ = w.conn.Close()
					return
				}
			}
		}
	}()
}

func (s *Server) herdWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}
	defer conn.Close()
	ws := &wsConn{conn: conn}
	id, events, snap := s.rt.SubscribeHerd()
	defer s.rt.UnsubscribeHerd(id)
	if err := ws.writeJSON(map[string]any{
		"type":     "snapshot",
		"sessions": snap,
	}); err != nil {
		return
	}
	pingDone := make(chan struct{})
	defer close(pingDone)
	ws.startPings(pingDone)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
			_ = conn.SetReadDeadline(time.Now().Add(wsPongWait))
		}
	}()
	for {
		select {
		case <-done:
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if err := ws.writeJSON(ev); err != nil {
				return
			}
		}
	}
}

func (s *Server) sessionWS(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		id = r.URL.Query().Get("sessionId")
	}
	cols, rows := session.ParseSize(r.URL.Query().Get("cols"), r.URL.Query().Get("rows"))
	name := r.URL.Query().Get("name")
	kind := r.URL.Query().Get("kind")

	if id == "" {
		hostID := r.URL.Query().Get("hostId")
		if hostID == "" {
			writeError(w, http.StatusBadRequest, "id or hostId is required")
			return
		}
		info, err := s.rt.Spawn(hostID, cols, rows)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		id = info.ID
	}

	sess, err := s.rt.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}
	ws := &wsConn{conn: conn}
	defer conn.Close()
	info := sess.Info()
	_ = ws.writeJSON(map[string]any{
		"type":    "status",
		"message": "Attaching…",
		"id":      info.ID,
		"status":  info.Status,
	})
	cl, events, snap := sess.Subscribe(runtime.Join{Name: name, Kind: kind})
	defer sess.Unsubscribe(cl.ID)
	if cl.Role == runtime.RoleDriver {
		_ = sess.Resize(cl.ID, cols, rows)
	}
	if len(snap) > 0 {
		_ = ws.writeBinary(snap)
	}
	info = sess.Info()
	_ = ws.writeJSON(sessionPayload("ready", "attached", info, cl, 0, 0))

	pingDone := make(chan struct{})
	defer close(pingDone)
	ws.startPings(pingDone)
	// In-pane suggestions (agentsuggest.go) run off this loop and stop
	// with the socket.
	sugCtx, sugCancel := context.WithCancel(r.Context())
	defer sugCancel()
	sug := newSuggestConn(sugCtx, s, ws, sess, cl.ID)

	go func() {
		defer conn.Close()
		for ev := range events {
			if ev.Kind == "bytes" {
				if err := ws.writeBinary(ev.Bytes); err != nil {
					return
				}
				continue
			}
			if ev.Kind == "join-request" || ev.Kind == "join-resolved" || ev.Kind == "join-failed" {
				// From the share hub (Session.Announce): someone asked to
				// watch, or that request was answered. No session state.
				_ = ws.writeJSON(map[string]any{"type": ev.Kind, "id": info.ID, "peerId": ev.Peer, "message": ev.Message})
				continue
			}
			msg := sessionPayload(ev.Kind, ev.Message, ev.Info, cl, ev.FromID, ev.TargetID)
			if ev.Kind == "status" {
				msg["status"] = ev.Status
				msg["id"] = info.ID
			}
			if ev.Status == runtime.StatusError {
				errMsg := map[string]any{"type": "error", "message": ev.Message}
				if ev.Info.ErrorKind != "" {
					errMsg["errorKind"] = ev.Info.ErrorKind
					errMsg["hostname"] = ev.Info.HostKeyHost
					errMsg["fingerprint"] = ev.Info.HostKeyFingerprint
					errMsg["expected"] = ev.Info.HostKeyExpected
					errMsg["keyType"] = ev.Info.HostKeyType
					errMsg["trustToken"] = ev.Info.HostKeyToken
					errMsg["hostId"] = ev.Info.HostID
					errMsg["sessionId"] = ev.Info.ID
				}
				_ = ws.writeJSON(errMsg)
			}
			_ = ws.writeJSON(msg)
		}
	}()

	for {
		mt, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(wsPongWait))
		if mt == websocket.TextMessage {
			var ctl struct {
				Type     string `json:"type"`
				Surface  string `json:"surface"`
				Command  string `json:"command"`
				Nonce    string `json:"nonce"`
				Approved bool   `json:"approved"`
				Cols     int    `json:"cols"`
				Rows     int    `json:"rows"`
				TargetID uint64 `json:"targetId"`
				// Agent marks an inject as the user's Type into pane on an
				// agent proposal (agentask.go).
				Agent *agentTypeMsg `json:"agent"`
				// Suggestion marks an inject as the user's ⇥ / ⇧⇥ on an
				// in-pane suggestion (agentsuggest.go).
				Suggestion *agentInsertMsg `json:"suggestion"`
				Seq        int             `json:"seq"`
				Line       string          `json:"line"`
			}
			if json.Unmarshal(msg, &ctl) != nil {
				continue
			}
			var err error
			switch ctl.Type {
			case "resize":
				err = sess.Resize(cl.ID, ctl.Cols, ctl.Rows)
			case "request-control":
				err = sess.RequestControl(cl.ID)
			case "grant-control":
				err = sess.GrantControl(cl.ID, ctl.TargetID)
			case "take-control":
				err = sess.TakeControl(cl.ID)
			case "focus":
				err = sess.SetFocus(cl.ID, inject.Surface(ctl.Surface))
			case "inject-challenge":
				var nonce string
				nonce, err = sess.IssueInjectTicket(cl.ID, ctl.Command)
				if err == nil {
					_ = ws.writeJSON(map[string]any{"type": "inject-challenge", "nonce": nonce})
				}
			case "agent-detect":
				sug.detect(ctl.Seq, ctl.Line)
			case "agent-suggest":
				sug.suggest(ctl.Seq, ctl.Line)
			case "inject":
				if ctl.Suggestion != nil {
					// ⇥ / ⇧⇥ on a suggestion: the agent service checks the
					// suggestion, the bytes, the ⇧ rule and that this
					// client drives, and logs agent.insert, before the
					// ticket writes them (§6.1, §6.5, §6.6, §6.7). The
					// bytes end without a newline: inserted, not run.
					// agent.insert is logged only once the write happened.
					id := ctl.Suggestion.SuggestionID
					grant, aerr := s.authorizeAgentInsert(sess, cl.ID, ctl.Suggestion, ctl.Command)
					if aerr != nil {
						_ = ws.writeJSON(map[string]any{"type": "agent-insert", "ok": false, "suggestionId": id, "message": aerr.Error()})
						break
					}
					ierr := sess.InjectTicket(cl.ID, ctl.Command, ctl.Nonce)
					s.agent.InsertDone(grant, ierr)
					if ierr != nil {
						s.note("agent", "agent suggestion was not inserted: "+ierr.Error(), info.HostID, info.ID, "")
						_ = ws.writeJSON(map[string]any{"type": "agent-insert", "ok": false, "suggestionId": id, "message": "the insert expired; press ⇥ again"})
						break
					}
					_ = ws.writeJSON(map[string]any{"type": "agent-insert", "ok": true, "suggestionId": id})
					break
				}
				if ctl.Agent == nil {
					err = sess.InjectTicket(cl.ID, ctl.Command, ctl.Nonce)
					break
				}
				// An agent proposal: the agent service checks the ⇧ rule,
				// that this client drives the session and that the bytes
				// are the proposal's, and logs agent.type, before the
				// approved ticket writes them (§6.1, §6.5, §6.6, §6.7).
				if aerr := s.authorizeAgentType(sess, cl.ID, ctl.Agent, ctl.Command); aerr != nil {
					_ = ws.writeJSON(map[string]any{"type": "agent-type", "ok": false, "proposalId": ctl.Agent.ProposalID, "message": aerr.Error()})
					break
				}
				// The stream offset before the write: a runbook step's
				// result is the first command the shell reports after it.
				mark := sess.StreamMark()
				if ierr := sess.InjectTicket(cl.ID, ctl.Command, ctl.Nonce); ierr != nil {
					s.note("agent", "agent command was not typed: "+ierr.Error(), info.HostID, info.ID, "")
					_ = ws.writeJSON(map[string]any{"type": "agent-type", "ok": false, "proposalId": ctl.Agent.ProposalID, "message": "the approval expired; press Type again"})
					break
				}
				s.agent.TypeDoneAt(ctl.Agent.ConversationID, ctl.Agent.ProposalID, mark)
				_ = ws.writeJSON(map[string]any{"type": "agent-type", "ok": true, "proposalId": ctl.Agent.ProposalID})
			}
			if err != nil && !errors.Is(err, runtime.ErrReadOnly) && !errors.Is(err, inject.ErrNotApproved) && !errors.Is(err, inject.ErrNotTerminalFocus) && !errors.Is(err, inject.ErrBadTicket) {
				_ = ws.writeJSON(map[string]any{"type": "error", "message": err.Error()})
			}
			continue
		}
		// Live keystrokes stay on Write (ungated). Palette/chrome must not
		// send binary here; they use type=inject → InjectTicket (server nonce).
		if err := sess.Write(cl.ID, msg); err != nil {
			if errors.Is(err, runtime.ErrReadOnly) {
				continue
			}
			_ = ws.writeJSON(map[string]any{"type": "error", "message": err.Error()})
			return
		}
	}
}

func sessionPayload(kind, message string, info runtime.Info, you runtime.Client, fromID, targetID uint64) map[string]any {
	you.Role = runtime.RoleFollower
	if you.ID == info.DriverID {
		you.Role = runtime.RoleDriver
	} else if you.ID == info.OwnerID {
		you.Role = runtime.RoleOwner
	}
	out := map[string]any{
		"type":     kind,
		"message":  message,
		"id":       info.ID,
		"hostId":   info.HostID,
		"title":    info.Title,
		"status":   info.Status,
		"attached": info.Attached,
		"driverId": info.DriverID,
		"ownerId":  info.OwnerID,
		"you":      you,
	}
	if info.ErrorKind != "" {
		out["errorKind"] = info.ErrorKind
		out["hostname"] = info.HostKeyHost
		out["fingerprint"] = info.HostKeyFingerprint
		out["expected"] = info.HostKeyExpected
		out["keyType"] = info.HostKeyType
		out["trustToken"] = info.HostKeyToken
	}
	if fromID != 0 {
		out["fromId"] = fromID
	}
	if targetID != 0 {
		out["targetId"] = targetID
	}
	return out
}

func validateHost(h store.Host) error {
	if strings.TrimSpace(h.Name) == "" {
		return errors.New("name is required")
	}
	switch h.AuthType {
	case "local":
		return nil
	case "identity":
		if strings.TrimSpace(h.IdentityID) == "" {
			return errors.New("identity is required")
		}
		if strings.TrimSpace(h.Address) == "" {
			return errors.New("address is required")
		}
		if strings.TrimSpace(h.Username) == "" {
			return errors.New("username is required")
		}
		return nil
	case "password", "key":
		if strings.TrimSpace(h.Address) == "" {
			return errors.New("address is required")
		}
		if strings.TrimSpace(h.Username) == "" {
			return errors.New("username is required")
		}
		return nil
	default:
		return errors.New("authType must be local, password, key, or identity")
	}
}

func redactHost(h store.Host) store.Host {
	if h.Password != "" {
		h.Password = "__saved__"
	}
	if h.PrivateKey != "" {
		h.PrivateKey = "__saved__"
	}
	if h.Passphrase != "" {
		h.Passphrase = "__saved__"
	}
	return h
}

func (s *Server) note(kind, msg, hostID, sessionID, tunnelID string) {
	if s.logs == nil {
		return
	}
	s.logs.Append(applog.Entry{
		Kind:      kind,
		Message:   msg,
		HostID:    hostID,
		SessionID: sessionID,
		TunnelID:  tunnelID,
	})
}

func writeStoreErr(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrLocked) {
		writeError(w, http.StatusLocked, "vault locked")
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if errors.Is(err, store.ErrGroupCycle) || errors.Is(err, store.ErrGroupDepth) || errors.Is(err, store.ErrHostField) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/ws/") {
			log.Printf("%s %s", r.Method, r.URL.Path)
		}
		next.ServeHTTP(w, r)
	})
}
