package httpapi

import (
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
}

func New(st *store.Store, rt *runtime.Runtime, web fs.FS) *Server {
	return &Server{store: st, rt: rt, web: web}
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
	mux.Handle("/", http.FileServer(http.FS(s.web)))
	return withLogging(mux)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"role":     "runtime",
		"sessions": s.rt.Count(),
		"pid":      os.Getpid(),
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

func (s *Server) sessionWS(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		id = r.URL.Query().Get("sessionId")
	}
	cols, rows := session.ParseSize(r.URL.Query().Get("cols"), r.URL.Query().Get("rows"))

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
	_ = sess.Resize(cols, rows)
	subID, events, snap := sess.Subscribe()
	defer sess.Unsubscribe(subID)
	if len(snap) > 0 {
		_ = ws.writeBinary(snap)
	}
	_ = ws.writeJSON(map[string]any{
		"type":    "ready",
		"message": "attached",
		"id":      info.ID,
		"status":  sess.Info().Status,
		"title":   info.Title,
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range events {
			if ev.Kind == "bytes" {
				if err := ws.writeBinary(ev.Bytes); err != nil {
					_ = conn.Close()
					return
				}
				continue
			}
			_ = ws.writeJSON(map[string]any{
				"type":    "status",
				"message": ev.Message,
				"status":  ev.Status,
				"id":      info.ID,
			})
			if ev.Status == runtime.StatusError {
				_ = ws.writeJSON(map[string]any{"type": "error", "message": ev.Message})
			}
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
				Type string `json:"type"`
				Cols int    `json:"cols"`
				Rows int    `json:"rows"`
			}
			if json.Unmarshal(msg, &ctl) == nil && ctl.Type == "resize" {
				_ = sess.Resize(ctl.Cols, ctl.Rows)
			}
			continue
		}
		if err := sess.Write(msg); err != nil {
			_ = ws.writeJSON(map[string]any{"type": "error", "message": err.Error()})
			return
		}
	}
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
