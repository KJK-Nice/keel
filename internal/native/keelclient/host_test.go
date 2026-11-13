package keelclient

import "testing"

func TestHostFacts(t *testing.T) {
	no, yes := false, true
	if !(Host{AuthType: "local"}).IsLocal() || (Host{}).IsLocal() || (Host{AuthType: "password"}).IsLocal() {
		t.Error("IsLocal: only authType local")
	}
	if (Host{}).SSHPort() != 22 || (Host{Port: 2222}).SSHPort() != 2222 {
		t.Error("SSHPort")
	}
	if (Host{}).IsProduction() || (Host{Production: &no}).IsProduction() || !(Host{Production: &yes}).IsProduction() {
		t.Error("IsProduction: off unless set")
	}
	if !(Host{}).AgentMayRead() || (Host{AgentReadable: &no}).AgentMayRead() || !(Host{AgentReadable: &yes}).AgentMayRead() {
		t.Error("AgentMayRead: on unless turned off")
	}
	if !(Host{}).AgentMayNote() || (Host{AgentNotes: &no}).AgentMayNote() {
		t.Error("AgentMayNote: on unless turned off")
	}
	for _, c := range []struct{ user, addr, want string }{
		{"deploy", "10.0.0.1", "deploy@10.0.0.1"},
		{" ", " web ", "web"},
		{"u", "fe80::1", "u@[fe80::1]"},
		{"u", "[fe80::1]", "u@[fe80::1]"},
	} {
		if got := (Host{Username: c.user, Address: c.addr}).UserAtAddress(); got != c.want {
			t.Errorf("UserAtAddress(%q, %q) = %q, want %q", c.user, c.addr, got, c.want)
		}
	}
}
