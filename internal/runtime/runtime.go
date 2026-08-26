package runtime

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"keel/internal/session"
	"keel/internal/store"
)

type Status string

const (
	StatusConnecting Status = "connecting"
	StatusReady      Status = "ready"
	StatusError      Status = "error"
	StatusExited     Status = "exited"
)

type Info struct {
	ID        string    `json:"id"`
	HostID    string    `json:"hostId"`
	Title     string    `json:"title"`
	Subtitle  string    `json:"subtitle"`
	Color     string    `json:"color"`
	Status    Status    `json:"status"`
	Error     string    `json:"error,omitempty"`
	Clients   int       `json:"clients"`
	Cols      int       `json:"cols"`
	Rows      int       `json:"rows"`
	CreatedAt time.Time `json:"createdAt"`
}

type Event struct {
	Kind    string
	Bytes   []byte
	Status  Status
	Message string
}

type Opener func(host store.Host, cols, rows int) (session.Bridge, error)

type Runtime struct {
	store  *store.Store
	opener Opener
	mu     sync.Mutex
	sess   map[string]*Session
}

func New(st *store.Store, opener Opener) *Runtime {
	if opener == nil {
		opener = func(host store.Host, cols, rows int) (session.Bridge, error) {
			return session.Open(host, cols, rows, st.CheckHostKey)
		}
	}
	return &Runtime{
		store:  st,
		opener: opener,
		sess:   map[string]*Session{},
	}
}

func (r *Runtime) Spawn(hostID string, cols, rows int) (Info, error) {
	host, err := r.store.GetHost(hostID)
	if err != nil {
		return Info{}, err
	}
	if cols < 1 {
		cols = 80
	}
	if rows < 1 {
		rows = 24
	}
	s := &Session{
		id:      newID(),
		host:    host,
		buf:     newRing(256 * 1024),
		clients: map[uint64]chan Event{},
		cols:    cols,
		rows:    rows,
		info: Info{
			HostID:    host.ID,
			Title:     host.Name,
			Subtitle:  subtitle(host),
			Color:     host.Color,
			Status:    StatusConnecting,
			Cols:      cols,
			Rows:      rows,
			CreatedAt: time.Now().UTC(),
		},
	}
	s.info.ID = s.id
	r.mu.Lock()
	r.sess[s.id] = s
	r.mu.Unlock()
	go s.connect(r.opener, cols, rows)
	return s.Info(), nil
}

func (r *Runtime) List() []Info {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Info, 0, len(r.sess))
	for _, s := range r.sess {
		out = append(out, s.Info())
	}
	return out
}

func (r *Runtime) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sess)
}

func (r *Runtime) Get(id string) (*Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sess[id]
	if !ok {
		return nil, ErrNotFound
	}
	return s, nil
}

func (r *Runtime) Kill(id string) error {
	r.mu.Lock()
	s, ok := r.sess[id]
	if !ok {
		r.mu.Unlock()
		return ErrNotFound
	}
	delete(r.sess, id)
	r.mu.Unlock()
	s.close()
	return nil
}

type Session struct {
	id      string
	host    store.Host
	mu      sync.Mutex
	info    Info
	br      session.Bridge
	buf     *ring
	clients map[uint64]chan Event
	next    uint64
	cols    int
	rows    int
}

func (s *Session) Info() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	info := s.info
	info.Clients = len(s.clients)
	info.Cols = s.cols
	info.Rows = s.rows
	return info
}

func (s *Session) Subscribe() (id uint64, events <-chan Event, snapshot []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	id = s.next
	ch := make(chan Event, 128)
	s.clients[id] = ch
	s.info.Clients = len(s.clients)
	return id, ch, s.buf.Bytes()
}

func (s *Session) Unsubscribe(id uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch, ok := s.clients[id]; ok {
		delete(s.clients, id)
		close(ch)
	}
	s.info.Clients = len(s.clients)
}

func (s *Session) Write(p []byte) error {
	s.mu.Lock()
	br := s.br
	s.mu.Unlock()
	if br == nil {
		return errors.New("session is not ready")
	}
	_, err := br.Write(p)
	return err
}

func (s *Session) Resize(cols, rows int) error {
	if cols < 1 || rows < 1 {
		return nil
	}
	s.mu.Lock()
	s.cols = cols
	s.rows = rows
	br := s.br
	s.mu.Unlock()
	if br == nil {
		return nil
	}
	return br.Resize(cols, rows)
}

func (s *Session) connect(opener Opener, cols, rows int) {
	br, err := opener(s.host, cols, rows)
	if err != nil {
		s.setStatus(StatusError, err.Error())
		s.broadcast(Event{Kind: "status", Status: StatusError, Message: err.Error()})
		return
	}
	s.mu.Lock()
	s.br = br
	s.info.Status = StatusReady
	s.info.Error = ""
	s.mu.Unlock()
	s.broadcast(Event{Kind: "status", Status: StatusReady, Message: "connected"})

	buf := make([]byte, 32*1024)
	for {
		n, err := br.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			s.buf.Write(chunk)
			s.broadcast(Event{Kind: "bytes", Bytes: chunk})
		}
		if err != nil {
			msg := "session ended"
			if err != io.EOF {
				msg = err.Error()
			}
			s.setStatus(StatusExited, msg)
			s.broadcast(Event{Kind: "status", Status: StatusExited, Message: msg})
			return
		}
	}
}

func (s *Session) setStatus(st Status, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.info.Status = st
	s.info.Error = msg
}

func (s *Session) broadcast(ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.clients {
		select {
		case ch <- ev:
		default:
		}
	}
}

func (s *Session) close() {
	s.mu.Lock()
	br := s.br
	s.br = nil
	for id, ch := range s.clients {
		close(ch)
		delete(s.clients, id)
	}
	s.mu.Unlock()
	if br != nil {
		_ = br.Close()
	}
}

func subtitle(h store.Host) string {
	if h.AuthType == "local" {
		return "this machine"
	}
	port := ""
	if h.Port != 0 && h.Port != 22 {
		port = fmt.Sprintf(":%d", h.Port)
	}
	return h.Username + "@" + h.Address + port
}

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
