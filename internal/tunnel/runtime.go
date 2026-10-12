package tunnel

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"keel/internal/session"
	"keel/internal/store"
)

type Status string

const (
	StatusStopped    Status = "stopped"
	StatusConnecting Status = "connecting"
	StatusReady      Status = "ready"
	StatusError      Status = "error"
)

type Dialer interface {
	Dial(n, addr string) (net.Conn, error)
	Listen(n, addr string) (net.Listener, error)
	Close() error
}

type Factory func(host store.Host) (Dialer, error)

type sshDialer struct {
	client *ssh.Client
}

func (d sshDialer) Dial(n, addr string) (net.Conn, error) {
	return d.client.Dial(n, addr)
}

func (d sshDialer) Listen(n, addr string) (net.Listener, error) {
	return d.client.Listen(n, addr)
}

func (d sshDialer) Close() error {
	return d.client.Close()
}

// Wait blocks until the SSH connection closes.
func (d sshDialer) Wait() error {
	return d.client.Wait()
}

// waiter is implemented by dialers that can report their connection
// closing (sshDialer). run uses it to notice a dead SSH connection: local
// and dynamic forwards otherwise keep accepting on their local listener
// and show "ready" while every Dial fails.
type waiter interface {
	Wait() error
}

func DefaultFactory(st *store.Store) Factory {
	return func(host store.Host) (Dialer, error) {
		client, err := session.SSHClient(host, st.CheckHostKey)
		if err != nil {
			return nil, err
		}
		return sshDialer{client: client}, nil
	}
}

type Info struct {
	store.Tunnel
	Status     Status `json:"status"`
	Error      string `json:"error,omitempty"`
	ListenAddr string `json:"listenAddr,omitempty"`
}

type live struct {
	cancel     context.CancelFunc
	stopped    bool // true when Stop() was called intentionally
	status     Status
	err        string
	listenAddr string
}

type Runtime struct {
	store   *store.Store
	factory Factory
	mu      sync.Mutex
	live    map[string]*live
}

func New(st *store.Store, factory Factory) *Runtime {
	if factory == nil {
		factory = DefaultFactory(st)
	}
	return &Runtime{store: st, factory: factory, live: map[string]*live{}}
}

func (r *Runtime) List() []Info {
	saved, err := r.store.ListTunnels()
	if err != nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Info, len(saved))
	for i, t := range saved {
		out[i] = Info{Tunnel: t, Status: StatusStopped}
		if lv, ok := r.live[t.ID]; ok {
			out[i].Status = lv.status
			out[i].Error = lv.err
			out[i].ListenAddr = lv.listenAddr
		}
	}
	return out
}

func (r *Runtime) Start(id string) (Info, error) {
	tn, err := r.store.GetTunnel(id)
	if err != nil {
		return Info{}, err
	}
	host, err := r.store.HostForConnect(tn.HostID)
	if err != nil {
		return Info{}, err
	}
	if host.AuthType == "local" {
		return Info{}, fmt.Errorf("port forwarding needs an SSH host")
	}

	r.mu.Lock()
	if lv, ok := r.live[id]; ok && (lv.status == StatusReady || lv.status == StatusConnecting) {
		r.mu.Unlock()
		return r.infoLocked(tn), nil
	}
	if prev, ok := r.live[id]; ok {
		// Restarting after an error: stop the previous run so its goroutine
		// and SSH connection don't outlive it.
		prev.stopped = true
		prev.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	lv := &live{cancel: cancel, status: StatusConnecting}
	r.live[id] = lv
	r.mu.Unlock()

	dialer, err := r.factory(host)
	if err != nil {
		r.fail(id, lv, err.Error())
		return Info{}, err
	}
	go r.run(ctx, lv, tn, dialer)
	return r.Get(id)
}

func (r *Runtime) Get(id string) (Info, error) {
	tn, err := r.store.GetTunnel(id)
	if err != nil {
		return Info{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.infoLocked(tn), nil
}

func (r *Runtime) infoLocked(tn store.Tunnel) Info {
	info := Info{Tunnel: tn, Status: StatusStopped}
	if lv, ok := r.live[tn.ID]; ok {
		info.Status = lv.status
		info.Error = lv.err
		info.ListenAddr = lv.listenAddr
	}
	return info
}

func (r *Runtime) Stop(id string) error {
	r.mu.Lock()
	lv, ok := r.live[id]
	if ok {
		lv.stopped = true
		lv.cancel()
	}
	r.mu.Unlock()
	return nil
}

func (r *Runtime) StopAll() {
	r.mu.Lock()
	for _, lv := range r.live {
		lv.stopped = true
		lv.cancel()
	}
	r.mu.Unlock()
}

// The state helpers below only touch r.live[id] while it still is lv: a
// previous run that exits after Start replaced it must not overwrite or
// delete the new entry.

func (r *Runtime) fail(id string, lv *live, msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.live[id] == lv {
		lv.status = StatusError
		lv.err = msg
		lv.listenAddr = ""
	}
}

func (r *Runtime) setReady(id string, lv *live, listen string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.live[id] == lv {
		lv.status = StatusReady
		lv.err = ""
		lv.listenAddr = listen
	}
}

// finish records how a run ended: removed when stopped on purpose, kept as
// an error otherwise so the UI shows why. An error already recorded (a
// failed listen) is kept; anything else is a lost connection.
func (r *Runtime) finish(id string, lv *live) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.live[id] != lv {
		return
	}
	if lv.stopped {
		delete(r.live, id)
		return
	}
	if lv.status != StatusError {
		lv.status = StatusError
		lv.err = "connection lost"
	}
	lv.listenAddr = ""
}

func (r *Runtime) run(ctx context.Context, lv *live, tn store.Tunnel, dialer Dialer) {
	// lost ends this run when the SSH connection dies, without marking it
	// stopped, so finish reports "connection lost".
	ctx, lost := context.WithCancel(ctx)
	defer lost()
	defer dialer.Close()
	defer r.finish(tn.ID, lv)

	if w, ok := dialer.(waiter); ok {
		go func() {
			_ = w.Wait()
			lost()
		}()
	}
	go keepalive(ctx, dialer, keepaliveInterval, keepaliveTimeout)

	bind := net.JoinHostPort(tn.BindAddr, strconv.Itoa(tn.BindPort))
	dest := net.JoinHostPort(tn.DestHost, strconv.Itoa(tn.DestPort))

	var (
		ln  net.Listener
		err error
	)
	switch tn.Kind {
	case "local", "dynamic":
		ln, err = net.Listen("tcp", bind)
	case "remote":
		ln, err = dialer.Listen("tcp", bind)
	default:
		err = fmt.Errorf("unknown tunnel kind %q", tn.Kind)
	}
	if err != nil {
		// Fatal for this run: report it and release the SSH connection
		// instead of parking on it until someone presses Stop.
		r.fail(tn.ID, lv, err.Error())
		return
	}
	defer ln.Close()
	r.setReady(tn.ID, lv, ln.Addr().String())
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		switch tn.Kind {
		case "local":
			go func(local net.Conn) {
				remote, err := dialer.Dial("tcp", dest)
				if err != nil {
					_ = local.Close()
					return
				}
				pipe(local, remote)
			}(c)
		case "remote":
			go func(remote net.Conn) {
				local, err := net.Dial("tcp", dest)
				if err != nil {
					_ = remote.Close()
					return
				}
				pipe(local, remote)
			}(c)
		case "dynamic":
			go serveSOCKS5(c, dialer.Dial)
		}
	}
}

func pipe(a, b net.Conn) {
	defer a.Close()
	defer b.Close()
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(a, b)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(b, a)
		done <- struct{}{}
	}()
	<-done
}

var (
	keepaliveInterval = 30 * time.Second
	keepaliveTimeout  = 15 * time.Second
)

// pinger is implemented by dialers that can probe their connection.
type pinger interface {
	Ping() error
}

func (d sshDialer) Ping() error {
	_, _, err := d.client.SendRequest("keepalive@openssh.com", true, nil)
	return err
}

// keepalive pings the connection every interval. A ping that fails or gets
// no reply within timeout closes the dialer, which ends the run through
// Wait (or through the listener and Dial errors for dialers without it).
func keepalive(ctx context.Context, d Dialer, interval, timeout time.Duration) {
	p, ok := d.(pinger)
	if !ok {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		res := make(chan error, 1)
		go func() { res <- p.Ping() }()
		select {
		case <-ctx.Done():
			return
		case err := <-res:
			if err == nil {
				continue
			}
		case <-time.After(timeout):
		}
		_ = d.Close()
		return
	}
}
