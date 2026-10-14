package ui

import (
	"strings"
	"testing"
	"time"

	"keel/internal/applog"
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

// Status is a dot class and a word, never colour alone; failures carry
// their cause.
func TestTunnelStateAndLogKinds(t *testing.T) {
	ready := tunnelState(tunnel.Info{Status: tunnel.StatusReady})
	if ready.Label != "Running" || ready.DotClass != "k-dot--live" || !ready.On || ready.Filter != "running" {
		t.Fatalf("ready = %+v", ready)
	}
	failed := tunnelState(tunnel.Info{Status: tunnel.StatusError, Error: "refused"})
	if failed.Label != "Failed · refused" || failed.DotClass != "k-dot--dead" || failed.On || failed.Filter != "stopped" {
		t.Fatalf("failed = %+v", failed)
	}
	if st := tunnelState(tunnel.Info{}); st.Label != "Stopped" || st.On {
		t.Fatalf("stopped = %+v", st)
	}
	if !tunnelEveryInterface(tunnel.Info{Tunnel: store.Tunnel{BindAddr: "0.0.0.0"}}) ||
		tunnelEveryInterface(tunnel.Info{Tunnel: store.Tunnel{BindAddr: "127.0.0.1"}}) {
		t.Fatal("only a wildcard bind is every interface")
	}
	if logBadgeClass("error") != "k-badge--flare" || logBadgeClass("info") != "" {
		t.Fatal("only failures are flare")
	}
	if logGroup("session") != "sessions" || logGroup("tunnel") != "forwards" || logGroup("identity") != "vault" || logGroup("info") != "other" || logGroup("agent.read") != "agent" || logGroup("agent") != "agent" || logGroup("agentx") != "other" {
		t.Fatal("log chip groups")
	}
	p := PageProps{Hosts: []store.Host{{ID: "h1", Name: "db"}}}
	if logTarget(p, applog.Entry{HostID: "h1"}) != "db" || logTarget(p, applog.Entry{}) != "—" {
		t.Fatal("log targets")
	}
}

func TestVaultTileLabels(t *testing.T) {
	if identityKindLabel("key") != "SSH key" || identityKindLabel("password") != "Password" {
		t.Fatal("identity kind labels")
	}
	hosts := []store.Host{
		{ID: "a", AuthType: "identity", IdentityID: "k"},
		{ID: "b", AuthType: "identity", IdentityID: "k"},
		{ID: "c", AuthType: "password", IdentityID: "k"},
	}
	if identityHostCount(hosts, "k") != 2 {
		t.Fatal("only hosts that authenticate with the identity use it")
	}
	if got := identitySubtitle(hosts, store.Identity{ID: "k", Kind: "key", Username: "ops"}); got != "SSH key · ops · 2 hosts" {
		t.Fatalf("subtitle = %q", got)
	}
	if tunnelFlag("local") != "-L" || tunnelFlag("remote") != "-R" || tunnelFlag("dynamic") != "-D" {
		t.Fatal("tunnel flags")
	}
	if snippetCommand(store.Snippet{Command: " ls -la\n "}) != "ls -la" {
		t.Fatal("snippet command trim")
	}
	if snippetFirstLine(store.Snippet{Command: "ls\ndf -h\n"}) != "ls …" {
		t.Fatal("snippet first line")
	}
	ts := time.Date(2026, 8, 31, 15, 32, 8, 0, time.UTC)
	if logDateLabel(ts) == "" || logTimeLabel(ts) == "" {
		t.Fatal("log date/time labels")
	}
	if logDateLabel(time.Time{}) != "" || logTimeLabel(time.Time{}) != "" {
		t.Fatal("zero log time should be empty")
	}
}

func TestUngroupedHostsKeepOrphans(t *testing.T) {
	groups := []store.Group{{ID: "g", Name: "prod"}}
	hosts := []store.Host{{ID: "a", GroupID: "g"}, {ID: "b"}, {ID: "c", GroupID: "gone"}}
	got := ungroupedHosts(hosts, groups, "")
	if len(got) != 2 || got[0].ID != "b" || got[1].ID != "c" {
		t.Fatalf("a host whose group is gone sits at the root: %+v", got)
	}
}
