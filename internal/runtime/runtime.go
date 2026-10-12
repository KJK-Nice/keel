package runtime

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"keel/internal/inject"
	"keel/internal/scrollback"
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

type Role string

const (
	RoleOwner    Role = "owner"
	RoleDriver   Role = "driver"
	RoleFollower Role = "follower"
)

type Join struct {
	Name string
	Kind string // web | tui
}

type Client struct {
	ID         uint64    `json:"id"`
	Name       string    `json:"name"`
	Kind       string    `json:"kind"`
	Role       Role      `json:"role"`
	AttachedAt time.Time `json:"attachedAt"`
}

type Info struct {
	ID                 string    `json:"id"`
	HostID             string    `json:"hostId"`
	Title              string    `json:"title"`
	Subtitle           string    `json:"subtitle"`
	Color              string    `json:"color"`
	Status             Status    `json:"status"`
	Error              string    `json:"error,omitempty"`
	Clients            int       `json:"clients"`
	Attached           []Client  `json:"attached"`
	OwnerID            uint64    `json:"ownerId"`
	DriverID           uint64    `json:"driverId"`
	Cols               int       `json:"cols"`
	Rows               int       `json:"rows"`
	CreatedAt          time.Time `json:"createdAt"`
	ErrorKind          string    `json:"errorKind,omitempty"`
	HostKeyHost        string    `json:"hostKeyHost,omitempty"`
	HostKeyFingerprint string    `json:"hostKeyFingerprint,omitempty"`
	HostKeyExpected    string    `json:"hostKeyExpected,omitempty"`
}

type Event struct {
	Kind     string
	Bytes    []byte
	Status   Status
	Message  string
	Info     Info
	FromID   uint64
	TargetID uint64
}

type HerdEvent struct {
	Type     string `json:"type"` // snapshot, spawn, kill, update
	Sessions []Info `json:"sessions,omitempty"`
	Session  Info   `json:"session,omitempty"`
	ID       string `json:"id,omitempty"`
}

type Opener func(host store.Host, cols, rows int) (session.Bridge, error)

var (
	ErrNotFound   = errors.New("session not found")
	ErrReadOnly   = errors.New("read-only: you are not driving")
	ErrNotOwner   = errors.New("only the session owner can do that")
	ErrNoClient   = errors.New("client is not attached")
	ErrNotAllowed = errors.New("not allowed to pass control")
)

type Runtime struct {
	store    *store.Store
	opener   Opener
	mu       sync.Mutex
	sess     map[string]*Session
	herdMu   sync.Mutex
	herd     map[uint64]chan HerdEvent
	herdNext uint64
	backoff  time.Duration
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
		herd:   map[uint64]chan HerdEvent{},
	}
}

func (r *Runtime) Spawn(hostID string, cols, rows int) (Info, error) {
	host, err := r.store.HostForConnect(hostID)
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
		rt:            r,
		id:            newID(),
		host:          host,
		buf:           newRing(256 * 1024),
		clients:       map[uint64]*clientConn{},
		cols:          cols,
		rows:          rows,
		stop:          make(chan struct{}),
		kick:          make(chan struct{}, 1),
		backoff:       r.backoff,
		injectTickets: inject.NewIssuer(),
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
	go s.runLoop()
	info := s.Info()
	r.emitHerd(HerdEvent{Type: "spawn", Session: info, ID: s.id})
	return info, nil
}

func (r *Runtime) List() []Info {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Info, 0, len(r.sess))
	for _, s := range r.sess {
		out = append(out, s.Info())
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
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

// ReadScrollback returns redacted PTY history for inspection. Clients and
// later AI must use this path — not a live Subscribe snapshot or raw ring.
func (r *Runtime) ReadScrollback(id string) ([]byte, error) {
	s, err := r.Get(id)
	if err != nil {
		return nil, err
	}
	return s.ReadScrollback(), nil
}

func (r *Runtime) Reconnect(id string) (Info, error) {
	s, err := r.Get(id)
	if err != nil {
		return Info{}, err
	}
	if err := s.requestReconnect(); err != nil {
		return Info{}, err
	}
	return s.Info(), nil
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
	r.emitHerd(HerdEvent{Type: "kill", ID: id, Session: Info{ID: id}})
	return nil
}

func (r *Runtime) KillAll() {
	r.mu.Lock()
	ids := make([]string, 0, len(r.sess))
	for id := range r.sess {
		ids = append(ids, id)
	}
	r.mu.Unlock()
	for _, id := range ids {
		_ = r.Kill(id)
	}
}

func (r *Runtime) SubscribeHerd() (id uint64, events <-chan HerdEvent, snapshot []Info) {
	r.herdMu.Lock()
	r.herdNext++
	id = r.herdNext
	ch := make(chan HerdEvent, 32)
	r.herd[id] = ch
	r.herdMu.Unlock()
	return id, ch, r.List()
}

func (r *Runtime) UnsubscribeHerd(id uint64) {
	r.herdMu.Lock()
	defer r.herdMu.Unlock()
	if ch, ok := r.herd[id]; ok {
		delete(r.herd, id)
		close(ch)
	}
}

func (r *Runtime) emitHerd(ev HerdEvent) {
	r.herdMu.Lock()
	defer r.herdMu.Unlock()
	for id, ch := range r.herd {
		select {
		case ch <- ev:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- ev:
			default:
				delete(r.herd, id)
				close(ch)
			}
		}
	}
}

type clientConn struct {
	Client  Client
	ch      chan Event
	focused inject.Surface
}

type Session struct {
	rt             *Runtime
	id             string
	host           store.Host
	mu             sync.Mutex
	info           Info
	br             session.Bridge
	buf            *ring
	clients        map[uint64]*clientConn
	next           uint64
	ownerID        uint64
	driverID       uint64
	cols           int
	rows           int
	stop           chan struct{}
	kick           chan struct{}
	closed         bool
	looping        bool
	reconnectN     int
	backoff        time.Duration
	everReady      bool
	forceReconnect bool
	injectTickets  *inject.Issuer
}

func (s *Session) Info() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.infoLocked()
}

func (s *Session) infoLocked() Info {
	info := s.info
	attached := make([]Client, 0, len(s.clients))
	for _, c := range s.clients {
		cl := c.Client
		cl.Role = s.roleLocked(cl.ID)
		attached = append(attached, cl)
	}
	sort.Slice(attached, func(i, j int) bool { return attached[i].ID < attached[j].ID })
	info.Attached = attached
	info.Clients = len(attached)
	info.OwnerID = s.ownerID
	info.DriverID = s.driverID
	info.Cols = s.cols
	info.Rows = s.rows
	return info
}

func (s *Session) roleLocked(id uint64) Role {
	if id == s.driverID {
		return RoleDriver
	}
	if id == s.ownerID {
		return RoleOwner
	}
	return RoleFollower
}

func (s *Session) Subscribe(join Join) (Client, <-chan Event, []byte) {
	s.mu.Lock()
	s.next++
	id := s.next
	name := sanitizeName(join.Name)
	kind := join.Kind
	if kind != "tui" && kind != "webrtc" {
		kind = "web"
	}
	ch := make(chan Event, 512)
	cl := Client{
		ID:         id,
		Name:       name,
		Kind:       kind,
		AttachedAt: time.Now().UTC(),
	}
	if !isRemoteJoin(kind) {
		if s.ownerID == 0 {
			s.ownerID = id
		}
		if s.driverID == 0 {
			s.driverID = id
		}
	}
	cl.Role = s.roleLocked(id)
	s.clients[id] = &clientConn{Client: cl, ch: ch, focused: inject.SurfaceTerm}
	snap := s.buf.Bytes()
	s.mu.Unlock()
	s.emitPresence("joined")
	return cl, ch, snap
}

func (s *Session) Unsubscribe(id uint64) {
	s.dropClient(id, false)
}

func (s *Session) dropClient(id uint64, slow bool) {
	s.mu.Lock()
	c, ok := s.clients[id]
	if !ok {
		s.mu.Unlock()
		return
	}
	delete(s.clients, id)
	close(c.ch)
	if s.ownerID == id {
		s.ownerID = firstLocalClientID(s.clients)
	}
	if s.driverID == id {
		s.driverID = s.ownerID
	}
	s.mu.Unlock()
	msg := "left"
	if slow {
		msg = "dropped (slow consumer)"
	}
	s.emitPresence(msg)
}

func isRemoteJoin(kind string) bool {
	return kind == "webrtc"
}

func firstLocalClientID(clients map[uint64]*clientConn) uint64 {
	var best uint64
	for id, c := range clients {
		if isRemoteJoin(c.Client.Kind) {
			continue
		}
		if best == 0 || id < best {
			best = id
		}
	}
	return best
}

func (s *Session) Write(clientID uint64, p []byte) error {
	s.mu.Lock()
	if _, ok := s.clients[clientID]; !ok {
		s.mu.Unlock()
		return ErrNoClient
	}
	if s.driverID != clientID {
		s.mu.Unlock()
		return ErrReadOnly
	}
	br := s.br
	s.mu.Unlock()
	if br == nil {
		return errors.New("session is not ready")
	}
	_, err := br.Write(p)
	return err
}

// SetFocus records which UI surface owns this client's keyboard. Palette
// and chrome must not leave SurfaceTerm in place or Keystroke will type.
func (s *Session) SetFocus(clientID uint64, focused inject.Surface) error {
	switch focused {
	case inject.SurfaceTerm, inject.SurfacePalette, inject.SurfaceChrome:
	case "":
		focused = inject.SurfaceTerm
	default:
		focused = inject.SurfaceChrome
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.clients[clientID]
	if !ok {
		return ErrNoClient
	}
	c.focused = focused
	return nil
}

// Focus is the surface last set for this client. Empty/unknown clients
// report SurfaceTerm so live typing keeps working until palette or chrome opens.
func (s *Session) Focus(clientID uint64) inject.Surface {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.clients[clientID]
	if !ok || c.focused == "" {
		return inject.SurfaceTerm
	}
	return c.focused
}

// Inject writes p to the PTY only after an explicit approve. Live typing
// still uses Write. Palette/snippet inject must not call Write directly.
func (s *Session) Inject(clientID uint64, p []byte, approved inject.Approval) error {
	if err := inject.Guard(approved); err != nil {
		return err
	}
	return s.Write(clientID, p)
}

// IssueInjectTicket mints a one-shot nonce bound to this driver and command.
// WS inject must consume it; a client approved:true flag is not enough.
func (s *Session) IssueInjectTicket(clientID uint64, command string) (string, error) {
	s.mu.Lock()
	if _, ok := s.clients[clientID]; !ok {
		s.mu.Unlock()
		return "", ErrNoClient
	}
	if s.driverID != clientID {
		s.mu.Unlock()
		return "", ErrReadOnly
	}
	iss := s.injectTickets
	s.mu.Unlock()
	return iss.Issue(clientID, command)
}

// InjectTicket consumes a server-issued nonce then Injects. Replay and
// client-only approved flags fail before the PTY is touched.
func (s *Session) InjectTicket(clientID uint64, command, nonce string) error {
	s.mu.Lock()
	iss := s.injectTickets
	s.mu.Unlock()
	approved, err := iss.Consume(clientID, command, nonce)
	if err != nil {
		return err
	}
	return s.Inject(clientID, []byte(command), approved)
}

// Keystroke is live typing. Palette and other chrome must not call this
// unless the terminal owns focus; they inject through Inject after approve.
func (s *Session) Keystroke(clientID uint64, p []byte, focused inject.Surface) error {
	if err := inject.CanType(focused); err != nil {
		return err
	}
	return s.Write(clientID, p)
}

// ReadScrollback returns PTY history for inspection. Secrets are redacted
// by default. Subscribe snapshots stay raw so the live terminal can replay.
// HTTP GET /api/sessions/{id}/scrollback must use this, never the raw ring.
func (s *Session) ReadScrollback() []byte {
	return scrollback.Read(s.buf.Bytes())
}

func (s *Session) Resize(clientID uint64, cols, rows int) error {
	if cols < 1 || rows < 1 {
		return nil
	}
	s.mu.Lock()
	if s.driverID != clientID {
		s.mu.Unlock()
		return ErrReadOnly
	}
	if s.cols == cols && s.rows == rows {
		s.mu.Unlock()
		return nil
	}
	s.cols = cols
	s.rows = rows
	br := s.br
	s.mu.Unlock()
	if br == nil {
		return nil
	}
	return br.Resize(cols, rows)
}

func (s *Session) RequestControl(from uint64) error {
	s.mu.Lock()
	c, ok := s.clients[from]
	if !ok {
		s.mu.Unlock()
		return ErrNoClient
	}
	if !isRemoteJoin(c.Client.Kind) && (s.ownerID == from || s.ownerID == 0) {
		s.mu.Unlock()
		return s.TakeControl(from)
	}
	info := s.infoLocked()
	name := s.clients[from].Client.Name
	s.mu.Unlock()
	s.broadcast(Event{
		Kind:    "control",
		Message: name + " requested control",
		Info:    info,
		FromID:  from,
	})
	if s.rt != nil {
		s.rt.emitHerd(HerdEvent{Type: "update", Session: info, ID: s.id})
	}
	return nil
}

func (s *Session) GrantControl(from, to uint64) error {
	s.mu.Lock()
	if _, ok := s.clients[from]; !ok {
		s.mu.Unlock()
		return ErrNoClient
	}
	if _, ok := s.clients[to]; !ok {
		s.mu.Unlock()
		return ErrNoClient
	}
	if from != s.ownerID && from != s.driverID {
		s.mu.Unlock()
		return ErrNotAllowed
	}
	s.driverID = to
	info := s.infoLocked()
	s.mu.Unlock()
	s.broadcast(Event{Kind: "control", Message: "control passed", Info: info, FromID: from, TargetID: to})
	if s.rt != nil {
		s.rt.emitHerd(HerdEvent{Type: "update", Session: info, ID: s.id})
	}
	return nil
}

func (s *Session) TakeControl(from uint64) error {
	s.mu.Lock()
	c, ok := s.clients[from]
	if !ok {
		s.mu.Unlock()
		return ErrNoClient
	}
	if isRemoteJoin(c.Client.Kind) || (s.ownerID != 0 && from != s.ownerID) {
		s.mu.Unlock()
		return ErrNotOwner
	}
	s.driverID = from
	if s.ownerID == 0 {
		s.ownerID = from
	}
	info := s.infoLocked()
	s.mu.Unlock()
	s.broadcast(Event{Kind: "control", Message: "took control", Info: info, FromID: from, TargetID: from})
	if s.rt != nil {
		s.rt.emitHerd(HerdEvent{Type: "update", Session: info, ID: s.id})
	}
	return nil
}

func (s *Session) emitPresence(msg string) {
	info := s.Info()
	s.broadcast(Event{Kind: "presence", Message: msg, Info: info})
	if s.rt != nil {
		s.rt.emitHerd(HerdEvent{Type: "update", Session: info, ID: s.id})
	}
}

func (s *Session) requestReconnect() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrNotFound
	}
	s.forceReconnect = true
	s.reconnectN = 0
	looping := s.looping
	br := s.br
	// Keep s.buf. Reconnect replaces the PTY bridge, not scrollback.
	s.kickWaitLocked()
	s.mu.Unlock()
	if br != nil {
		_ = br.Close()
	}
	if looping {
		return nil
	}
	s.setStatus(StatusConnecting, "reconnecting")
	s.broadcast(Event{Kind: "status", Status: StatusConnecting, Message: "reconnecting", Info: s.Info()})
	go s.runLoop()
	return nil
}

func (s *Session) runLoop() {
	s.mu.Lock()
	if s.closed || s.looping {
		s.mu.Unlock()
		return
	}
	s.looping = true
	opener := s.rt.opener
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.looping = false
		restart := !s.closed && s.forceReconnect
		s.mu.Unlock()
		if restart {
			s.setStatus(StatusConnecting, "reconnecting")
			s.broadcast(Event{Kind: "status", Status: StatusConnecting, Message: "reconnecting", Info: s.Info()})
			go s.runLoop()
		}
	}()

	for {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return
		}
		cols, rows := s.cols, s.rows
		hostID := s.host.ID
		s.mu.Unlock()
		if s.rt != nil && s.rt.store != nil {
			if h, err := s.rt.store.HostForConnect(hostID); err == nil {
				s.mu.Lock()
				s.host = h
				s.mu.Unlock()
			}
		}
		s.mu.Lock()
		host := s.host
		s.mu.Unlock()

		br, err := opener(host, cols, rows)
		if err != nil {
			if host.AutoReconnect() && shouldRetryConnect(err) {
				s.setStatus(StatusConnecting, err.Error())
				s.mu.Lock()
				retryNote := "Connect failed — retrying: " + err.Error()
				if s.everReady {
					retryNote = "Reconnect failed: " + err.Error()
				}
				s.mu.Unlock()
				s.noteLine(retryNote)
				s.broadcast(Event{Kind: "status", Status: StatusConnecting, Message: err.Error(), Info: s.Info()})
				if !s.backoffWait() {
					return
				}
				continue
			}
			s.setConnectError(err)
			s.broadcast(Event{Kind: "status", Status: StatusError, Message: err.Error(), Info: s.Info()})
			s.mu.Lock()
			s.forceReconnect = false
			s.mu.Unlock()
			return
		}

		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			_ = br.Close()
			return
		}
		old := s.br
		s.br = br
		s.reconnectN = 0
		s.forceReconnect = false
		retry := s.everReady
		s.everReady = true
		s.info.Status = StatusReady
		s.info.Error = ""
		s.info.ErrorKind = ""
		s.info.HostKeyHost = ""
		s.info.HostKeyFingerprint = ""
		s.info.HostKeyExpected = ""
		s.mu.Unlock()
		if old != nil {
			_ = old.Close()
		}
		if retry {
			s.noteLine("Reconnected")
		}
		info := s.Info()
		s.broadcast(Event{Kind: "status", Status: StatusReady, Message: "connected", Info: info})
		if s.rt != nil {
			s.rt.emitHerd(HerdEvent{Type: "update", Session: info, ID: s.id})
		}

		buf := make([]byte, 32*1024)
		dropped := false
		for {
			n, rerr := br.Read(buf)
			if n > 0 {
				chunk := append([]byte(nil), buf[:n]...)
				s.buf.Write(chunk)
				s.broadcast(Event{Kind: "bytes", Bytes: chunk})
			}
			if rerr == nil {
				continue
			}
			msg := "session ended"
			if rerr != io.EOF {
				msg = rerr.Error()
			}
			s.mu.Lock()
			closed := s.closed
			same := s.br == br
			if same {
				s.br = nil
			}
			auto := s.host.AutoReconnect()
			force := s.forceReconnect
			s.forceReconnect = false
			s.mu.Unlock()
			_ = br.Close()
			if closed {
				return
			}
			kind := session.BridgeExit(br)
			if force || (auto && kind == session.ExitDrop) {
				dropped = true
				s.setStatus(StatusConnecting, "reconnecting")
				if force {
					s.noteLine("Reconnecting…")
				} else {
					s.noteLine("Connection lost — reconnecting…")
				}
				s.broadcast(Event{Kind: "status", Status: StatusConnecting, Message: "reconnecting", Info: s.Info()})
				if !force && !s.backoffWait() {
					return
				}
				break
			}
			s.setStatus(StatusExited, msg)
			s.broadcast(Event{Kind: "status", Status: StatusExited, Message: msg, Info: s.Info()})
			return
		}
		if !dropped {
			return
		}
	}
}

func shouldRetryConnect(err error) bool {
	var unk *store.UnknownHostError
	var mm *store.HostKeyMismatchError
	if errors.As(err, &unk) || errors.As(err, &mm) {
		return false
	}
	msg := err.Error()
	if strings.Contains(msg, "unable to authenticate") || strings.Contains(msg, "password is required") || strings.Contains(msg, "private key is required") || strings.Contains(msg, "parse private key") {
		return false
	}
	return true
}

func (s *Session) kickWaitLocked() {
	if s.kick == nil {
		s.kick = make(chan struct{}, 1)
	}
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// maxReconnectDelay caps the wait between reconnect attempts.
const maxReconnectDelay = 15 * time.Second

// reconnectDelay returns unit doubled n times, capped at maxReconnectDelay.
// It doubles step by step instead of shifting by n: a host that stays down
// keeps n growing, and unit<<n overflows time.Duration (negative or zero)
// after ~34 attempts, which turned the wait into a busy retry loop.
func reconnectDelay(unit time.Duration, n int) time.Duration {
	if unit <= 0 {
		unit = time.Second
	}
	if unit >= maxReconnectDelay {
		return maxReconnectDelay
	}
	d := unit
	for i := 0; i < n; i++ {
		d *= 2
		if d >= maxReconnectDelay {
			return maxReconnectDelay
		}
	}
	return d
}

func (s *Session) backoffWait() bool {
	s.mu.Lock()
	n := s.reconnectN
	s.reconnectN++
	unit := s.backoff
	stop := s.stop
	if s.kick == nil {
		s.kick = make(chan struct{}, 1)
	}
	kick := s.kick
	s.mu.Unlock()
	t := time.NewTimer(reconnectDelay(unit, n))
	defer t.Stop()
	select {
	case <-stop:
		return false
	case <-kick:
		return true
	case <-t.C:
		return true
	}
}

func (s *Session) noteLine(msg string) {
	line := []byte("\r\n\x1b[38;2;139;149;168m" + msg + "\x1b[0m\r\n")
	s.buf.Write(line)
	s.broadcast(Event{Kind: "bytes", Bytes: line})
}

func (s *Session) setStatus(st Status, msg string) {
	s.mu.Lock()
	s.info.Status = st
	s.info.Error = msg
	s.mu.Unlock()
	if s.rt != nil {
		s.rt.emitHerd(HerdEvent{Type: "update", Session: s.Info(), ID: s.id})
	}
}

func (s *Session) setConnectError(err error) {
	s.mu.Lock()
	s.info.Status = StatusError
	s.info.Error = err.Error()
	s.info.ErrorKind = ""
	s.info.HostKeyHost = ""
	s.info.HostKeyFingerprint = ""
	s.info.HostKeyExpected = ""
	var unk *store.UnknownHostError
	var mm *store.HostKeyMismatchError
	switch {
	case errors.As(err, &unk):
		s.info.ErrorKind = "unknown-host"
		s.info.HostKeyHost = unk.Host
		s.info.HostKeyFingerprint = unk.Fingerprint
	case errors.As(err, &mm):
		s.info.ErrorKind = "host-key-mismatch"
		s.info.HostKeyHost = mm.Host
		s.info.HostKeyFingerprint = mm.Got
		s.info.HostKeyExpected = mm.Expected
	}
	s.mu.Unlock()
	if s.rt != nil {
		s.rt.emitHerd(HerdEvent{Type: "update", Session: s.Info(), ID: s.id})
	}
}

func (s *Session) broadcast(ev Event) {
	s.mu.Lock()
	var slow []uint64
	for id, c := range s.clients {
		select {
		case c.ch <- ev:
		default:
			slow = append(slow, id)
		}
	}
	s.mu.Unlock()
	for _, id := range slow {
		s.dropClient(id, true)
	}
}

func (s *Session) close() {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		if s.stop != nil {
			close(s.stop)
		}
	}
	br := s.br
	s.br = nil
	for id, c := range s.clients {
		close(c.ch)
		delete(s.clients, id)
	}
	s.ownerID = 0
	s.driverID = 0
	s.mu.Unlock()
	if br != nil {
		_ = br.Close()
	}
}

func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "guest"
	}
	if utf8.RuneCountInString(name) > 32 {
		runes := []rune(name)
		name = string(runes[:32])
	}
	return name
}

func subtitle(h store.Host) string {
	if h.AuthType == "local" {
		return localSubtitle()
	}
	port := ""
	if h.Port != 0 && h.Port != 22 {
		port = fmt.Sprintf(":%d", h.Port)
	}
	return h.Username + "@" + h.Address + port
}

func localSubtitle() string {
	host, _ := os.Hostname()
	user := os.Getenv("USER")
	switch {
	case user != "" && host != "":
		return user + "@" + host
	case host != "":
		return host
	default:
		return "this machine"
	}
}

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
