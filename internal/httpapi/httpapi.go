package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"keel/internal/runtime"
	"keel/internal/session"
	"keel/internal/store"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  32 * 1024,
	WriteBufferSize: 32 * 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type Server struct {
	store *store.Store
	rt    *runtime.Runtime
	web   fs.FS
	token string
}

func New(st *store.Store, rt *runtime.Runtime, web fs.FS, token string) *Server {
	return &Server{store: st, rt: rt, web: web, token: strings.TrimSpace(token)}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/hosts", s.listHosts)
	mux.HandleFunc("POST /api/hosts", s.upsertHost)
	mux.HandleFunc("PUT /api/hosts/{id}", s.updateHost)
	mux.HandleFunc("DELETE /api/hosts/{id}", s.deleteHost)
	mux.HandleFunc("GET /api/snippets", s.listSnippets)
	mux.HandleFunc("POST /api/snippets", s.upsertSnippet)
	mux.HandleFunc("PUT /api/snippets/{id}", s.updateSnippet)
	mux.HandleFunc("DELETE /api/snippets/{id}", s.deleteSnippet)
	mux.HandleFunc("GET /api/sessions", s.listSessions)
	mux.HandleFunc("POST /api/sessions", s.spawnSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", s.killSession)
	mux.HandleFunc("GET /ws/session", s.sessionWS)
	mux.HandleFunc("GET /ws/herd", s.herdWS)
	mux.Handle("/", http.FileServer(http.FS(s.web)))
	return withLogging(s.withAuth(mux))
}

func (s *Server) withAuth(next http.Handler) http.Handler {
	if s.token == "" {
		return next
	}
	token := s.token
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/ws/") {
			next.ServeHTTP(w, r)
			return
		}
		if !tokenOK(token, requestToken(r)) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requestToken(r *http.Request) string {
	if t := strings.TrimSpace(r.URL.Query().Get("token")); t != "" {
		return t
	}
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) >= len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}

func tokenOK(want, got string) bool {
	return subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"role":     "runtime",
		"sessions": s.rt.Count(),
		"pid":      os.Getpid(),
		"auth":     s.token != "",
	})
}

func (s *Server) listHosts(w http.ResponseWriter, _ *http.Request) {
	hosts := s.store.ListHosts()
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
		writeError(w, http.StatusInternalServerError, err.Error())
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
	if err := validateHost(h); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	saved, err := s.store.UpsertHost(h)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, redactHost(saved))
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
}

func (s *Server) listSnippets(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.store.ListSnippets())
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
	writeJSON(w, http.StatusCreated, info)
}

func (s *Server) killSession(w http.ResponseWriter, r *http.Request) {
	if err := s.rt.Kill(r.PathValue("id")); err != nil {
		if errors.Is(err, runtime.ErrNotFound) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
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
	case "password", "key":
		if strings.TrimSpace(h.Address) == "" {
			return errors.New("address is required")
		}
		if strings.TrimSpace(h.Username) == "" {
			return errors.New("username is required")
		}
		return nil
	default:
		return errors.New("authType must be local, password, or key")
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
