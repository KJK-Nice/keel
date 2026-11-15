package keelclient

import (
	"net"
	"testing"
	"time"
)

// A daemon that accepts the connection and never answers the handshake must not hold a dial for the library's 45 s.
func TestDialGivesUpOnADaemonThatNeverAnswers(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { c.Close() })
		}
	}()
	was := dialTimeout
	dialTimeout = 300 * time.Millisecond
	t.Cleanup(func() { dialTimeout = was })
	done := make(chan error, 1)
	go func() { _, err := dialWS("ws://"+ln.Addr().String()+"/ws/session", "tok"); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a dial to a silent daemon worked")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("still dialing after 3 s")
	}
}
