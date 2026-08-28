package session

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
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

func SSHClient(host store.Host, checkKey func(hostname, fingerprint string) error) (*ssh.Client, error) {
	auth, err := sshAuth(host)
	if err != nil {
		return nil, err
	}
	port := host.Port
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(host.Address, strconv.Itoa(port))
	cfg := &ssh.ClientConfig{
		User:            host.Username,
		Auth:            auth,
		Timeout:         12 * time.Second,
		HostKeyCallback: hostKeyCallback(checkKey),
	}
	client, err := dialSSH(addr, cfg, host.KeepAlive())
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	return client, nil
}

func dialSSH(addr string, cfg *ssh.ClientConfig, keep time.Duration) (*ssh.Client, error) {
	d := net.Dialer{Timeout: cfg.Timeout}
	if keep > 0 {
		d.KeepAliveConfig = net.KeepAliveConfig{Enable: true, Idle: keep, Interval: keep, Count: 4}
	}
	raw, err := d.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	c, chans, reqs, err := ssh.NewClientConn(raw, addr, cfg)
	if err != nil {
		_ = raw.Close()
		return nil, err
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

func hostKeyCallback(check func(hostname, fingerprint string) error) ssh.HostKeyCallback {
	return func(hostname string, _ net.Addr, key ssh.PublicKey) error {
		return check(hostname, ssh.FingerprintSHA256(key))
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
