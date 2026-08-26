package httpapi

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"

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
	web   fs.FS
}

func New(st *store.Store, web fs.FS) *Server {
	return &Server{store: st, web: web}
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
	mux.HandleFunc("GET /ws/session", s.session)
	mux.Handle("/", http.FileServer(http.FS(s.web)))
	return withLogging(mux)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	hostID := r.URL.Query().Get("hostId")
	if hostID == "" {
		writeError(w, http.StatusBadRequest, "hostId is required")
		return
	}
	host, err := s.store.GetHost(hostID)
	if err != nil {
		writeError(w, http.StatusNotFound, "host not found")
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}
	cols, rows := session.ParseSize(r.URL.Query().Get("cols"), r.URL.Query().Get("rows"))
	_ = conn.WriteJSON(map[string]string{"type": "status", "message": connectingMessage(host)})

	br, err := session.Open(host, cols, rows, s.store.CheckHostKey)
	if err != nil {
		_ = conn.WriteJSON(map[string]string{"type": "error", "message": err.Error()})
		_ = conn.Close()
		return
	}
	_ = conn.WriteJSON(map[string]string{"type": "ready", "message": "connected"})
	session.BridgeWS(conn, br)
}

func connectingMessage(h store.Host) string {
	if h.AuthType == "local" {
		return "Opening local shell…"
	}
	return "Connecting to " + h.Username + "@" + h.Address + "…"
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
