// Package sshtest runs small in-process SSH servers on loopback for tests:
// password auth, a "shell" that records what it is sent and echoes it, and
// direct-tcpip forwarding so one server can act as a jump host for
// another. It never listens on anything but 127.0.0.1.
package sshtest

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/crypto/ssh"
)

// Server is one SSH server on 127.0.0.1.
type Server struct {
	Addr     string
	Host     string
	Port     int
	User     string
	Password string
	Key      ssh.PublicKey

	ln        net.Listener
	cfg       *ssh.ServerConfig
	wg        sync.WaitGroup
	forwards  atomic.Int64
	sessions  atomic.Int64
	authOK    atomic.Int64
	mu        sync.Mutex
	shellIn   []byte
	conns     []net.Conn
	closeOnce sync.Once
}

// Options configure New. Empty User and Password default to "u" and "pw".
type Options struct {
	User     string
	Password string
	// NoForward refuses direct-tcpip channels.
	NoForward bool
	// Port is the loopback port; 0 picks a free one.
	Port int
}

// New starts a server and stops it when the test ends.
func New(t testing.TB, opts Options) *Server {
	t.Helper()
	s, err := Start(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// Start starts a server on 127.0.0.1; Close stops it.
func Start(opts Options) (*Server, error) {
	if opts.User == "" {
		opts.User = "u"
	}
	if opts.Password == "" {
		opts.Password = "pw"
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, err
	}
	s := &Server{User: opts.User, Password: opts.Password, Key: signer.PublicKey()}
	s.cfg = &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == s.User && string(pass) == s.Password {
				s.authOK.Add(1)
				return nil, nil
			}
			return nil, errors.New("bad password")
		},
	}
	s.cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(opts.Port)))
	if err != nil {
		return nil, err
	}
	s.ln = ln
	s.Addr = ln.Addr().String()
	host, port, _ := net.SplitHostPort(s.Addr)
	s.Host = host
	s.Port, _ = strconv.Atoi(port)
	s.wg.Add(1)
	go s.accept(!opts.NoForward)
	return s, nil
}

// Fingerprint is the server key's SHA256 fingerprint.
func (s *Server) Fingerprint() string { return ssh.FingerprintSHA256(s.Key) }

// Forwards counts direct-tcpip channels opened through this server.
func (s *Server) Forwards() int64 { return s.forwards.Load() }

// Sessions counts session channels opened.
func (s *Server) Sessions() int64 { return s.sessions.Load() }

// Logins counts successful authentications.
func (s *Server) Logins() int64 { return s.authOK.Load() }

// ShellInput is every byte the shells on this server were sent.
func (s *Server) ShellInput() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.shellIn)
}

// Close stops the server and drops its connections.
func (s *Server) Close() {
	s.closeOnce.Do(func() {
		_ = s.ln.Close()
		s.mu.Lock()
		for _, c := range s.conns {
			_ = c.Close()
		}
		s.mu.Unlock()
		s.wg.Wait()
	})
}

func (s *Server) accept(forward bool) {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns = append(s.conns, c)
		s.mu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.serve(c, forward)
		}()
	}
}

func (s *Server) serve(c net.Conn, forward bool) {
	defer c.Close()
	sc, chans, reqs, err := ssh.NewServerConn(c, s.cfg)
	if err != nil {
		return
	}
	defer sc.Close()
	go ssh.DiscardRequests(reqs)
	var wg sync.WaitGroup
	defer wg.Wait()
	for nc := range chans {
		switch nc.ChannelType() {
		case "session":
			ch, creqs, err := nc.Accept()
			if err != nil {
				continue
			}
			s.sessions.Add(1)
			wg.Add(1)
			go func() {
				defer wg.Done()
				s.session(ch, creqs)
			}()
		case "direct-tcpip":
			if !forward {
				_ = nc.Reject(ssh.Prohibited, "forwarding off")
				continue
			}
			addr, ok := parseDirect(nc.ExtraData())
			if !ok {
				_ = nc.Reject(ssh.ConnectionFailed, "bad payload")
				continue
			}
			out, err := net.Dial("tcp", addr)
			if err != nil {
				_ = nc.Reject(ssh.ConnectionFailed, err.Error())
				continue
			}
			ch, creqs, err := nc.Accept()
			if err != nil {
				_ = out.Close()
				continue
			}
			s.forwards.Add(1)
			go ssh.DiscardRequests(creqs)
			wg.Add(1)
			go func() {
				defer wg.Done()
				pipe(ch, out)
			}()
		default:
			_ = nc.Reject(ssh.UnknownChannelType, "no")
		}
	}
}

func pipe(ch ssh.Channel, out net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(ch, out)
		_ = ch.CloseWrite()
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(out, ch)
		if tc, ok := out.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
		done <- struct{}{}
	}()
	<-done
	_ = ch.Close()
	_ = out.Close()
	<-done
}

// session answers pty-req and shell, then echoes what it reads and records
// it, until the channel closes.
func (s *Server) session(ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()
	go func() {
		for r := range reqs {
			switch r.Type {
			case "pty-req", "shell", "window-change", "env":
				_ = r.Reply(true, nil)
			default:
				_ = r.Reply(false, nil)
			}
		}
	}()
	buf := make([]byte, 4096)
	for {
		n, err := ch.Read(buf)
		if n > 0 {
			s.mu.Lock()
			s.shellIn = append(s.shellIn, buf[:n]...)
			s.mu.Unlock()
			_, _ = ch.Write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func parseDirect(b []byte) (string, bool) {
	host, rest, ok := sshString(b)
	if !ok || len(rest) < 4 {
		return "", false
	}
	port := binary.BigEndian.Uint32(rest[:4])
	return net.JoinHostPort(host, strconv.Itoa(int(port))), true
}

func sshString(b []byte) (string, []byte, bool) {
	if len(b) < 4 {
		return "", nil, false
	}
	n := binary.BigEndian.Uint32(b[:4])
	if uint32(len(b)-4) < n {
		return "", nil, false
	}
	return string(b[4 : 4+n]), b[4+n:], true
}
