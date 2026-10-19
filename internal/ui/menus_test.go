package ui

import (
	"strings"
	"testing"

	"keel/internal/store"
	"keel/internal/tunnel"
)

func TestSSHCommand(t *testing.T) {
	jump := store.Host{ID: "j", Name: "bastion", Address: "bastion.example", Port: 2200, Username: "ed", AuthType: "password", Password: "jump-secret"}
	hosts := []store.Host{
		jump,
		{ID: "jd", Address: "bastion", Username: "-oProxyCommand=id", AuthType: "password"},
		{ID: "ja", Address: "-oProxyCommand=id", Username: "ed", AuthType: "password"},
		{ID: "js", Address: "bastion x", Username: "ed", AuthType: "password"},
	}
	cases := []struct {
		name string
		h    store.Host
		want string
	}{
		{"plain", store.Host{Address: "10.0.0.5", Port: 22, Username: "ubuntu", AuthType: "password", Password: "pw-123"}, "ssh ubuntu@10.0.0.5"},
		{"port", store.Host{Address: "db.lan", Port: 2222, Username: "pg", AuthType: "key", PrivateKey: "-----BEGIN KEY-----", Passphrase: "pp"}, "ssh -p 2222 pg@db.lan"},
		{"no user", store.Host{Address: "db.lan", AuthType: "password"}, "ssh db.lan"},
		{"jump", store.Host{Address: "10.1.0.9", Username: "app", AuthType: "password", JumpHost: "j"}, "ssh -J ed@bastion.example:2200 app@10.1.0.9"},
		{"ipv6", store.Host{Address: "fe80::1", Username: "root", AuthType: "password"}, "ssh root@[fe80::1]"},
		{"quoted", store.Host{Address: "web", Username: "o'neil", AuthType: "password"}, `ssh 'o'\''neil@web'`},
		// Anything ssh could read as an option, or that splits into words,
		// gets no command: quoting does not stop "-oProxyCommand=…".
		{"dash user", store.Host{Address: "web", Username: "-oProxyCommand=touch /tmp/x", AuthType: "password"}, ""},
		{"dash user no space", store.Host{Address: "web", Username: "-oProxyCommand=id", AuthType: "password"}, ""},
		{"dash address", store.Host{Address: "-oProxyCommand=id", Username: "ops", AuthType: "password"}, ""},
		{"dash address no user", store.Host{Address: "-oProxyCommand=id", AuthType: "password"}, ""},
		{"space user", store.Host{Address: "web", Username: "o neil", AuthType: "password"}, ""},
		{"newline address", store.Host{Address: "web\nid", Username: "ops", AuthType: "password"}, ""},
		{"control user", store.Host{Address: "web", Username: "ops\x1b", AuthType: "password"}, ""},
		{"format char address", store.Host{Address: "web\u200b", Username: "ops", AuthType: "password"}, ""},
		{"dash jump user", store.Host{Address: "10.1.0.9", Username: "app", AuthType: "password", JumpHost: "jd"}, ""},
		{"dash jump address", store.Host{Address: "10.1.0.9", Username: "app", AuthType: "password", JumpHost: "ja"}, ""},
		{"space jump address", store.Host{Address: "10.1.0.9", Username: "app", AuthType: "password", JumpHost: "js"}, ""},
		{"local", store.Host{Name: "Local Shell", AuthType: "local"}, ""},
	}
	for _, c := range cases {
		got := sshCommand(c.h, hosts)
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
		for _, secret := range []string{"pw-123", "BEGIN", "jump-secret"} {
			if strings.Contains(got, secret) {
				t.Errorf("%s: holds a secret: %q", c.name, got)
			}
		}
	}
}

func TestTunnelLocalAddress(t *testing.T) {
	info := func(tn store.Tunnel, listen string) tunnel.Info {
		return tunnel.Info{Tunnel: tn, ListenAddr: listen}
	}
	cases := []struct {
		t    tunnel.Info
		want string
	}{
		{info(store.Tunnel{Kind: "local", BindAddr: "127.0.0.1", BindPort: 5432, DestHost: "db", DestPort: 5432}, ""), "127.0.0.1:5432"},
		{info(store.Tunnel{Kind: "local", BindAddr: "127.0.0.1", BindPort: 0}, ""), ""},
		{info(store.Tunnel{Kind: "local", BindAddr: "127.0.0.1", BindPort: 0}, "127.0.0.1:61234"), "127.0.0.1:61234"},
		{info(store.Tunnel{Kind: "dynamic", BindPort: 1080}, ""), "127.0.0.1:1080"},
		{info(store.Tunnel{Kind: "remote", BindAddr: "0.0.0.0", BindPort: 8080, DestHost: "localhost", DestPort: 3000}, ""), "localhost:3000"},
	}
	for i, c := range cases {
		if got := tunnelLocalAddress(c.t); got != c.want {
			t.Errorf("%d: got %q, want %q", i, got, c.want)
		}
	}
}
