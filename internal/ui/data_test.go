package ui

import (
	"strings"
	"testing"
	"time"

	"keel/components/badge"
	"keel/internal/store"
	"keel/internal/tunnel"
)

func TestGroupHostsFiltersQuery(t *testing.T) {
	hosts := []store.Host{
		{Name: "prod-api", Address: "10.0.0.1", Group: "Production"},
		{Name: "laptop", Address: "127.0.0.1", Group: "Local", AuthType: "local"},
		{Name: "trainer", Address: "10.0.0.9", Group: "Labs", Tags: []string{"gpu", "ml"}},
	}
	got := groupHosts(hosts, "prod")
	if len(got) != 1 || got[0].Name != "Production" || len(got[0].Hosts) != 1 {
		t.Fatalf("got %+v", got)
	}
	byTag := groupHosts(hosts, "gpu")
	if len(byTag) != 1 || byTag[0].Hosts[0].Name != "trainer" {
		t.Fatalf("tag filter got %+v", byTag)
	}
}

func TestFolderGroupsSkipsDefaultBucket(t *testing.T) {
	groups := []hostGroup{
		{Name: "This machine", Hosts: []store.Host{{Name: "Local Shell"}}},
		{Name: "Hosts", Hosts: []store.Host{{Name: "orphan"}}},
	}
	got := folderGroups(groups)
	if len(got) != 1 || got[0].Name != "This machine" {
		t.Fatalf("got %+v", got)
	}
	if hostCountLabel(1) != "1 Host" || hostCountLabel(3) != "3 Hosts" {
		t.Fatal("host count labels")
	}
	if hostProtocol(store.Host{AuthType: "local"}) != "local" || hostProtocol(store.Host{}) != "ssh" {
		t.Fatal("host protocol labels")
	}
	if n := len(flatHosts(groups)); n != 2 {
		t.Fatalf("flat hosts = %d", n)
	}
}

func TestSignalsJSONIncludesLocked(t *testing.T) {
	s := PageProps{Locked: true, NeedsSetup: true, Query: `a"b`}.SignalsJSON()
	for _, want := range []string{`"locked":true`, `"needsSetup":true`, `"query":"a\"b"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s in %s", want, s)
		}
	}
}

func TestTunnelAndLogBadgeVariants(t *testing.T) {
	if tunnelBadgeVariant(tunnel.StatusReady) != badge.VariantDefault {
		t.Fatal("ready tunnel should be default badge")
	}
	if tunnelBadgeVariant(tunnel.StatusError) != badge.VariantDestructive {
		t.Fatal("error tunnel should be destructive badge")
	}
	if logBadgeVariant("error") != badge.VariantDestructive {
		t.Fatal("error log should be destructive badge")
	}
	if logBadgeVariant("info") != badge.VariantSecondary {
		t.Fatal("info log should be secondary badge")
	}
}

func TestVaultTileLabels(t *testing.T) {
	if identityKindLabel("key") != "Private key" || identityKindLabel("password") != "Auth password" {
		t.Fatal("identity kind labels")
	}
	if identityColor("key") != "#c084fc" || identityColor("password") != "#6ea8ff" {
		t.Fatal("identity colors")
	}
	if tunnelKindLetter("local") != "L" || tunnelKindLetter("remote") != "R" || tunnelKindLetter("dynamic") != "D" {
		t.Fatal("tunnel kind letters")
	}
	if tunnelKindLabel("dynamic") != "Dynamic" || tunnelKindLabel("remote") != "Remote" {
		t.Fatal("tunnel kind labels")
	}
	if tunnelColor(tunnel.StatusReady) != "#5ee0b5" || tunnelColor(tunnel.StatusStopped) != "#8a8a8a" {
		t.Fatal("tunnel status colors")
	}
	if snippetCommand(store.Snippet{Command: " ls -la\n "}) != "ls -la" {
		t.Fatal("snippet command trim")
	}
	if logColor("error") != "#ff6b7a" || logColor("session") != "#5ee0b5" {
		t.Fatal("log colors")
	}
	ts := time.Date(2026, 8, 31, 15, 32, 8, 0, time.UTC)
	if logDateLabel(ts) == "" || logTimeLabel(ts) == "" {
		t.Fatal("log date/time labels")
	}
	if logDateLabel(time.Time{}) != "" || logTimeLabel(time.Time{}) != "" {
		t.Fatal("zero log time should be empty")
	}
}
