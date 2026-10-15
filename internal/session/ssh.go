package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"keel/internal/store"
)

type sshBridge struct {
	client  *ssh.Client
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
	stop    chan struct{}
	once    sync.Once
	waitMu  sync.Mutex
	waitErr error
}

func openSSH(host store.Host, cols, rows int, checkKey func(hostname, fingerprint string) error) (Bridge, error) {
	client, err := SSHClient(host, checkKey)
	if err != nil {
		return nil, err
	}
	sess, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ssh session: %w", err)
	}
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if cols < 1 {
		cols = 80
	}
	if rows < 1 {
		rows = 24
	}
	if err := sess.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		_ = sess.Close()
		_ = client.Close()
		return nil, fmt.Errorf("request pty: %w", err)
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		_ = sess.Close()
		_ = client.Close()
		return nil, err
	}
	pr, pw := io.Pipe()
	sess.Stdout = pw
	sess.Stderr = pw
	if err := sess.Shell(); err != nil {
		_ = pw.Close()
		_ = sess.Close()
		_ = client.Close()
		return nil, fmt.Errorf("start shell: %w", err)
	}
	br := &sshBridge{
		client:  client,
		session: sess,
		stdin:   stdin,
		stdout:  pr,
		stop:    make(chan struct{}),
	}
	go func() {
		err := sess.Wait()
		br.waitMu.Lock()
		br.waitErr = err
		br.waitMu.Unlock()
		_ = pw.CloseWithError(err)
	}()
	if keep := host.KeepAlive(); keep > 0 {
		go br.keepalive(keep)
	}
	return br, nil
}

// CheckKey is the host key check: the store's CheckHostKey, called with
// the dialed "address:port" and the key's SHA256 fingerprint.
type CheckKey func(hostname, fingerprint string) error

// connectTimeout bounds a whole connect (both hops, TCP and handshake).
const connectTimeout = 30 * time.Second

// dialTimeout bounds one TCP dial.
const dialTimeout = 12 * time.Second

// SSHClient connects and authenticates to host, through host.Jump when it
// is set (see DialContext).
func SSHClient(host store.Host, checkKey func(hostname, fingerprint string) error) (*ssh.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	return DialContext(ctx, host, checkKey)
}

// HopError says which hop of a jump host connect failed: Hop is "jump" or
// "target" and Name is that host's label. Errors from a direct connect are
// not wrapped.
type HopError struct {
	Hop  string
	Name string
	Err  error
}

func (e *HopError) Error() string {
	if e.Hop == "jump" {
		return fmt.Sprintf("jump host %s: %v", e.Name, e.Err)
	}
	return fmt.Sprintf("%s (through jump host): %v", e.Name, e.Err)
}

func (e *HopError) Unwrap() error { return e.Err }

// DialContext connects and authenticates to host. With host.Jump set it
// first connects to the jump host, opens a direct-tcpip channel from there
// to host's address and runs the SSH handshake over that channel: the
// same as ssh -J, one hop. Both hops go through checkKey, each under its
// own "address:port". Closing the returned client also closes the jump
// connection. ctx cancels the connect; once it returns, ctx no longer
// matters.
func DialContext(ctx context.Context, host store.Host, checkKey func(hostname, fingerprint string) error) (*ssh.Client, error) {
	if host.Jump == nil {
		return dialHost(ctx, nil, host, checkKey)
	}
	jump := *host.Jump
	if jump.Jump != nil || strings.TrimSpace(jump.JumpHost) != "" {
		return nil, errors.New("only one jump host hop is supported")
	}
	if host.ID != "" && jump.ID == host.ID {
		return nil, errors.New("a host cannot be its own jump host")
	}
	jc, err := dialHost(ctx, nil, jump, checkKey)
	if err != nil {
		return nil, &HopError{Hop: "jump", Name: hopName(jump), Err: err}
	}
	tc, err := dialHost(ctx, jc, host, checkKey)
	if err != nil {
		_ = jc.Close()
		return nil, &HopError{Hop: "target", Name: hopName(host), Err: err}
	}
	go func() {
		_ = tc.Wait()
		_ = jc.Close()
	}()
	return tc, nil
}

func hopName(h store.Host) string {
	if n := strings.TrimSpace(h.Name); n != "" {
		return n
	}
	return store.HostKeyName(h)
}

// dialHost connects to host directly, or through via (an open SSH client)
// when via is set.
func dialHost(ctx context.Context, via *ssh.Client, host store.Host, checkKey func(hostname, fingerprint string) error) (*ssh.Client, error) {
	auth, err := sshAuth(host)
	if err != nil {
		return nil, err
	}
	addr := store.HostKeyName(host)
	cfg := &ssh.ClientConfig{
		User:            host.Username,
		Auth:            auth,
		Timeout:         dialTimeout,
		HostKeyCallback: hostKeyCallback(checkKey),
	}
	var raw net.Conn
	if via == nil {
		d := net.Dialer{Timeout: dialTimeout}
		if keep := host.KeepAlive(); keep > 0 {
			d.KeepAliveConfig = net.KeepAliveConfig{Enable: true, Idle: keep, Interval: keep, Count: 4}
		}
		raw, err = d.DialContext(ctx, "tcp", addr)
	} else {
		raw, err = via.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	// The handshake has no timeout of its own: close the connection when
	// ctx ends, so a server that never answers cannot hang the connect.
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	c, chans, reqs, err := ssh.NewClientConn(raw, addr, cfg)
	if !stop() {
		if c != nil {
			_ = c.Close()
		}
		_ = raw.Close()
		return nil, fmt.Errorf("ssh dial %s: %w", addr, ctx.Err())
	}
	if err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	return ssh.NewClient(c, chans, reqs), nil
}

func (b *sshBridge) keepalive(interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-b.stop:
			return
		case <-t.C:
			_, _, err := b.client.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				_ = b.Close()
				return
			}
		}
	}
}

func (b *sshBridge) ExitKind() ExitKind {
	b.waitMu.Lock()
	err := b.waitErr
	b.waitMu.Unlock()
	if err == nil {
		return ExitClean
	}
	var ee *ssh.ExitError
	if errors.As(err, &ee) {
		return ExitClean
	}
	return ExitDrop
}

func sshAuth(host store.Host) ([]ssh.AuthMethod, error) {
	switch host.AuthType {
	case "key":
		if host.PrivateKey == "" {
			return nil, fmt.Errorf("private key is required")
		}
		var (
			signer ssh.Signer
			err    error
		)
		if host.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(host.PrivateKey), []byte(host.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(host.PrivateKey))
		}
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	default:
		if host.Password == "" {
			return nil, fmt.Errorf("password is required")
		}
		return []ssh.AuthMethod{ssh.Password(host.Password)}, nil
	}
}

// hostKeyCallback checks the key's fingerprint and adds its type to an
// unknown or changed key error, so the trust prompt can show and save it.
func hostKeyCallback(check func(hostname, fingerprint string) error) ssh.HostKeyCallback {
	return func(hostname string, _ net.Addr, key ssh.PublicKey) error {
		err := check(hostname, ssh.FingerprintSHA256(key))
		var unk *store.UnknownHostError
		var mm *store.HostKeyMismatchError
		switch {
		case errors.As(err, &unk):
			unk.KeyType = key.Type()
		case errors.As(err, &mm):
			mm.KeyType = key.Type()
		}
		return err
	}
}

func (b *sshBridge) Read(p []byte) (int, error)  { return b.stdout.Read(p) }
func (b *sshBridge) Write(p []byte) (int, error) { return b.stdin.Write(p) }

func (b *sshBridge) Resize(cols, rows int) error {
	if cols < 1 {
		cols = 80
	}
	if rows < 1 {
		rows = 24
	}
	return b.session.WindowChange(rows, cols)
}

func (b *sshBridge) Close() error {
	var err error
	b.once.Do(func() {
		if b.stop != nil {
			close(b.stop)
		}
		_ = b.stdin.Close()
		_ = b.session.Close()
		err = b.client.Close()
	})
	return err
}
