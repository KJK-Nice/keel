package ui

import (
	"strings"
	"testing"

	"keel/components/badge"
	"keel/internal/store"
	"keel/internal/tunnel"
)

func TestGroupHostsFiltersQuery(t *testing.T) {
	hosts := []store.Host{
		{Name: "prod-api", Address: "10.0.0.1", Group: "Production"},
		{Name: "laptop", Address: "127.0.0.1", Group: "Local", AuthType: "local"},
	}
	got := groupHosts(hosts, "prod")
	if len(got) != 1 || got[0].Name != "Production" || len(got[0].Hosts) != 1 {
		t.Fatalf("got %+v", got)
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
