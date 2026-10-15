package session_test

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"keel/internal/session"
	"keel/internal/session/sshtest"
	"keel/internal/store"
)

// keys is a stand-in for the store's known hosts.
type keys struct {
	mu    sync.Mutex
	known map[string]string
	asked []string
}

func (k *keys) check(hostname, keyType, fp string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.asked = append(k.asked, hostname)
	prev, ok := k.known[hostname]
	if !ok {
		return &store.UnknownHostError{Host: hostname, Fingerprint: fp, KeyType: keyType, Token: "tok"}
	}
	if prev != fp {
		return &store.HostKeyMismatchError{Host: hostname, Expected: prev, Got: fp, KeyType: keyType, Token: "tok"}
	}
	return nil
}

func trusted(servers ...*sshtest.Server) *keys {
	k := &keys{known: map[string]string{}}
	for _, s := range servers {
		k.known[s.Addr] = s.Fingerprint()
	}
	return k
}

func hostFor(s *sshtest.Server, name string) store.Host {
	return store.Host{ID: name, Name: name, Address: s.Host, Port: s.Port, Username: s.User, Password: s.Password, AuthType: "password", KeepAliveSec: -1}
}

func viaJump(target, jump *sshtest.Server) store.Host {
	h := hostFor(target, "target")
	j := hostFor(jump, "bastion")
	h.JumpHost = j.ID
	h.Jump = &j
	return h
}

func TestDialThroughJumpHost(t *testing.T) {
	jump := sshtest.New(t, sshtest.Options{User: "jumper", Password: "jpw"})
	target := sshtest.New(t, sshtest.Options{User: "deploy", Password: "tpw", NoForward: true})
	k := trusted(jump, target)
	client, err := session.SSHClient(viaJump(target, jump), k.check)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if jump.Forwards() != 1 || jump.Logins() != 1 || target.Logins() != 1 {
		t.Fatalf("forwards=%d jump logins=%d target logins=%d", jump.Forwards(), jump.Logins(), target.Logins())
	}
	// Both hops were checked, each under its own address.
	if strings.Join(k.asked, ",") != jump.Addr+","+target.Addr {
		t.Fatalf("checked %v", k.asked)
	}
	// A shell works over the forwarded connection.
	sess, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	sess.Close()
}

func TestOpenThroughJumpHostRunsShell(t *testing.T) {
	jump := sshtest.New(t, sshtest.Options{})
	target := sshtest.New(t, sshtest.Options{})
	br, err := session.Open(viaJump(target, jump), 80, 24, trusted(jump, target).check)
	if err != nil {
		t.Fatal(err)
	}
	defer br.Close()
	if _, err := br.Write([]byte("hi\r")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for target.ShellInput() != "hi\r" {
		if time.Now().After(deadline) {
			t.Fatalf("target shell got %q", target.ShellInput())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if jump.Sessions() != 0 {
		t.Fatal("the jump host must not get a shell")
	}
}

func TestJumpHostKeyChecksEachHop(t *testing.T) {
	jump := sshtest.New(t, sshtest.Options{})
	target := sshtest.New(t, sshtest.Options{})

	t.Run("jump mismatch", func(t *testing.T) {
		k := trusted(jump, target)
		k.known[jump.Addr] = "SHA256:not-the-key"
		_, err := session.SSHClient(viaJump(target, jump), k.check)
		var mm *store.HostKeyMismatchError
		var hop *session.HopError
		if !errors.As(err, &mm) || !errors.As(err, &hop) || hop.Hop != "jump" {
			t.Fatalf("err = %v", err)
		}
		if mm.Host != jump.Addr || mm.Got != jump.Fingerprint() || mm.KeyType != "ssh-ed25519" {
			t.Fatalf("mismatch = %+v", mm)
		}
		if !strings.Contains(err.Error(), "jump host bastion") {
			t.Fatalf("message does not name the hop: %v", err)
		}
		if target.Logins() != 0 {
			t.Fatal("target reached after the jump host failed its key check")
		}
	})

	t.Run("target mismatch", func(t *testing.T) {
		k := trusted(jump, target)
		k.known[target.Addr] = "SHA256:not-the-key"
		_, err := session.SSHClient(viaJump(target, jump), k.check)
		var mm *store.HostKeyMismatchError
		var hop *session.HopError
		if !errors.As(err, &mm) || !errors.As(err, &hop) || hop.Hop != "target" {
			t.Fatalf("err = %v", err)
		}
		if mm.Host != target.Addr {
			t.Fatalf("mismatch names %s, want %s", mm.Host, target.Addr)
		}
		if !strings.Contains(err.Error(), "target (through jump host)") {
			t.Fatalf("message does not name the hop: %v", err)
		}
	})

	t.Run("target unknown", func(t *testing.T) {
		k := trusted(jump)
		_, err := session.SSHClient(viaJump(target, jump), k.check)
		var unk *store.UnknownHostError
		if !errors.As(err, &unk) || unk.Host != target.Addr || unk.Fingerprint != target.Fingerprint() || unk.KeyType != "ssh-ed25519" {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestJumpHostRejectsSecondHopAndSelf(t *testing.T) {
	jump := sshtest.New(t, sshtest.Options{})
	target := sshtest.New(t, sshtest.Options{})
	h := viaJump(target, jump)
	inner := hostFor(jump, "inner")
	h.Jump.Jump = &inner
	if _, err := session.SSHClient(h, trusted(jump, target).check); err == nil || !strings.Contains(err.Error(), "one jump host hop") {
		t.Fatalf("two hops: err = %v", err)
	}
	self := hostFor(target, "same")
	j := self
	self.Jump = &j
	if _, err := session.SSHClient(self, trusted(target).check); err == nil || !strings.Contains(err.Error(), "own jump host") {
		t.Fatalf("self: err = %v", err)
	}
	if jump.Logins() != 0 || target.Logins() != 0 {
		t.Fatal("a rejected chain must not connect")
	}
}

func TestProbeOutcomes(t *testing.T) {
	jump := sshtest.New(t, sshtest.Options{})
	target := sshtest.New(t, sshtest.Options{})
	ctx := context.Background()

	t.Run("ok through jump", func(t *testing.T) {
		res := session.Probe(ctx, viaJump(target, jump), trusted(jump, target).check)
		if !res.OK || res.Kind != session.ProbeOK {
			t.Fatalf("res = %+v", res)
		}
		if target.Sessions() != 0 {
			t.Fatal("a probe must not open a session")
		}
	})

	t.Run("auth", func(t *testing.T) {
		h := hostFor(target, "t")
		h.Password = "wrong"
		res := session.Probe(ctx, h, trusted(target).check)
		if res.OK || res.Kind != session.ProbeAuth {
			t.Fatalf("res = %+v", res)
		}
	})

	t.Run("unknown host is reported, not trusted", func(t *testing.T) {
		k := trusted()
		res := session.Probe(ctx, hostFor(target, "t"), k.check)
		if res.Kind != session.ProbeUnknown || res.Fingerprint != target.Fingerprint() || res.KeyType != "ssh-ed25519" || res.Hostname != target.Addr || res.TrustToken != "tok" {
			t.Fatalf("res = %+v", res)
		}
		if len(k.known) != 0 {
			t.Fatal("probe trusted a key")
		}
	})

	t.Run("changed key", func(t *testing.T) {
		k := trusted(target)
		k.known[target.Addr] = "SHA256:old"
		res := session.Probe(ctx, hostFor(target, "t"), k.check)
		if res.Kind != session.ProbeMismatch || res.Expected != "SHA256:old" || res.Fingerprint != target.Fingerprint() {
			t.Fatalf("res = %+v", res)
		}
	})

	t.Run("jump hop named", func(t *testing.T) {
		h := viaJump(target, jump)
		h.Jump.Password = "wrong"
		res := session.Probe(ctx, h, trusted(jump, target).check)
		if res.Kind != session.ProbeAuth || res.Hop != "jump" || res.HopName != "bastion" {
			t.Fatalf("res = %+v", res)
		}
	})

	t.Run("refused", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		_, port, _ := net.SplitHostPort(ln.Addr().String())
		ln.Close()
		h := hostFor(target, "t")
		h.Port, _ = strconv.Atoi(port)
		res := session.Probe(ctx, h, trusted().check)
		if res.Kind != session.ProbeRefused {
			t.Fatalf("res = %+v", res)
		}
	})

	t.Run("refused behind jump", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		_, port, _ := net.SplitHostPort(ln.Addr().String())
		ln.Close()
		h := viaJump(target, jump)
		h.Port, _ = strconv.Atoi(port)
		res := session.Probe(ctx, h, trusted(jump).check)
		if res.Kind != session.ProbeRefused || res.Hop != "target" {
			t.Fatalf("res = %+v", res)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		// A listener that accepts and never speaks SSH.
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				defer c.Close()
			}
		}()
		_, port, _ := net.SplitHostPort(ln.Addr().String())
		h := hostFor(target, "t")
		h.Port, _ = strconv.Atoi(port)
		tctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		defer cancel()
		res := session.Probe(tctx, h, trusted().check)
		if res.Kind != session.ProbeTimedOut {
			t.Fatalf("res = %+v", res)
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				defer c.Close()
			}
		}()
		_, port, _ := net.SplitHostPort(ln.Addr().String())
		h := hostFor(target, "t")
		h.Port, _ = strconv.Atoi(port)
		cctx, cancel := context.WithCancel(ctx)
		time.AfterFunc(200*time.Millisecond, cancel)
		start := time.Now()
		res := session.Probe(cctx, h, trusted().check)
		if res.Kind != session.ProbeCancelled || time.Since(start) > 5*time.Second {
			t.Fatalf("res = %+v after %v", res, time.Since(start))
		}
	})

	t.Run("dns", func(t *testing.T) {
		res := session.ClassifyProbe(ctx, &net.DNSError{Err: "no such host", Name: "nowhere.invalid", IsNotFound: true})
		if res.Kind != session.ProbeDNS {
			t.Fatalf("res = %+v", res)
		}
	})
}
