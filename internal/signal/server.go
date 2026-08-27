package signal

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type envelope struct {
	Type      string `json:"type"`
	PeerID    string `json:"peerId,omitempty"`
	Name      string `json:"name,omitempty"`
	Kind      string `json:"kind,omitempty"`
	SDP       string `json:"sdp,omitempty"`
	Candidate string `json:"candidate,omitempty"`
	SDPMid    string `json:"sdpMid,omitempty"`
	SDPLine   *int   `json:"sdpMLineIndex,omitempty"`
}

type peer struct {
	id   string
	name string
	role string
	conn *websocket.Conn
	mu   sync.Mutex
}

func (p *peer) send(v any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conn.WriteJSON(v)
}

type room struct {
	id      string
	mu      sync.Mutex
	host    *peer
	joiners map[string]*peer
}

type Server struct {
	mu    sync.Mutex
	rooms map[string]*room
}

func New() *Server {
	return &Server{rooms: map[string]*room{}}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /rooms", s.createRoom)
	mux.HandleFunc("GET /ws", s.ws)
	mux.Handle("GET /join", http.HandlerFunc(s.joinPage))
	mux.Handle("GET /join.js", http.HandlerFunc(s.joinJS))
	mux.Handle("GET /healthz", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	mux.Handle("GET /{$}", http.HandlerFunc(s.joinPage))
	return mux
}

func (s *Server) createRoom(w http.ResponseWriter, _ *http.Request) {
	id := newID()
	s.mu.Lock()
	s.rooms[id] = &room{id: id, joiners: map[string]*peer{}}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
}

func (s *Server) getRoom(id string) *room {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rooms[id]
}

func (s *Server) deleteRoom(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rooms, id)
}

func (s *Server) ws(w http.ResponseWriter, r *http.Request) {
	roomID := r.URL.Query().Get("room")
	role := r.URL.Query().Get("role")
	name := r.URL.Query().Get("name")
	if roomID == "" || (role != "host" && role != "join") {
		http.Error(w, "room and role=host|join required", http.StatusBadRequest)
		return
	}
	rm := s.getRoom(roomID)
	if rm == nil {
		http.Error(w, "room not found", http.StatusNotFound)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("signal upgrade: %v", err)
		return
	}
	p := &peer{id: newID(), name: name, role: role, conn: conn}
	if role == "host" {
		rm.mu.Lock()
		if rm.host != nil {
			rm.mu.Unlock()
			_ = conn.Close()
			return
		}
		rm.host = p
		rm.mu.Unlock()
	} else {
		deadline := time.Now().Add(8 * time.Second)
		var host *peer
		for time.Now().Before(deadline) {
			rm.mu.Lock()
			host = rm.host
			rm.mu.Unlock()
			if host != nil {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if host == nil {
			_ = conn.Close()
			return
		}
		rm.mu.Lock()
		rm.joiners[p.id] = p
		host = rm.host
		rm.mu.Unlock()
		if host == nil {
			_ = conn.Close()
			return
		}
		_ = host.send(envelope{Type: "peer", PeerID: p.id, Name: name})
	}
	defer func() {
		_ = conn.Close()
		rm.mu.Lock()
		if role == "host" {
			for _, j := range rm.joiners {
				_ = j.send(envelope{Type: "leave"})
				_ = j.conn.Close()
			}
			rm.host = nil
			rm.mu.Unlock()
			s.deleteRoom(roomID)
			return
		}
		delete(rm.joiners, p.id)
		host := rm.host
		rm.mu.Unlock()
		if host != nil {
			_ = host.send(envelope{Type: "leave", PeerID: p.id})
		}
	}()

	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Minute))
	for {
		var msg envelope
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Minute))
		if msg.Type != "signal" {
			continue
		}
		rm.mu.Lock()
		if role == "host" {
			j := rm.joiners[msg.PeerID]
			rm.mu.Unlock()
			if j != nil {
				msg.PeerID = ""
				_ = j.send(msg)
			}
			continue
		}
		host := rm.host
		rm.mu.Unlock()
		if host != nil {
			msg.PeerID = p.id
			_ = host.send(msg)
		}
	}
}

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte("fallback"))
	}
	return hex.EncodeToString(b)
}
