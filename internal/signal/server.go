package signal

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	defaultRoomTTL  = 2 * time.Minute
	defaultMaxRooms = 512
	// maxRoomAge bounds a hosted room; the host's 30-minute read deadline
	// alone let any traffic keep a room alive forever.
	maxRoomAge = 24 * time.Hour
	// maxJoiners caps peers per room: each joiner costs the host a WebRTC
	// PeerConnection.
	maxJoiners = 16
	// maxMessage bounds one signaling message (SDP offers are a few KB).
	maxMessage = 64 << 10
	// Room creation per client address: a burst, then one per interval.
	roomBurst    = 6
	roomInterval = 10 * time.Second
)

const (
	// defaultIdleTimeout closes a connection that has neither sent a message
	// nor answered a ping for this long.
	defaultIdleTimeout = 30 * time.Minute
	// defaultPingInterval keeps quiet connections (after the SDP exchange)
	// alive.
	defaultPingInterval = 5 * time.Minute
)

var upgrader = websocket.Upgrader{
	CheckOrigin: allowSignalOrigin,
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
	hostKey string
	created time.Time
	mu      sync.Mutex
	host    *peer
	joiners map[string]*peer
}

type Server struct {
	mu       sync.Mutex
	rooms    map[string]*room
	roomTTL  time.Duration
	maxRooms int
	limiter  *rateLimiter
	// idleTimeout and pingInterval: see defaultIdleTimeout and
	// defaultPingInterval. Fields so tests can shorten them.
	idleTimeout  time.Duration
	pingInterval time.Duration
	// clientKey says who a request comes from for per-address rate limiting.
	// nil disables it (the default: see SetClientIP).
	clientKey func(*http.Request) string
}

func New() *Server {
	return &Server{
		rooms:    map[string]*room{},
		roomTTL:  defaultRoomTTL,
		maxRooms: defaultMaxRooms,
		limiter:  newRateLimiter(roomBurst, roomInterval),

		idleTimeout:  defaultIdleTimeout,
		pingInterval: defaultPingInterval,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /rooms", s.createRoom)
	mux.HandleFunc("GET /ws", s.ws)
	mux.Handle("GET /join", http.HandlerFunc(s.joinPage))
	mux.Handle("GET /join.js", http.HandlerFunc(s.joinJS))
	web, err := fsSub()
	if err == nil {
		mux.Handle("GET /vendor/", http.FileServer(http.FS(web)))
	}
	mux.Handle("GET /healthz", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	mux.Handle("GET /{$}", http.HandlerFunc(s.joinPage))
	return mux
}

func (s *Server) createRoom(w http.ResponseWriter, r *http.Request) {
	if s.clientKey != nil && !s.limiter.allow(s.clientKey(r), time.Now()) {
		w.Header().Set("Retry-After", strconv.Itoa(int(roomInterval/time.Second)))
		http.Error(w, "too many rooms from this address; try again shortly", http.StatusTooManyRequests)
		return
	}
	id := newID()
	hostKey := newID()
	s.mu.Lock()
	s.sweepLocked(time.Now())
	if s.maxRooms > 0 && len(s.rooms) >= s.maxRooms {
		s.mu.Unlock()
		http.Error(w, "too many rooms", http.StatusServiceUnavailable)
		return
	}
	s.rooms[id] = &room{id: id, hostKey: hostKey, created: time.Now(), joiners: map[string]*peer{}}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	// hostKey proves the caller created the room: anyone who learns the
	// room ID from a join link could otherwise connect as its host first.
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "hostKey": hostKey})
}

func (s *Server) getRoom(id string) *room {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(time.Now())
	return s.rooms[id]
}

func (s *Server) deleteRoom(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rooms, id)
}

func (s *Server) sweepLocked(now time.Time) {
	ttl := s.roomTTL
	if ttl <= 0 {
		ttl = defaultRoomTTL
	}
	for id, rm := range s.rooms {
		rm.mu.Lock()
		idle := rm.host == nil && now.Sub(rm.created) > ttl
		rm.mu.Unlock()
		if idle {
			delete(s.rooms, id)
		}
	}
	s.limiter.sweep(now)
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
	if role == "host" && subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("key")), []byte(rm.hostKey)) != 1 {
		http.Error(w, "host key required", http.StatusForbidden)
		return
	}
	if role == "join" {
		rm.mu.Lock()
		full := len(rm.joiners) >= maxJoiners
		rm.mu.Unlock()
		if full {
			http.Error(w, "room is full", http.StatusServiceUnavailable)
			return
		}
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("signal upgrade: %v", err)
		return
	}
	conn.SetReadLimit(maxMessage)
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
		// Re-check under the lock: joiners that connected while no host was
		// there all passed the early check, as can concurrent ones.
		if len(rm.joiners) >= maxJoiners {
			rm.mu.Unlock()
			_ = conn.Close()
			return
		}
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

	expires := rm.created.Add(maxRoomAge)
	readDeadline := func() time.Time {
		d := time.Now().Add(s.idleTimeout)
		if d.After(expires) {
			return expires
		}
		return d
	}
	_ = conn.SetReadDeadline(readDeadline())
	// After the SDP exchange host and joiners send nothing, so without pings
	// a share ended after idleTimeout of signaling silence. Browsers and the
	// gorilla client answer pings automatically; each pong extends the
	// deadline, up to the room's maximum age.
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(readDeadline()) })
	stopPing := make(chan struct{})
	defer close(stopPing)
	go func() {
		t := time.NewTicker(s.pingInterval)
		defer t.Stop()
		for {
			select {
			case <-stopPing:
				return
			case <-t.C:
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
					return
				}
			}
		}
	}()
	for {
		var msg envelope
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		_ = conn.SetReadDeadline(readDeadline())
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

// allowSignalOrigin lets the join page (same origin) and the Keel desktop
// (loopback Origin, or no Origin from the Go dialer) use signaling. Other
// websites cannot open a WebSocket to the relay.
func allowSignalOrigin(r *http.Request) bool {
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
	host := u.Hostname()
	if isLoopbackHost(host) {
		return true
	}
	reqHost, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		reqHost = r.Host
	}
	return strings.EqualFold(host, reqHost)
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// SetClientIP chooses where the client address for per-address rate
// limiting of room creation comes from:
//
//	""       off (default)
//	"remote" the TCP peer — for a server reached directly, with no proxy
//	"xff:N"  the Nth X-Forwarded-For entry from the right (1 = last) — for
//	         a server behind N proxies that each append the address they saw
//
// Only the proxy's own entries can be trusted; anything to their left is
// client-supplied. A wrong choice either lets clients pick their own key
// (too far left) or puts every client behind one proxy in one bucket (too
// far right), which lets anyone block sharing for everyone. That is why it
// is off until the deployment's header chain has been checked.
func (s *Server) SetClientIP(spec string) error {
	switch {
	case spec == "":
		s.clientKey = nil
	case spec == "remote":
		s.clientKey = func(r *http.Request) string {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				host = r.RemoteAddr
			}
			return addrKey(host)
		}
	case strings.HasPrefix(spec, "xff:"):
		n, err := strconv.Atoi(strings.TrimPrefix(spec, "xff:"))
		if err != nil || n < 1 {
			return fmt.Errorf("client IP %q: want xff:N with N >= 1", spec)
		}
		s.clientKey = func(r *http.Request) string {
			var parts []string
			for _, h := range r.Header.Values("X-Forwarded-For") {
				parts = append(parts, strings.Split(h, ",")...)
			}
			if len(parts) < n {
				// Fewer hops than configured: not from the expected proxy.
				return "unknown"
			}
			return addrKey(strings.TrimSpace(parts[len(parts)-n]))
		}
	default:
		return fmt.Errorf("client IP %q: want \"\", \"remote\" or \"xff:N\"", spec)
	}
	return nil
}

// addrKey normalizes an address for rate limiting: IPv6 by /64 (one host
// usually has the whole prefix), anything unparsable as one shared key so
// junk can't create unbounded buckets.
func addrKey(host string) string {
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return "unknown"
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// newID returns 128 random bits: a room ID is the only secret in a join link.
func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte("fallback"))
	}
	return hex.EncodeToString(b)
}
