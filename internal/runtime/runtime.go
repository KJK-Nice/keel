package runtime

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"keel/internal/inject"
	"keel/internal/scrollback"
	"keel/internal/session"
	"keel/internal/shellmark"
	"keel/internal/store"
	"keel/internal/wire"
)

// Status, Role and Client are the session socket's own (internal/wire).
type (
	Status = wire.Status
	Role   = wire.Role
	Client = wire.Client
)

const (
	StatusConnecting = wire.StatusConnecting
	StatusReady      = wire.StatusReady
	StatusError      = wire.StatusError
	StatusExited     = wire.StatusExited

	RoleOwner    = wire.RoleOwner
	RoleDriver   = wire.RoleDriver
	RoleFollower = wire.RoleFollower
)

type Join struct {
	Name string
	Kind string // wire.KindWindow or wire.KindJoined
}

type Info struct {
	ID        string    `json:"id"`
	HostID    string    `json:"hostId"`
	Title     string    `json:"title"`
	Subtitle  string    `json:"subtitle"`
	Color     string    `json:"color"`
	Status    Status    `json:"status"`
	Error     string    `json:"error,omitempty"`
	Clients   int       `json:"clients"`
	Attached  []Client  `json:"attached"`
	OwnerID   uint64    `json:"ownerId"`
	DriverID  uint64    `json:"driverId"`
	Cols      int       `json:"cols"`
	Rows      int       `json:"rows"`
	CreatedAt time.Time `json:"createdAt"`
	// HostKey is the key the last connect was refused for, with the
	// store's one-time offer to trust it: zero when there is none. It is
	// sent only to the session's own clients, on the session socket.
	HostKey wire.HostKey `json:"-"`
}

// Role is id's role in the session this Info describes.
func (i Info) Role(id uint64) Role { return wire.RoleOf(id, i.DriverID, i.OwnerID) }

// EventBytes is the Kind of an Event that carries PTY output. Every other
// Kind is the type of the frame it becomes (wire.TypeStatus, …).
const EventBytes = "bytes"

type Event struct {
	Kind     string
	Bytes    []byte
	Status   Status
	Message  string
	Info     Info
	FromID   uint64
	TargetID uint64
	// Peer identifies a share joiner in "join-request" and "join-resolved".
	Peer string
}

type Opener func(host store.Host, cols, rows int) (session.Bridge, error)

var (
	ErrNotFound   = errors.New("session not found")
	ErrReadOnly   = errors.New("read-only: you are not driving")
	ErrNotOwner   = errors.New("only the session owner can do that")
	ErrNoClient   = errors.New("client is not attached")
	ErrNotAllowed = errors.New("not allowed to pass control")
	// ErrNotLocal refuses a joined peer what only a client attached in Keel
	// itself may ask: Keel typing for it, and the agent.
	ErrNotLocal = errors.New("only Keel on the session's own machine can do that")
	// ErrNotReady is Write while the session has no bridge (connecting,
	// reconnecting, failed). Live keystrokes then are dropped, not buffered:
	// buffered input would land in whatever prompt comes up first (a
	// password or host key prompt). Inject still fails with it, so an
	// approved Type is reported as not typed (fail closed).
	ErrNotReady = errors.New("session is not ready")
)

type Runtime struct {
	store    *store.Store
	opener   Opener
	mu       sync.Mutex
	sess     map[string]*Session
	backoff  time.Duration
	notifyMu sync.Mutex
	notify   func(kind, msg, hostID, sessionID string)
}

// SetNotify sets where the runtime logs session events (keel.log).
func (r *Runtime) SetNotify(fn func(kind, msg, hostID, sessionID string)) {
	r.notifyMu.Lock()
	r.notify = fn
	r.notifyMu.Unlock()
}

func (r *Runtime) note(kind, msg, hostID, sessionID string) {
	if r == nil {
		return
	}
	r.notifyMu.Lock()
	fn := r.notify
	r.notifyMu.Unlock()
	if fn != nil {
		fn(kind, msg, hostID, sessionID)
	}
}

// DefaultOpener opens a real session: a local shell, or SSH with the host
// key checked against the store.
func DefaultOpener(st *store.Store) Opener {
	return func(host store.Host, cols, rows int) (session.Bridge, error) {
		return session.Open(host, cols, rows, st.CheckHostKeyType)
	}
}

// New makes the sessions' runtime. opener opens a session's connection:
// keel serve passes DefaultOpener, a test a fake.
func New(st *store.Store, opener Opener) *Runtime {
	return &Runtime{
		store:  st,
		opener: opener,
		sess:   map[string]*Session{},
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
		marks:         shellmark.NewTracker(),
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
	info := s.Info()
	go s.runLoop()
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

type Session struct {
	rt   *Runtime
	id   string
	host store.Host
	mu   sync.Mutex
	info Info
	br   session.Bridge
	buf  *ring
	// marks reads OSC 133 / OSC 7 shell-integration marks from the output
	// (under mu, with buf). Read-only: it never writes to the PTY.
	marks *shellmark.Tracker
	// alt follows the alternate screen buffer (altscreen.go), under mu.
	alt            altScreen
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
	// startupSent is set once the host's startup command was typed, so a
	// reconnect never types it again.
	startupSent bool
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
	return wire.RoleOf(id, s.driverID, s.ownerID)
}

// Role is a client's role in the session now: never what the client says.
func (s *Session) Role(id uint64) Role {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.roleLocked(id)
}

// DrivenByJoined reports whether the session's driver is a peer who joined
// through a share link.
func (s *Session) DrivenByJoined() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.clients[s.driverID]
	return ok && isRemoteJoin(c.Client.Kind)
}

func (s *Session) Subscribe(join Join) (Client, <-chan Event, []byte) {
	return s.subscribe(join, 0, false)
}

// SubscribeSince is Subscribe with a snapshot of only the output written
// after mark (see Mark): a share viewer sees nothing from before sharing
// began.
func (s *Session) SubscribeSince(join Join, mark uint64) (Client, <-chan Event, []byte) {
	return s.subscribe(join, mark, true)
}

// Mark returns the current output position for SubscribeSince.
func (s *Session) Mark() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Mark()
}

// Announce sends ev to the clients attached in Keel itself, not to share
// viewers: it is for the host's own UI (e.g. "bob wants to watch").
func (s *Session) Announce(ev Event) {
	s.mu.Lock()
	var slow []uint64
	for id, c := range s.clients {
		if isRemoteJoin(c.Client.Kind) {
			continue
		}
		if !c.offer(ev) {
			slow = append(slow, id)
		}
	}
	s.mu.Unlock()
	for _, id := range slow {
		s.dropClient(id, true)
	}
}

func (s *Session) subscribe(join Join, mark uint64, since bool) (Client, <-chan Event, []byte) {
	s.mu.Lock()
	s.next++
	id := s.next
	name := sanitizeName(join.Name)
	kind := join.Kind
	if kind != wire.KindJoined {
		kind = wire.KindWindow
	}
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
	c := newClientConn(cl)
	s.clients[id] = c
	ch := c.ch
	snap := s.buf.Bytes()
	if since {
		snap = s.buf.BytesSince(mark)
	}
	s.mu.Unlock()
	s.emitPresence(wire.MsgJoined)
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
	c.stop()
	if s.ownerID == id {
		s.ownerID = firstLocalClientID(s.clients)
	}
	if s.driverID == id {
		s.driverID = s.ownerID
	}
	hostID := s.host.ID
	s.mu.Unlock()
	msg := wire.MsgLeft
	if slow {
		msg = "dropped (slow consumer)"
		// The name is chosen by the joining peer: quote it so it cannot
		// forge log lines.
		s.rt.note("session", fmt.Sprintf("client %q (%s) dropped: %s", c.Client.Name, c.Client.Kind, c.dropped()), hostID, s.id)
	}
	s.emitPresence(msg)
}

func isRemoteJoin(kind string) bool {
	return kind == wire.KindJoined
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
		return ErrNotReady
	}
	_, err := br.Write(p)
	return err
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
// WS inject must consume it; a client approved:true flag is not enough. A
// joined peer gets none, driving or not: Keel types only for a client
// attached in Keel itself, and with no ticket InjectTicket writes nothing.
func (s *Session) IssueInjectTicket(clientID uint64, command string) (string, error) {
	s.mu.Lock()
	c, ok := s.clients[clientID]
	if !ok {
		s.mu.Unlock()
		return "", ErrNoClient
	}
	if isRemoteJoin(c.Client.Kind) {
		s.mu.Unlock()
		return "", ErrNotLocal
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

// ReadScrollback returns PTY history for inspection. Secrets are redacted
// by default. Subscribe snapshots stay raw so the live terminal can replay.
// HTTP GET /api/sessions/{id}/scrollback must use this, never the raw ring.
func (s *Session) ReadScrollback() []byte {
	return scrollback.Read(s.buf.Bytes())
}

// maxCommandText caps the command line read for a block: the typed text
// between the B and C marks (a huge paste is cut, not read whole).
const maxCommandText = 4096

// maxAfterText caps what LastCommand reads after a finished command: the
// whole ring, so the part after the command is never cut shorter than the
// scrollback the agent would otherwise read.
const maxAfterText = 256 * 1024

// LastCommand returns the pane's latest command from its shell-integration
// marks (OSC 133), with its text read from the scrollback under the same
// mask as ReadScrollback. ok is false when the shell sent no marks, or the
// command's output has left the ring. A running command's output runs to
// the end of what was written so far. The marks are untrusted output: the
// block is something to read, never a reason to do anything.
func (s *Session) LastCommand() (shellmark.Block, bool) {
	s.mu.Lock()
	c, ok := s.marks.Last()
	s.mu.Unlock()
	if !ok {
		return shellmark.Block{}, false
	}
	end := c.OutputEnd
	if !c.Done {
		end = s.buf.Mark()
	}
	out, truncated := s.buf.Range(c.OutputStart, end)
	if truncated && len(out) == 0 {
		return shellmark.Block{}, false
	}
	blk := shellmark.Block{
		Output:    string(scrollback.Read(out)),
		Exit:      c.Exit,
		HasExit:   c.HasExit,
		Done:      c.Done,
		Abandoned: c.Abandoned,
		Cwd:       c.Cwd,
		Truncated: truncated,
	}
	if c.Done {
		// What came after the end: never hidden by a D mark, so a fake
		// end printed by a program cannot keep the rest of the pane from
		// the agent. Bounded to the last maxAfterText bytes.
		total := s.buf.Mark()
		from := c.OutputEnd
		if total > from+maxAfterText {
			from, blk.AfterTruncated = total-maxAfterText, true
		}
		after, cut := s.buf.Range(from, total)
		blk.After = string(scrollback.Read(after))
		blk.AfterTruncated = blk.AfterTruncated || cut
	}
	regionStart := c.CommandTextEnd
	if c.HasCommandStart {
		regionStart = c.CommandStart
	}
	before, _ := s.buf.Range(0, regionStart)
	blk.Before = string(scrollback.Read(before))
	if c.HasCommandStart {
		cmd, cut := s.buf.Range(c.CommandStart, c.CommandTextEnd)
		if !cut {
			if len(cmd) > maxCommandText {
				cmd = cmd[:maxCommandText]
			}
			blk.Command = string(scrollback.Read(cmd))
		}
	}
	return blk, true
}

// AltScreen reports whether the pane shows the alternate screen buffer
// now (a full-screen program such as vim, less or htop), as its output
// last set it. Like the marks it is untrusted output: a program can claim
// either state. It is only ever a reason to ask more (the approve dialog),
// never a reason to type.
func (s *Session) AltScreen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.alt.on
}

// PromptState is what keel serve knows of the pane's prompt before a
// direct type (§6 rule 13): whether its shell sends OSC 133 marks, whether
// those say it is at its prompt with no command open, and the last line of
// its output (escape sequences taken out), to look for an input prompt
// such as a password prompt. The line is read raw, not redacted (a mask
// can swallow "password:"): it is only matched in keel serve, never sent
// or logged. All of it is untrusted output.
func (s *Session) PromptState() (marks, atPrompt bool, lastLine string) {
	s.mu.Lock()
	marks, atPrompt = s.marks.Prompt()
	raw := s.buf.Bytes()
	s.mu.Unlock()
	if len(raw) > 4096 {
		raw = raw[len(raw)-4096:]
	}
	return marks, atPrompt, LastLine(raw)
}

var (
	oscRe = regexp.MustCompile(`(?:\x1b\]|\x{9d})[^\x07\x1b\x{9c}]*(?:\x07|\x1b\\|\x{9c})?`)
	csiRe = regexp.MustCompile(`(?:\x1b\[|\x{9b})[0-9;?<=>]*[ -/]*[@-~]`)
	escRe = regexp.MustCompile(`\x1b[()][0-9A-Za-z]|\x1b[@-Z\\-_]`)
)

// LastLine is the last line of terminal output as text: escape sequences
// taken out, and what a carriage return wrote over dropped.
func LastLine(out []byte) string {
	s := oscRe.ReplaceAllString(string(out), "")
	s = csiRe.ReplaceAllString(s, "")
	s = escRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndexByte(s, '\r'); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimRight(s, " \t\x07")
}

// StreamMark is the pane's output stream offset now (the scrollback ring's
// write counter). Taken right before a runbook step is typed, it lets
// CommandsAfter find the commands that step started.
func (s *Session) StreamMark() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Mark()
}

// CommandsAfter returns the command records whose output started at or
// after from (a StreamMark), oldest first; none when the shell reported
// none yet. Like every mark they are untrusted output: a runbook step
// shows its exit status as reported, never as proof.
func (s *Session) CommandsAfter(from uint64) []shellmark.Command {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []shellmark.Command
	for _, c := range s.marks.Commands() {
		if c.OutputStart >= from {
			out = append(out, c)
		}
	}
	return out
}

// maxCaptureOutput caps the output CommandOutput returns for a capture.
const maxCaptureOutput = 256 * 1024

// CommandOutput returns a finished command's output as written to the
// pane, raw (escape sequences included, no mask), between its C and D
// marks. It is read only by keel serve's own capture code for a group
// run's step (agent/service/groupvars.go), which takes one value from it
// with the step's pattern and never sends or logs the text. ok is false
// for a command still running, or one whose output left the ring or is
// longer than 256 KiB.
func (s *Session) CommandOutput(c shellmark.Command) ([]byte, bool) {
	if !c.Done || c.OutputEnd < c.OutputStart || c.OutputEnd-c.OutputStart > maxCaptureOutput {
		return nil, false
	}
	out, truncated := s.buf.Range(c.OutputStart, c.OutputEnd)
	if truncated {
		return nil, false
	}
	return out, true
}

// CommandOutput is Session.CommandOutput by session id.
func (r *Runtime) CommandOutput(id string, c shellmark.Command) ([]byte, bool) {
	s, err := r.Get(id)
	if err != nil {
		return nil, false
	}
	return s.CommandOutput(c)
}

// CommandsAfter is Session.CommandsAfter by session id.
func (r *Runtime) CommandsAfter(id string, from uint64) []shellmark.Command {
	s, err := r.Get(id)
	if err != nil {
		return nil
	}
	return s.CommandsAfter(from)
}

// LastCommand is Session.LastCommand by session id.
func (r *Runtime) LastCommand(id string) (shellmark.Block, bool) {
	s, err := r.Get(id)
	if err != nil {
		return shellmark.Block{}, false
	}
	return s.LastCommand()
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
		Kind:    wire.TypeControl,
		Message: name + " requested control",
		Info:    info,
		FromID:  from,
	})
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
	s.broadcast(Event{Kind: wire.TypeControl, Message: wire.MsgControlPassed, Info: info, FromID: from, TargetID: to})
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
	s.broadcast(Event{Kind: wire.TypeControl, Message: wire.MsgTookControl, Info: info, FromID: from, TargetID: from})
	return nil
}

func (s *Session) emitPresence(msg string) {
	info := s.Info()
	s.broadcast(Event{Kind: wire.TypePresence, Message: msg, Info: info})
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
	s.broadcast(Event{Kind: wire.TypeStatus, Status: StatusConnecting, Message: "reconnecting", Info: s.Info()})
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
			s.broadcast(Event{Kind: wire.TypeStatus, Status: StatusConnecting, Message: "reconnecting", Info: s.Info()})
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
			// Every connect uses the host as saved now. If it cannot be
			// loaded (deleted, vault locked, its jump host gone), the
			// connect fails: a stale copy would dial an old address or
			// jump host with old secrets.
			h, err := s.rt.store.HostForConnect(hostID)
			if err != nil {
				s.setConnectError(err)
				s.noteLine("Cannot connect: " + err.Error())
				s.broadcast(Event{Kind: wire.TypeStatus, Status: StatusError, Message: err.Error(), Info: s.Info()})
				s.mu.Lock()
				s.forceReconnect = false
				s.mu.Unlock()
				return
			}
			s.mu.Lock()
			s.host = h
			s.mu.Unlock()
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
				s.broadcast(Event{Kind: wire.TypeStatus, Status: StatusConnecting, Message: err.Error(), Info: s.Info()})
				if !s.backoffWait() {
					return
				}
				continue
			}
			s.setConnectError(err)
			s.broadcast(Event{Kind: wire.TypeStatus, Status: StatusError, Message: err.Error(), Info: s.Info()})
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
		// A new shell: half a mark from the old one must not join it, and
		// a command the old shell left open is abandoned.
		s.marks.Reset(s.buf.Mark())
		s.alt.Reset()
		// A driver may have resized while the bridge was connecting (an SSH
		// connect takes seconds; the client fits and sends its size as soon
		// as it attaches). Resize only recorded that size, since there was
		// no bridge, and later resizes to the same size are skipped, so the
		// PTY stayed at the size it was opened with (80x24) while xterm
		// showed the fitted grid: shells redrew lines for the wrong width.
		wantCols, wantRows := s.cols, s.rows
		s.reconnectN = 0
		s.forceReconnect = false
		retry := s.everReady
		s.everReady = true
		s.info.Status = StatusReady
		s.info.Error = ""
		s.info.HostKey = wire.HostKey{}
		s.mu.Unlock()
		if old != nil {
			_ = old.Close()
		}
		if wantCols != cols || wantRows != rows {
			_ = br.Resize(wantCols, wantRows)
		}
		if retry {
			s.noteLine("Reconnected")
		}
		info := s.Info()
		s.broadcast(Event{Kind: wire.TypeStatus, Status: StatusReady, Message: wire.MsgConnected, Info: info})
		s.sendStartupCommand(br, host)

		buf := make([]byte, 32*1024)
		dropped := false
		for {
			n, rerr := br.Read(buf)
			if n > 0 {
				s.output(append([]byte(nil), buf[:n]...))
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
			// The shell is gone: a command it left open ends here, before
			// Keel's own note ("Connection lost — reconnecting…").
			s.marks.Reset(s.buf.Mark())
			s.alt.Reset()
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
				s.broadcast(Event{Kind: wire.TypeStatus, Status: StatusConnecting, Message: "reconnecting", Info: s.Info()})
				if !force && !s.backoffWait() {
					return
				}
				break
			}
			s.setStatus(StatusExited, msg)
			s.broadcast(Event{Kind: wire.TypeStatus, Status: StatusExited, Message: msg, Info: s.Info()})
			return
		}
		if !dropped {
			return
		}
	}
}

// sendStartupCommand types the host's startup command into a session's
// first shell, as the user would: the line, then Enter (CR, what a
// terminal sends for Return). It runs once per session. A reconnect opens a
// new shell but does not type it again: the command may change state
// (cd is harmless, a deploy is not), and the user who saw the session drop
// is there to type it. Restored tabs attach to running sessions and spawn
// none, and followers only attach, so neither reaches here. The log line
// names the host only: the command may hold something the user would not
// want in keel.log. This is a user setting, not agent output (§6.1).
func (s *Session) sendStartupCommand(br session.Bridge, host store.Host) {
	cmd := host.StartupCommand
	if cmd == "" || host.IsLocal() || store.ValidateStartupCommand(cmd) != nil {
		return
	}
	s.mu.Lock()
	if s.startupSent || s.closed {
		s.mu.Unlock()
		return
	}
	s.startupSent = true
	s.mu.Unlock()
	if _, err := br.Write([]byte(cmd + "\r")); err != nil {
		s.rt.note("session", "startup command not sent to "+host.Name+": "+err.Error(), host.ID, s.id)
		return
	}
	s.rt.note("session", "startup command sent to "+host.Name, host.ID, s.id)
}

func shouldRetryConnect(err error) bool {
	var unk *store.UnknownHostError
	var mm *store.HostKeyMismatchError
	if errors.As(err, &unk) || errors.As(err, &mm) || errors.Is(err, store.ErrHostField) {
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
	s.output([]byte("\r\n\x1b[38;2;139;149;168m" + msg + "\x1b[0m\r\n"))
}

// output appends b to the scrollback ring and fans it out to the attached
// clients under s.mu, the lock Subscribe holds while it registers a client
// and copies the ring. Doing the two steps under separate locks let a
// Subscribe land between them: its snapshot already held b, and the
// broadcast then delivered b again, so the new client printed it twice.
func (s *Session) output(b []byte) {
	s.mu.Lock()
	base := s.buf.Mark()
	s.buf.Write(b)
	s.marks.Feed(b, base)
	s.alt.Feed(b)
	slow := s.fanoutLocked(Event{Kind: EventBytes, Bytes: b})
	s.mu.Unlock()
	for _, id := range slow {
		s.dropClient(id, true)
	}
}

func (s *Session) setStatus(st Status, msg string) {
	s.mu.Lock()
	s.info.Status = st
	s.info.Error = msg
	s.mu.Unlock()
}

func (s *Session) setConnectError(err error) {
	s.mu.Lock()
	s.info.Status = StatusError
	s.info.Error = err.Error()
	s.info.HostKey, _ = store.RefusedHostKey(err)
	s.mu.Unlock()
}

func (s *Session) broadcast(ev Event) {
	s.mu.Lock()
	slow := s.fanoutLocked(ev)
	s.mu.Unlock()
	for _, id := range slow {
		s.dropClient(id, true)
	}
}

// fanoutLocked offers ev to every client without blocking and returns the
// clients over their queue budget (slow consumers to drop).
func (s *Session) fanoutLocked(ev Event) []uint64 {
	var slow []uint64
	for id, c := range s.clients {
		if !c.offer(ev) {
			slow = append(slow, id)
		}
	}
	return slow
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
		c.finish()
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
	if h.IsLocal() {
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
