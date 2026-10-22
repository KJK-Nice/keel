package hostimport

import (
	"testing"

	"keel/internal/sshconfig"
	"keel/internal/store"
)

func TestPlanJumpPicksOnlyAnExactSavedHost(t *testing.T) {
	h := sshconfig.Host{Alias: "app", HostName: "10.0.0.5", User: "deploy", Port: 22, Proxy: "ProxyJump", ProxyJump: "ops@bastion.example"}
	saved := []store.Host{
		{ID: "a", Name: "bastion", Address: "bastion.example", Username: "ops"},
		{ID: "b", Name: "bastion root", Address: "bastion.example", Username: "root"},
	}
	p := planJump(h, map[string]sshconfig.Host{"app": h}, saved, "me")
	if p.savedID != "a" || p.via != "ops@bastion.example:22" || p.problem != "" {
		t.Fatalf("plan = %+v", p)
	}
	saved = append(saved, store.Host{ID: "c", Name: "bastion again", Address: "bastion.example", Port: 22, Username: "ops"})
	p = planJump(h, map[string]sshconfig.Host{"app": h}, saved, "me")
	if p.savedID != "" || p.problem == "" {
		t.Fatalf("two matches must not pick one: %+v", p)
	}
}

func TestPlanJumpUsesLocalUserAndConfigAlias(t *testing.T) {
	jump := sshconfig.Host{Alias: "bastion", HostName: "10.0.0.1", Port: 2222}
	h := sshconfig.Host{Alias: "app", HostName: "10.0.0.5", Proxy: "ProxyJump", ProxyJump: "bastion"}
	p := planJump(h, map[string]sshconfig.Host{"app": h, "bastion": jump}, nil, "me")
	if p.alias != "bastion" || p.via != "me@10.0.0.1:2222" {
		t.Fatalf("plan = %+v", p)
	}
	if !p.linked(map[string]bool{"bastion": true}) || p.linked(map[string]bool{}) {
		t.Fatal("linked only when the jump alias is imported too")
	}
}

func TestUniqueName(t *testing.T) {
	taken := map[string]bool{"id_ed25519": true, "id_ed25519 (2)": true}
	if got := uniqueName("id_ed25519", taken); got != "id_ed25519 (3)" {
		t.Fatalf("got %q", got)
	}
	if got := uniqueName("", nil); got != "key" {
		t.Fatalf("got %q", got)
	}
}

func TestNotSetUp(t *testing.T) {
	if _, err := BuildPreview(nil, sshconfig.Options{}); err != ErrNotSetUp {
		t.Fatalf("err = %v", err)
	}
}
