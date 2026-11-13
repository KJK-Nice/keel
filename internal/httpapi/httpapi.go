package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"keel/internal/agent/service"
	"keel/internal/applog"
	"keel/internal/authtoken"
	"keel/internal/runtime"
	"keel/internal/session"
	"keel/internal/sessionsock"
	"keel/internal/share"
	"keel/internal/store"
	"keel/internal/tunnel"
	"keel/internal/wire"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  32 * 1024,
	WriteBufferSize: 32 * 1024,
	CheckOrigin:     allowLocalOrigin,
}

type Server struct {
	store   *store.Store
	rt      *runtime.Runtime
	share   *share.Hub
	tunnels *tunnel.Runtime
	logs    *applog.Book
	onStop  func()
	// started, addr and dataDir describe this keel serve for GET /api/runtime.
	started time.Time
	addr    string
	dataDir string
	// authToken is required on privileged routes (auth.go).
	authToken string
	build     string
	// mcpToken is keel mcp's token: it alone opens /api/mcp/ (auth.go).
	mcpToken string
	// sshDir is ~/.ssh (KEEL_SSH_DIR), read by the first-run host import.
	sshDir string
	// The agent panel (agentask.go). agent is nil without a log.
	agent        *service.Service
	agentFactory service.Factory
	agentTest    bool
	// agentJudge builds the detector (Jev) for in-pane suggestions.
	agentJudge        service.JudgeFactory
	agentDetectWindow time.Duration
	// agentSuggestTimeout bounds the model call of one in-pane suggestion; 0 is the service's default.
	agentSuggestTimeout time.Duration
	// agentOff says why agent is nil, for the routes that need it.
	agentOff string
	// closed is closed by Close: the agent's retention timer stops on it.
	closed    chan struct{}
	closeOnce sync.Once
	// anytime holds the paths that answer while the vault is locked, as
	// Handler registered them.
	anytime map[string]bool
	// vault counts store and tunnel changes: GET /api/vault reports it as "rev" (vaultrev.go).
	vault *vaultRev
	// sockets counts the open session sockets and the work they start
	// off their read loop (suggestions, direct types), which
	// can still be logging after the socket closes (waitSockets).
	sockets sync.WaitGroup
}

func New(st *store.Store, rt *runtime.Runtime, sh *share.Hub) *Server {
	s := &Server{store: st, rt: rt, share: sh, started: time.Now(), vault: newVaultRev(), closed: make(chan struct{})}
	if st != nil {
		st.OnChange(s.vault.bump)
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
		t.OnChange(s.vault.bump)
	}
	return s
}

func (s *Server) WithLogs(l *applog.Book) *Server {
	s.logs = l
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

// Handler is the API. It panics when no token was set (WithAuth): keel
// serve never runs open.
func (s *Server) Handler() http.Handler {
	if s.authToken == "" {
		panic("httpapi: Handler needs WithAuth: there is no server without a token")
	}
	s.initAgent()
	mux := http.NewServeMux()
	// anytime registers a route that answers while the vault is locked;
	// withVault refuses every other one then. agentRoute registers one that
	// needs the agent service and answers 503 with the reason without it.
	s.anytime = map[string]bool{}
	anytime := func(pattern string, h http.HandlerFunc) {
		_, path, _ := strings.Cut(pattern, " ")
		s.anytime[path] = true
		mux.HandleFunc(pattern, h)
	}
	agentRoute := func(pattern string, h http.HandlerFunc) { mux.HandleFunc(pattern, s.withAgent(h)) }
	anytime("GET /api/health", s.health)
	mux.HandleFunc("GET /api/runtime", s.runtimeInfo)
	anytime("POST /api/runtime/stop", s.stopRuntime)
	anytime("POST /api/vault/unlock", s.vaultUnlock)
	anytime("POST /api/vault/setup", s.vaultSetup)
	anytime("GET /api/vault", s.vaultState)
	mux.HandleFunc("POST /api/first-run/done", s.finishFirstRun)
	mux.HandleFunc("GET /api/ssh-config/preview", s.sshConfigPreview)
	mux.HandleFunc("POST /api/ssh-config/import", s.sshConfigImport)
	mux.HandleFunc("POST /api/share", s.shareSession)
	mux.HandleFunc("PUT /api/share/signal", s.putSignal)
	mux.HandleFunc("DELETE /api/share/{id}", s.stopShare)
	mux.HandleFunc("POST /api/share/{id}/admit", s.admitShare)
	mux.HandleFunc("GET /api/hosts", s.listHosts)
	mux.HandleFunc("POST /api/hosts", s.upsertHost)
	mux.HandleFunc("PUT /api/hosts/{id}", s.upsertHost)
	mux.HandleFunc("POST /api/hosts/{id}/duplicate", s.duplicateHost)
	mux.HandleFunc("PUT /api/hosts/{id}/agent-readable", s.setHostAgentReadable)
	mux.HandleFunc("POST /api/hosts/move", s.moveHosts)
	mux.HandleFunc("POST /api/hosts/test", s.testHost)
	mux.HandleFunc("GET /api/hosts/{id}/host-key", s.getHostKey)
	mux.HandleFunc("DELETE /api/hosts/{id}/host-key", s.forgetHostKey)
	mux.HandleFunc("DELETE /api/hosts/{id}", s.deleteHost)
	mux.HandleFunc("GET /api/groups", s.listGroups)
	mux.HandleFunc("POST /api/groups", s.upsertGroup)
	mux.HandleFunc("PUT /api/groups/{id}", s.upsertGroup)
	mux.HandleFunc("DELETE /api/groups/{id}", s.deleteGroup)
	mux.HandleFunc("GET /api/groups/{id}/members", s.groupMembers)
	mux.HandleFunc("PUT /api/groups/{id}/roles", s.setGroupRole)
	agentRoute("PUT /api/groups/{id}/vars", s.setGroupVar)
	mux.HandleFunc("GET /api/groups/{id}/ansible", s.groupAnsible)
	mux.HandleFunc("GET /api/snippets", s.listSnippets)
	mux.HandleFunc("POST /api/snippets", s.upsertSnippet)
	mux.HandleFunc("PUT /api/snippets/{id}", s.upsertSnippet)
	mux.HandleFunc("DELETE /api/snippets/{id}", s.deleteSnippet)
	mux.HandleFunc("GET /api/notebooks", s.listNotebooks)
	mux.HandleFunc("POST /api/notebooks", s.upsertNotebook)
	mux.HandleFunc("PUT /api/notebooks/{id}", s.updateNotebook)
	mux.HandleFunc("DELETE /api/notebooks/{id}", s.deleteNotebook)
	mux.HandleFunc("PATCH /api/notebooks/{id}", s.patchNotebook)
	mux.HandleFunc("PUT /api/notebooks/{id}/private", s.setNotebookPrivate)
	mux.HandleFunc("DELETE /api/notebooks/{id}/history/{vid}", s.purgeNotebookVersion)
	mux.HandleFunc("GET /api/notebook-trash/{vid}", s.deletedNotebook)
	mux.HandleFunc("DELETE /api/notebook-trash/{vid}", s.purgeDeletedNotebook)
	mux.HandleFunc("PUT /api/notebooks/{id}/reviewed", s.markNotebookReviewed)
	mux.HandleFunc("GET /api/notebooks/{id}/history", s.notebookHistory)
	mux.HandleFunc("GET /api/notebooks/{id}/history/{vid}", s.notebookVersion)
	mux.HandleFunc("POST /api/notebooks/{id}/history/{vid}/restore", s.restoreNotebookVersion)
	mux.HandleFunc("GET /api/notebook-trash", s.deletedNotebooks)
	mux.HandleFunc("POST /api/notebook-trash/{vid}/restore", s.restoreDeletedNotebook)
	mux.HandleFunc("GET /api/identities", s.listIdentities)
	mux.HandleFunc("POST /api/identities", s.upsertIdentity)
	mux.HandleFunc("PUT /api/identities/{id}", s.upsertIdentity)
	mux.HandleFunc("DELETE /api/identities/{id}", s.deleteIdentity)
	mux.HandleFunc("GET /api/tunnels", s.listTunnels)
	mux.HandleFunc("POST /api/tunnels", s.upsertTunnel)
	mux.HandleFunc("PUT /api/tunnels/{id}", s.upsertTunnel)
	mux.HandleFunc("DELETE /api/tunnels/{id}", s.deleteTunnel)
	mux.HandleFunc("POST /api/tunnels/{id}/start", s.startTunnel)
	mux.HandleFunc("POST /api/tunnels/{id}/stop", s.stopTunnel)
	mux.HandleFunc("GET /api/logs", s.listLogs)
	mux.HandleFunc("GET /api/agent/settings", s.getAgentSettings)
	mux.HandleFunc("PUT /api/agent/settings", s.putAgentSettings)
	mux.HandleFunc("GET /api/agent/providers", s.listAgentProviders)
	mux.HandleFunc("PUT /api/agent/keys/{provider}", s.putAgentKey)
	mux.HandleFunc("DELETE /api/agent/keys/{provider}", s.deleteAgentKey)
	agentRoute("POST /api/agent/ask", s.agentAsk)
	agentRoute("GET /api/mcp/panes", s.mcpPanes)
	agentRoute("POST /api/mcp/read", s.mcpRead)
	agentRoute("POST /api/mcp/propose", s.mcpPropose)
	agentRoute("GET /api/mcp/proposals/{id}", s.mcpProposal)
	agentRoute("PUT /api/agent/conversations/{id}/proposals/{pid}", s.agentEditProposal)
	mux.HandleFunc("GET /api/agent/conversations", s.agentSessions)
	mux.HandleFunc("GET /api/agent/conversations/{id}", s.agentTranscript)
	agentRoute("PATCH /api/agent/conversations/{id}", s.agentRename)
	agentRoute("POST /api/agent/conversations/{id}/proposals/{pid}/again", s.agentProposeAgain)
	mux.HandleFunc("DELETE /api/agent/conversations/{id}", s.agentReset)
	mux.HandleFunc("DELETE /api/agent/conversations", s.agentResetAll)
	agentRoute("POST /api/agent/conversations/{id}/notes/{nid}", s.agentSaveNoteProposal)
	agentRoute("DELETE /api/agent/conversations/{id}/notes/{nid}", s.agentDismissNoteProposal)
	agentRoute("POST /api/agent/conversations/{id}/notes/{nid}/undo", s.agentUndoNoteProposal)
	mux.HandleFunc("GET /api/agent/notes", s.agentNotes)
	agentRoute("POST /api/agent/notes", s.agentAddNote)
	agentRoute("PUT /api/agent/notes/{id}", s.agentEditNote)
	agentRoute("DELETE /api/agent/notes/{id}", s.agentDeleteNote)
	agentRoute("DELETE /api/agent/notes", s.agentDeleteAllNotes)
	mux.HandleFunc("GET /api/agent/runbooks", s.agentRunbooks)
	agentRoute("POST /api/agent/runbooks", s.agentAddRunbook)
	agentRoute("POST /api/agent/runbooks/draft", s.agentRunbookDraft)
	agentRoute("PUT /api/agent/runbooks/{id}", s.agentUpdateRunbook)
	agentRoute("PUT /api/agent/runbooks/{id}/pick", s.agentRunbookPick)
	agentRoute("DELETE /api/agent/runbooks/{id}", s.agentDeleteRunbook)
	agentRoute("POST /api/agent/conversations/{id}/runbooks/{rid}", s.agentSaveRunbookProposal)
	agentRoute("DELETE /api/agent/conversations/{id}/runbooks/{rid}", s.agentDismissRunbookProposal)
	agentRoute("POST /api/agent/conversations/{id}/runbooks/{rid}/undo", s.agentUndoRunbookProposal)
	agentRoute("POST /api/agent/conversations/{id}/notebooks/{nid}/undo", s.agentUndoNotebookChange)
	agentRoute("POST /api/agent/conversations/{id}/notebooks/{nid}/delete", s.agentConfirmNotebookDelete)
	agentRoute("DELETE /api/agent/conversations/{id}/notebooks/{nid}", s.agentDismissNotebookProposal)
	agentRoute("POST /api/agent/conversations/{id}/notebooks/{nid}/restore", s.agentRestoreNotebookChange)
	mux.HandleFunc("GET /api/agent/conversations/{id}/runbook", s.agentRunState)
	agentRoute("POST /api/agent/conversations/{id}/runbook/steps/{sid}", s.agentStepAction)
	agentRoute("DELETE /api/agent/conversations/{id}/runbook", s.agentStopRunbook)
	agentRoute("POST /api/agent/conversations/{id}/runbook/move", s.agentMoveRunbook)
	agentRoute("POST /api/agent/groupruns", s.agentStartGroupRun)
	agentRoute("GET /api/agent/groupruns/{id}", s.agentGroupRun)
	agentRoute("POST /api/agent/groupruns/{id}/propose", s.agentProposeGroupStep)
	agentRoute("POST /api/agent/groupruns/{id}/fill", s.agentFillGroupRun)
	agentRoute("POST /api/agent/groupruns/{id}/steps/{sid}", s.agentGroupHostAction)
	agentRoute("PUT /api/agent/groupruns/{id}/mode", s.agentGroupRunMode)
	agentRoute("DELETE /api/agent/groupruns/{id}", s.agentStopGroupRun)
	mux.HandleFunc("POST /api/known-hosts", s.trustHostKey)
	mux.HandleFunc("GET /api/sessions", s.listSessions)
	mux.HandleFunc("GET /api/sessions/{id}/scrollback", s.readScrollback)
	mux.HandleFunc("POST /api/sessions/{id}/reconnect", s.reconnectSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", s.killSession)
	mux.HandleFunc("GET /ws/session", s.sessionWS)
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
	if !store.IsLoopbackAddr(u.Hostname()) {
		return false
	}
	return originHostPort(u) == requestHostPort(r)
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
		if apiPath(r.URL.Path) && !allowLocalOrigin(r) {
			writeError(w, http.StatusForbidden, "origin not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withVault(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.anytime[r.URL.Path] && apiPath(r.URL.Path) && s.store.Locked() {
			writeErr(w, store.ErrLocked)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"pid": os.Getpid()}
	if s.build != "" {
		out["build"] = s.build
	}
	if proof, ok := s.healthProof(r); ok {
		out[authtoken.WindowChallenge.Field] = proof
	}
	if proof, ok := s.mcpHealthProof(r); ok {
		out[authtoken.MCPChallenge.Field] = proof
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
	}
	for k, v := range s.signalInfo() {
		out[k] = v
	}
	writeJSON(w, http.StatusOK, out)
}

// signalInfo is the signal server for Settings · Sharing: the one in use
// (signalURL), where it comes from (signalSource: env, settings, build or
// ""), and the one saved in Settings (signalSaved), which KEEL_SIGNAL
// overrides for the run.
func (s *Server) signalInfo() map[string]any {
	return map[string]any{
		"signalURL":    s.share.URL(),
		"signalSource": s.share.Source(),
		"signalSaved":  s.store.SignalURL(),
	}
}

// putSignal saves Settings · Sharing's signal server ({"url": ""} removes
// it) and uses it for the next share. Shares already running keep theirs.
func (s *Server) putSignal(w http.ResponseWriter, r *http.Request) {
	if s.share == nil {
		writeError(w, http.StatusServiceUnavailable, "sharing is not available")
		return
	}
	var req struct {
		URL string `json:"url"`
	}
	if err := decode(w, r, bodyNote, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	u, err := share.NormalizeSignal(req.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.SetSignalURL(u); err != nil {
		writeErr(w, err)
		return
	}
	s.share.SetSaved(u)
	if u == "" {
		s.note("share", "signal server removed", "", "", "")
	} else {
		s.note("share", "signal server set to "+u, "", "", "")
	}
	writeJSON(w, http.StatusOK, s.signalInfo())
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
	if err := decode(w, r, bodyForm, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	if err := s.store.Unlock(req.Passphrase); err != nil {
		writeErr(w, otherwise(err, http.StatusBadRequest))
		return
	}
	s.note("vault", "vault unlocked", "", "", "")
	writeJSON(w, http.StatusOK, map[string]any{"locked": false})
}

func (s *Server) vaultSetup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Passphrase string `json:"passphrase"`
	}
	if err := decode(w, r, bodyForm, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	if err := s.store.Setup(req.Passphrase); err != nil {
		writeErr(w, otherwise(err, http.StatusBadRequest))
		return
	}
	s.note("vault", "vault created", "", "", "")
	writeJSON(w, http.StatusOK, map[string]any{"locked": false})
}

func (s *Server) shareSession(w http.ResponseWriter, r *http.Request) {
	if s.share == nil || !s.share.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "set a signal server in Settings · Sharing to share")
		return
	}
	var req struct {
		SessionID string `json:"sessionId"`
	}
	if err := decode(w, r, bodyForm, &req); err != nil || req.SessionID == "" {
		writeError(w, http.StatusBadRequest, "sessionId is required")
		return
	}
	join, err := s.share.Share(req.SessionID)
	if err != nil {
		writeErr(w, otherwise(err, http.StatusBadGateway))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": join})
}

// admitShare answers a share joiner's request to watch:
// {"peerId": "…", "allow": true}.
func (s *Server) admitShare(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PeerID string `json:"peerId"`
		Allow  bool   `json:"allow"`
	}
	if err := decode(w, r, bodyForm, &req); err != nil || req.PeerID == "" {
		writeError(w, http.StatusBadRequest, "peerId is required")
		return
	}
	if s.share == nil {
		writeError(w, http.StatusNotFound, "session is not shared")
		return
	}
	if err := s.share.Admit(r.PathValue("id"), req.PeerID, req.Allow); err != nil {
		// Anything else: the request expired or was already answered.
		writeErr(w, otherwise(err, http.StatusConflict))
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

func (s *Server) listHosts(w http.ResponseWriter, _ *http.Request) {
	hosts, err := s.store.ListHosts()
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]store.Host, len(hosts))
	for i, h := range hosts {
		out[i] = h.Redacted()
	}
	writeJSON(w, http.StatusOK, out)
}

// The saves of hosts, groups, snippets, identities and forwards have one
// handler each for POST (create: the id in the body is ignored, 201) and
// PUT /{id} (update: the id is the path's, 200). savedStatus picks the
// status.

func (s *Server) upsertHost(w http.ResponseWriter, r *http.Request) {
	var h store.Host
	if err := decode(w, r, bodyHost, &h); err != nil {
		writeError(w, http.StatusBadRequest, "invalid host payload")
		return
	}
	h.ID = r.PathValue("id")
	saved, err := s.saveHost(h)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, savedStatus(r.PathValue("id")), saved.Redacted())
}

func (s *Server) duplicateHost(w http.ResponseWriter, r *http.Request) {
	saved, err := s.store.DuplicateHost(r.PathValue("id"))
	if err != nil {
		writeErr(w, worded(err, store.ErrNotFound, "host not found"))
		return
	}
	writeJSON(w, http.StatusCreated, saved.Redacted())
	s.note("host", "host duplicated: "+saved.Name, saved.ID, "", "")
}

// setHostAgentReadable is a host menu's "Agent can read" (the Edit host
// switch of the same name, §5.8): it changes that one switch and keeps
// everything else of the saved host. Only the user's page calls it.
func (s *Server) setHostAgentReadable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Readable *bool `json:"readable"`
	}
	if err := decode(w, r, bodyForm, &req); err != nil || req.Readable == nil {
		writeError(w, http.StatusBadRequest, "readable is required")
		return
	}
	h, err := s.store.GetHost(r.PathValue("id"))
	if err != nil {
		writeErr(w, worded(err, store.ErrNotFound, "host not found"))
		return
	}
	v := *req.Readable
	h.AgentReadable = &v
	saved, err := s.store.UpsertHost(h)
	if err != nil {
		writeErr(w, err)
		return
	}
	state := "off"
	if v {
		state = "on"
	}
	s.note("host", "agent can read "+state+": "+saved.Name, saved.ID, "", "")
	writeJSON(w, http.StatusOK, saved.Redacted())
}

type moveHostsReq struct {
	IDs     []string `json:"ids"`
	GroupID string   `json:"groupId"`
}

func (s *Server) moveHosts(w http.ResponseWriter, r *http.Request) {
	var req moveHostsReq
	if err := decode(w, r, bodyForm, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid move payload")
		return
	}
	if err := s.store.AssignHostsGroup(req.IDs, req.GroupID); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
	s.note("group", "hosts moved", "", "", "")
}

func (s *Server) listGroups(w http.ResponseWriter, _ *http.Request) {
	groups, err := s.store.ListGroups()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, groups)
}

func (s *Server) upsertGroup(w http.ResponseWriter, r *http.Request) {
	var g store.Group
	if err := decode(w, r, bodyForm, &g); err != nil {
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
		writeErr(w, err)
		return
	}
	writeJSON(w, savedStatus(r.PathValue("id")), saved)
	if r.PathValue("id") == "" {
		s.note("group", "group created: "+saved.Name, "", "", "")
	} else {
		s.note("group", "group updated: "+saved.Name, "", "", "")
	}
}

func (s *Server) deleteGroup(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteGroup(r.PathValue("id")); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
	s.note("group", "group deleted", "", "", "")
}

func (s *Server) deleteHost(w http.ResponseWriter, r *http.Request) {
	host, err := s.store.GetHost(r.PathValue("id"))
	if err != nil {
		writeErr(w, worded(err, store.ErrNotFound, "host not found"))
		return
	}
	if host.IsLocal() {
		writeErr(w, badInput("the local shell host cannot be removed"))
		return
	}
	if err := s.store.DeleteHost(r.PathValue("id")); err != nil {
		writeErr(w, worded(err, store.ErrNotFound, "host not found"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
	s.note("host", "host deleted: "+host.Name, host.ID, "", "")
}

func (s *Server) listSnippets(w http.ResponseWriter, _ *http.Request) {
	snips, err := s.store.ListSnippets()
	if err != nil {
		writeErr(w, err)
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
	if err := decode(w, r, bodyForm, &p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid snippet payload")
		return
	}
	saved, err := s.saveSnippet(r.PathValue("id"), p.Name, p.Command, p.AgentUsable)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, savedStatus(r.PathValue("id")), saved)
}

func (s *Server) deleteSnippet(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteSnippet(r.PathValue("id")); err != nil {
		writeErr(w, worded(err, store.ErrNotFound, "snippet not found"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
	s.note("snippet", "snippet deleted", "", "", "")
}

func (s *Server) listNotebooks(w http.ResponseWriter, _ *http.Request) {
	nbs, err := s.store.ListNotebooks()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nbs)
}

func (s *Server) upsertNotebook(w http.ResponseWriter, r *http.Request) {
	var nb store.Notebook
	if err := decode(w, r, bodyBulk, &nb); err != nil {
		writeError(w, http.StatusBadRequest, "invalid notebook payload")
		return
	}
	nb.ID = ""
	saved, err := s.store.UpsertNotebook(nb)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, saved)
	s.note("notebook", "notebook created: "+saved.Name, "", "", "")
}

func (s *Server) updateNotebook(w http.ResponseWriter, r *http.Request) {
	var nb store.Notebook
	if err := decode(w, r, bodyBulk, &nb); err != nil {
		writeError(w, http.StatusBadRequest, "invalid notebook payload")
		return
	}
	nb.ID = r.PathValue("id")
	// nb.Rev is the revision the editor started from: a save over an agent
	// edit it had not loaded keeps the user's text and puts the agent's
	// version in History (store.SaveUserNotebook).
	saved, replaced, err := s.store.SaveUserNotebook(nb)
	if err != nil {
		writeErr(w, worded(err, store.ErrHistoryUnavailable, "Not saved: this notebook holds an agent edit, and Keel cannot keep that version in History right now. Your typing is still in the page."))
		return
	}
	writeJSON(w, http.StatusOK, savedNotebook{Notebook: saved, ReplacedAgentEdit: replaced})
	s.note("notebook", "notebook updated: "+saved.Name, "", "", "")
}

func (s *Server) deleteNotebook(w http.ResponseWriter, r *http.Request) {
	permanent := r.URL.Query().Get("permanent") == "1"
	if err := s.store.DeleteNotebook(r.PathValue("id"), permanent); err != nil {
		err = worded(err, store.ErrNotFound, "notebook not found")
		writeErr(w, worded(err, store.ErrHistoryUnavailable, "Keel cannot keep this notebook in Recently deleted right now. Delete it permanently?"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
	s.note("notebook", "notebook deleted", "", "", "")
}

func (s *Server) listSessions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.rt.List())
}

// readScrollback is the inspection path for clients (and later AI). It always
// returns redacted history. Live UI replay uses Subscribe snapshots on
// /ws/session, which stay raw. There is no raw= query bypass.
func (s *Server) readScrollback(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	text, err := s.rt.ReadScrollback(id)
	if err != nil {
		writeErr(w, err)
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
		writeErr(w, err)
		return
	}
	s.note("session", "session reconnect: "+info.Title, info.HostID, info.ID, "")
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) killSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.rt.Kill(id); err != nil {
		writeErr(w, err)
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

// SendJSON and SendBytes make a wsConn a sessionsock.Transport.
func (w *wsConn) SendJSON(v any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteJSON(v)
}

func (w *wsConn) SendBytes(p []byte) error {
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

// goSocket runs a session socket's work off its read loop, counted in
// s.sockets. Call it only from a session socket's handler.
func (s *Server) goSocket(fn func()) {
	s.sockets.Add(1)
	go func() {
		defer s.sockets.Done()
		fn()
	}()
}

// waitSockets waits for the session sockets and the work they started,
// such as a suggestion's Jev check logging agent.suggest after its card
// was sent. Call it after the listener is closed and the clients are gone.
func (s *Server) waitSockets() { s.sockets.Wait() }

func (s *Server) sessionWS(w http.ResponseWriter, r *http.Request) {
	// Counted before the upgrade, while the HTTP server still tracks the
	// request; the socket's own work is counted while this is held.
	s.sockets.Add(1)
	defer s.sockets.Done()
	id := r.URL.Query().Get("id")
	cols, rows := session.ParseSize(r.URL.Query().Get("cols"), r.URL.Query().Get("rows"))
	name := r.URL.Query().Get("name")

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
	_ = ws.SendJSON(wire.State{Type: wire.TypeStatus, Message: wire.MsgAttaching, ID: info.ID, Status: info.Status})
	// What the socket starts off its read loop (suggestions, direct types)
	// stops with the socket.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	// A WebSocket is the window on this machine: the one client keel serve
	// types for. The kind is the transport's, never the client's word.
	c := sessionsock.Attach(sess, ws, sessionsock.Options{
		Name: name, Kind: wire.KindWindow, Caps: sessionsock.Local, Cols: cols, Rows: rows,
		Typist: func(c *sessionsock.Conn) sessionsock.Typist { return s.newTypist(ctx, c) },
	})
	defer c.Detach()

	pingDone := make(chan struct{})
	defer close(pingDone)
	ws.startPings(pingDone)

	go func() {
		defer conn.Close()
		c.Forward()
	}()

	for {
		mt, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(wsPongWait))
		if mt == websocket.TextMessage {
			c.Control(msg)
			continue
		}
		// Live keystrokes: written for the driver only, with no approval.
		// Anything Keel types for the user is an inject frame instead.
		if c.Keys(msg) != nil {
			return
		}
	}
}

// notReadyTypeMessage reports an approved Type or insert refused because
// the session was still connecting or reconnecting: nothing was typed.
const notReadyTypeMessage = "the session is not connected; nothing was typed"

// socketTypist is how keel serve types for one window's socket and answers
// its suggestion questions (sessionsock.Typist).
type socketTypist struct {
	srv *Server
	ctx context.Context // ends with the socket
	c   *sessionsock.Conn
	// In-pane suggestions (agentsuggest.go) run off the read loop.
	sug *suggestConn
	// Direct types are bounded per socket (typedirect.go).
	directLim *directLimiter
}

func (s *Server) newTypist(ctx context.Context, c *sessionsock.Conn) *socketTypist {
	return &socketTypist{srv: s, ctx: ctx, c: c, sug: newSuggestConn(ctx, s, c), directLim: newDirectLimiter()}
}

func (t *socketTypist) Detect(seq int, line string)  { t.sug.detect(seq, line) }
func (t *socketTypist) Suggest(seq int, line string) { t.sug.suggest(seq, line) }

// Inject is an inject frame from the window: the bytes are written only by
// Session.InjectTicket, with the nonce the session issued for them, after
// the checks of whatever the frame says the bytes are.
func (t *socketTypist) Inject(ctl wire.Control) error {
	s, c := t.srv, t.c
	sess, clientID := c.Session(), c.ClientID()
	info := sess.Info()
	if ctl.Direct != nil && !t.directLim.allow("type") {
		c.Send(wire.TypeResult{Type: wire.TypeTypeDirect, Ref: ctl.Direct.Ref, Dialog: true, Reason: reasonBusy})
		return nil
	}
	if ctl.Direct != nil {
		// One click, no dialog, no Enter (§6 rule 13): keel serve checks
		// again, off the read loop (a snippet or a notebook block may be
		// rated by Jev), then writes.
		s.goSocket(func() { s.typeDirect(t.ctx, c, ctl.Command, ctl.Nonce, ctl.Direct, ctl.Agent) })
		return nil
	}
	if ctl.Suggestion != nil {
		// ⇥ / ⇧⇥ on a suggestion: the agent service checks the suggestion,
		// the bytes, the ⇧ rule and that this client drives, and logs
		// agent.insert, before the ticket writes them (§6.1, §6.5, §6.6,
		// §6.7). The bytes end without a newline: inserted, not run.
		// agent.insert is logged only once the write happened.
		out := wire.TypeResult{Type: wire.TypeAgentInsert, SuggestionID: ctl.Suggestion.SuggestionID}
		grant, aerr := s.authorizeAgentInsert(sess, clientID, ctl.Suggestion, ctl.Command)
		if aerr != nil {
			out.Message = aerr.Error()
			c.Send(out)
			return nil
		}
		ierr := sess.InjectTicket(clientID, ctl.Command, ctl.Nonce)
		s.agent.InsertDone(grant, ierr)
		if ierr != nil {
			s.note("agent", "agent suggestion was not inserted: "+ierr.Error(), info.HostID, info.ID, "")
			out.Message = wire.MsgInsertExpired
			if errors.Is(ierr, runtime.ErrNotReady) {
				out.Message = notReadyTypeMessage
			}
			c.Send(out)
			return nil
		}
		out.OK = true
		c.Send(out)
		return nil
	}
	if ctl.Notebook && ctl.Agent == nil && service.HasPlaceholder(ctl.Command) {
		// A notebook fence (an agent's draft, or text pasted from an
		// answer) that still holds a placeholder is a shell glob, never
		// the value: not typed.
		s.note("notebook", "a notebook command was not typed: it holds a redacted placeholder", info.HostID, info.ID, "")
		return service.ErrPlaceholder
	}
	if ctl.Agent == nil {
		return sess.InjectTicket(clientID, ctl.Command, ctl.Nonce)
	}
	// An agent proposal: the agent service checks the ⇧ rule, that this
	// client drives the session and that the bytes are the proposal's, and
	// logs agent.type, before the approved ticket writes them (§6.1, §6.5,
	// §6.6, §6.7).
	out := wire.TypeResult{Type: wire.TypeAgentType, ProposalID: ctl.Agent.ProposalID}
	if aerr := s.authorizeAgentType(sess, clientID, ctl.Agent, ctl.Command); aerr != nil {
		out.Message = aerr.Error()
		c.Send(out)
		return nil
	}
	// The stream offset before the write: a runbook step's result is the
	// first command the shell reports after it.
	mark := sess.StreamMark()
	if ierr := sess.InjectTicket(clientID, ctl.Command, ctl.Nonce); ierr != nil {
		if ctl.Agent.GroupRunID != "" {
			// The host goes back to proposed: nothing was written.
			s.agent.GroupTypeFailed(ctl.Agent.GroupRunID, ctl.Agent.ProposalID)
		}
		s.note("agent", "agent command was not typed: "+ierr.Error(), info.HostID, info.ID, "")
		out.Message = "the approval expired; press Type again"
		if errors.Is(ierr, runtime.ErrNotReady) {
			out.Message = notReadyTypeMessage
		}
		c.Send(out)
		return nil
	}
	if ctl.Agent.GroupRunID != "" {
		s.agent.GroupTypeDoneAt(ctl.Agent.GroupRunID, ctl.Agent.ProposalID, mark)
	} else {
		s.agent.TypeDoneAt(ctl.Agent.ConversationID, ctl.Agent.ProposalID, mark)
	}
	out.OK = true
	c.Send(out)
	return nil
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
