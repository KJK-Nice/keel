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

func TestNestGroupTreeParentsAndCounts(t *testing.T) {
	groups := []store.Group{
		{ID: "root", Name: "work"},
		{ID: "prod", Name: "prod", ParentID: "root"},
		{ID: "labs", Name: "labs"},
	}
	hosts := []store.Host{
		{ID: "h1", Name: "api", Group: "prod", GroupID: "prod", Address: "10.0.0.1"},
		{ID: "h2", Name: "gpu", Group: "labs", GroupID: "labs", Address: "10.0.0.9", Tags: []string{"ml"}},
		{ID: "h3", Name: "solo", Address: "10.0.0.8"},
	}
	tree := nestGroupTree(groups, hosts, "", "")
	if len(tree) != 2 {
		t.Fatalf("roots = %d", len(tree))
	}
	var work, labs groupNode
	for _, n := range tree {
		switch n.Group.ID {
		case "root":
			work = n
		case "labs":
			labs = n
		}
	}
	if work.Count != 1 || len(work.Kids) != 1 || work.Kids[0].Group.ID != "prod" || len(work.Kids[0].Hosts) != 1 {
		t.Fatalf("work tree = %+v", work)
	}
	if labs.Count != 1 || labs.Depth != 1 {
		t.Fatalf("labs = %+v", labs)
	}
	if n := len(ungroupedHosts(hosts, groups, "")); n != 1 {
		t.Fatalf("ungrouped = %d", n)
	}
	filtered := nestGroupTree(groups, hosts, "work", "")
	if len(filtered) != 1 || filtered[0].Group.ID != "root" || filtered[0].Count != 1 {
		t.Fatalf("ancestor query = %+v", filtered)
	}
	all := hostsInCurrentAndSubgroups(groups, hosts, "", "")
	if len(all) != 3 {
		t.Fatalf("root hosts (current+subgroups) = %d", len(all))
	}
	workHosts := hostsInCurrentAndSubgroups(groups, hosts, "root", "")
	if len(workHosts) != 1 || workHosts[0].ID != "h1" {
		t.Fatalf("work subtree = %+v", workHosts)
	}
	byTag := hostsInCurrentAndSubgroups(groups, hosts, "", "ml")
	if len(byTag) != 1 || byTag[0].ID != "h2" {
		t.Fatalf("tags stay orthogonal: %+v", byTag)
	}
	path := groupFocusPath(groups, "prod")
	if len(path) != 2 || path[0].ID != "root" || path[1].ID != "prod" {
		t.Fatalf("focus path = %+v", path)
	}
	if vaultTreeEmpty(tree, ungroupedHosts(hosts, groups, "")) {
		t.Fatal("tree should not be empty")
	}
	if nestDepthAttr(3) != "3" {
		t.Fatal("depth attr")
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
