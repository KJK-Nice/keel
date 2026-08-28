package ui

import (
	"strings"
	"testing"

	"keel/internal/store"
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

func TestSignalsJSONIncludesLocked(t *testing.T) {
	s := PageProps{Locked: true, NeedsSetup: true, Query: `a"b`}.SignalsJSON()
	for _, want := range []string{`"locked":true`, `"needsSetup":true`, `"query":"a\"b"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s in %s", want, s)
		}
	}
}
