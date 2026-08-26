package session

import (
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
	once    sync.Once
}

func openSSH(host store.Host, cols, rows int, checkKey func(hostname, fingerprint string) error) (Bridge, error) {
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
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
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
	}
	go func() {
		err := sess.Wait()
		_ = pw.CloseWithError(err)
	}()
	return br, nil
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
		_ = b.stdin.Close()
		_ = b.session.Close()
		err = b.client.Close()
	})
	return err
}
