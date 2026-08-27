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
	ctx, cancel := context.WithCancel(context.Background())
	lv := &live{cancel: cancel, status: StatusConnecting}
	r.live[id] = lv
	r.mu.Unlock()

	dialer, err := r.factory(host)
	if err != nil {
		r.fail(id, err.Error())
		return Info{}, err
	}
	go r.run(ctx, tn, dialer)
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
		lv.cancel()
	}
	r.mu.Unlock()
	return nil
}

func (r *Runtime) StopAll() {
	r.mu.Lock()
	ids := make([]string, 0, len(r.live))
	for id, lv := range r.live {
		lv.cancel()
		ids = append(ids, id)
	}
	r.mu.Unlock()
	_ = ids
}

func (r *Runtime) fail(id, msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if lv, ok := r.live[id]; ok {
		lv.status = StatusError
		lv.err = msg
	} else {
		r.live[id] = &live{cancel: func() {}, status: StatusError, err: msg}
	}
}

func (r *Runtime) setReady(id, listen string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if lv, ok := r.live[id]; ok {
		lv.status = StatusReady
		lv.err = ""
		lv.listenAddr = listen
	}
}

func (r *Runtime) setStopped(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.live, id)
}

func (r *Runtime) run(ctx context.Context, tn store.Tunnel, dialer Dialer) {
	defer dialer.Close()
	defer r.setStopped(tn.ID)

	go keepalive(ctx, dialer)

	bind := net.JoinHostPort(tn.BindAddr, strconv.Itoa(tn.BindPort))
	dest := net.JoinHostPort(tn.DestHost, strconv.Itoa(tn.DestPort))

	switch tn.Kind {
	case "local":
		ln, err := net.Listen("tcp", bind)
		if err != nil {
			r.fail(tn.ID, err.Error())
			<-ctx.Done()
			return
		}
		defer ln.Close()
		r.setReady(tn.ID, ln.Addr().String())
		go func() {
			<-ctx.Done()
			_ = ln.Close()
		}()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(local net.Conn) {
				remote, err := dialer.Dial("tcp", dest)
				if err != nil {
					_ = local.Close()
					return
				}
				pipe(local, remote)
			}(c)
		}
	case "remote":
		ln, err := dialer.Listen("tcp", bind)
		if err != nil {
			r.fail(tn.ID, err.Error())
			<-ctx.Done()
			return
		}
		defer ln.Close()
		r.setReady(tn.ID, ln.Addr().String())
		go func() {
			<-ctx.Done()
			_ = ln.Close()
		}()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(remote net.Conn) {
				local, err := net.Dial("tcp", dest)
				if err != nil {
					_ = remote.Close()
					return
				}
				pipe(local, remote)
			}(c)
		}
	case "dynamic":
		ln, err := net.Listen("tcp", bind)
		if err != nil {
			r.fail(tn.ID, err.Error())
			<-ctx.Done()
			return
		}
		defer ln.Close()
		r.setReady(tn.ID, ln.Addr().String())
		go func() {
			<-ctx.Done()
			_ = ln.Close()
		}()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go serveSOCKS5(c, dialer.Dial)
		}
	default:
		r.fail(tn.ID, "unknown tunnel kind")
		<-ctx.Done()
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

func keepalive(ctx context.Context, d Dialer) {
	sd, ok := d.(sshDialer)
	if !ok {
		return
	}
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_, _, _ = sd.client.SendRequest("keepalive@openssh.com", true, nil)
		}
	}
}
