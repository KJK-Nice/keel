package daemon

import "testing"

func TestClientBaseLocalWildcard(t *testing.T) {
	if got := ClientBase("0.0.0.0:47241"); got != "http://127.0.0.1:47241" {
		t.Fatalf("got %s", got)
	}
}

func TestClientBaseRemoteHost(t *testing.T) {
	if got := ClientBase("192.168.1.9:47241"); got != "http://192.168.1.9:47241" {
		t.Fatalf("got %s", got)
	}
}

func TestIsLocalHost(t *testing.T) {
	if !isLocalHost("127.0.0.1") || !isLocalHost("0.0.0.0") {
		t.Fatal("expected local")
	}
	if isLocalHost("10.0.0.4") {
		t.Fatal("10.0.0.4 should be remote")
	}
}
