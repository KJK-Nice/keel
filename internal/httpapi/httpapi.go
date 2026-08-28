package httpapi

import (
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

	"keel/internal/applog"
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
}

func New(st *store.Store, rt *runtime.Runtime, web fs.FS, sh *share.Hub) *Server {
	return &Server{store: st, rt: rt, web: web, share: sh}
}

func (s *Server) WithTunnels(t *tunnel.Runtime) *Server {
	s.tunnels = t
	return s
}

func (s *Server) WithLogs(l *applog.Book) *Server {
	s.logs = l
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("POST /api/vault/unlock", s.vaultUnlock)
	mux.HandleFunc("POST /api/vault/setup", s.vaultSetup)
	mux.HandleFunc("POST /api/share", s.shareSession)
	mux.HandleFunc("GET /api/hosts", s.listHosts)
	mux.HandleFunc("POST /api/hosts", s.upsertHost)
	mux.HandleFunc("PUT /api/hosts/{id}", s.updateHost)
	mux.HandleFunc("DELETE /api/hosts/{id}", s.deleteHost)
	mux.HandleFunc("GET /api/snippets", s.listSnippets)
	mux.HandleFunc("POST /api/snippets", s.upsertSnippet)
	mux.HandleFunc("PUT /api/snippets/{id}", s.updateSnippet)
	mux.HandleFunc("DELETE /api/snippets/{id}", s.deleteSnippet)
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
	mux.HandleFunc("GET /api/sessions", s.listSessions)
	mux.HandleFunc("POST /api/sessions", s.spawnSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", s.killSession)
	mux.HandleFunc("GET /ws/session", s.sessionWS)
	mux.HandleFunc("GET /ws/herd", s.herdWS)
	mux.Handle("/", http.FileServer(http.FS(s.web)))
	return withLogging(withLocalOrigin(s.withVault(mux)))
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
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws/") {
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
		if r.URL.Path == "/api/health" || r.URL.Path == "/api/vault/unlock" || r.URL.Path == "/api/vault/setup" {
			next.ServeHTTP(w, r)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/ws/") {
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

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	out := map[string]any{
		"status":     "ok",
		"role":       "runtime",
		"sessions":   s.rt.Count(),
		"pid":        os.Getpid(),
		"locked":     s.store.Locked(),
		"needsSetup": s.store.NeedsSetup(),
		"signal":     s.share != nil && s.share.Enabled(),
	}
	if s.share != nil && s.share.Enabled() {
		out["signalURL"] = s.share.URL()
	}
	writeJSON(w, http.StatusOK, out)
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
	if err := validateHost(h); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	saved, err := s.store.UpsertHost(h)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, redactHost(saved))
	s.note("host", "host created: "+saved.Name, saved.ID, "", "")
}

func (s *Server) updateHost(w http.ResponseWriter, r *http.Request) {
	var h store.Host
	if err := json.NewDecoder(r.Body).Decode(&h); err != nil {
		writeError(w, http.StatusBadRequest, "invalid host payload")
		return
	}
	h.ID = r.PathValue("id")
	if err := validateHost(h); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	saved, err := s.store.UpsertHost(h)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, redactHost(saved))
	s.note("host", "host updated: "+saved.Name, saved.ID, "", "")
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

func (s *Server) upsertSnippet(w http.ResponseWriter, r *http.Request) {
	var sn store.Snippet
	if err := json.NewDecoder(r.Body).Decode(&sn); err != nil {
		writeError(w, http.StatusBadRequest, "invalid snippet payload")
		return
	}
	sn.ID = ""
	if strings.TrimSpace(sn.Name) == "" || strings.TrimSpace(sn.Command) == "" {
		writeError(w, http.StatusBadRequest, "name and command are required")
		return
	}
	saved, err := s.store.UpsertSnippet(sn)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, saved)
	s.note("snippet", "snippet created: "+saved.Name, "", "", "")
}

func (s *Server) updateSnippet(w http.ResponseWriter, r *http.Request) {
	var sn store.Snippet
	if err := json.NewDecoder(r.Body).Decode(&sn); err != nil {
		writeError(w, http.StatusBadRequest, "invalid snippet payload")
		return
	}
	sn.ID = r.PathValue("id")
	saved, err := s.store.UpsertSnippet(sn)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, saved)
	s.note("snippet", "snippet updated: "+saved.Name, "", "", "")
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.note("session", "session spawned: "+info.Title, info.HostID, info.ID, "")
	writeJSON(w, http.StatusCreated, info)
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
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Minute))
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
			_ = conn.SetReadDeadline(time.Now().Add(15 * time.Minute))
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

	go func() {
		for ev := range events {
			if ev.Kind == "bytes" {
				if err := ws.writeBinary(ev.Bytes); err != nil {
					_ = conn.Close()
					return
				}
				continue
			}
			msg := sessionPayload(ev.Kind, ev.Message, ev.Info, cl, ev.FromID, ev.TargetID)
			if ev.Kind == "status" {
				msg["status"] = ev.Status
				msg["id"] = info.ID
			}
			if ev.Status == runtime.StatusError {
				_ = ws.writeJSON(map[string]any{"type": "error", "message": ev.Message})
			}
			_ = ws.writeJSON(msg)
		}
	}()

	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Minute))
	for {
		mt, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(15 * time.Minute))
		if mt == websocket.TextMessage {
			var ctl struct {
				Type     string `json:"type"`
				Cols     int    `json:"cols"`
				Rows     int    `json:"rows"`
				TargetID uint64 `json:"targetId"`
			}
			if json.Unmarshal(msg, &ctl) != nil {
				continue
			}
			switch ctl.Type {
			case "resize":
				_ = sess.Resize(cl.ID, ctl.Cols, ctl.Rows)
			case "request-control":
				_ = sess.RequestControl(cl.ID)
			case "grant-control":
				_ = sess.GrantControl(cl.ID, ctl.TargetID)
			case "take-control":
				_ = sess.TakeControl(cl.ID)
			}
			continue
		}
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
		"title":    info.Title,
		"status":   info.Status,
		"attached": info.Attached,
		"driverId": info.DriverID,
		"ownerId":  info.OwnerID,
		"you":      you,
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
