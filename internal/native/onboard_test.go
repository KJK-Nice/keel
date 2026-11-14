package native

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"keel/internal/native/keelclient"
)

const (
	pathWeb = "/h/.ssh/id_web"
	pathDB  = "/h/.ssh/id_db"
	pathBad = "/h/.ssh/gone"
)

func testPreview() keelclient.SSHPreview {
	return keelclient.SSHPreview{
		Path: "~/.ssh/config", Found: true,
		Hosts: []keelclient.ConfigHost{
			{Alias: "web", HostName: "web.example.net", User: "deploy", Port: 22, IdentityFiles: []string{pathWeb}, Source: "~/.ssh/config:1"},
			{Alias: "db", HostName: "10.0.0.5", User: "deploy", Port: 2222, IdentityFiles: []string{pathDB}, Saved: true, Source: "~/.ssh/config:5"},
			{Alias: "jumpy", HostName: "10.0.0.9", User: "ops", Port: 22, Proxy: "ProxyJump", ProxyJump: "bastion", Source: "~/.ssh/config:9",
				Notes: []string{"ProxyJump is not supported yet; the host is added without it", `Port "x" is not a port number; 22 used`}},
		},
		Patterns: []keelclient.ConfigPattern{{Pattern: "*.prod", Source: "~/.ssh/config:12"}},
		Keys: []keelclient.ConfigKey{
			{Path: pathWeb, Display: "~/.ssh/id_web", Name: "id_web", Exists: true, UsedBy: []string{"web"}},
			{Path: pathDB, Display: "~/.ssh/id_db", Name: "id_db", Exists: true, Encrypted: true, UsedBy: []string{"db"}},
			{Path: pathBad, Display: "~/.ssh/gone", Name: "gone", Problem: "file not found"},
		},
		Notes: []keelclient.ConfigNote{{Source: "~/.ssh/config:14", Text: "Match block skipped: Keel imports Host blocks only"}},
	}
}

func sortedHosts(s *importSel) []string {
	var out []string
	for a, on := range s.hosts {
		if on {
			out = append(out, a)
		}
	}
	slices.Sort(out)
	return out
}

func TestImportDefaultsNewDirectHostsOnSavedAndProxyHostsOffEveryKeyOff(t *testing.T) {
	p := testPreview()
	s := defaultSelection(p)
	if got := sortedHosts(s); !slices.Equal(got, []string{"web"}) {
		t.Fatalf("on: %v", got)
	}
	for path, k := range s.keys {
		if k.on {
			t.Errorf("key %s is on by default", path)
		}
	}
	if len(s.keys) != 3 {
		t.Errorf("%d key states", len(s.keys))
	}
	if summaryText(p, s) != "1 of 3 selected" || allState(p, s) != checkMixed || addLabel(1) != "Add 1 host" || addLabel(0) != "Add hosts" || addLabel(2) != "Add 2 hosts" {
		t.Errorf("%q %v", summaryText(p, s), allState(p, s))
	}
}

func TestImportRequestNamesHostsAndOnlyTheKeysTurnedOnWithAPassphraseWhereNeeded(t *testing.T) {
	p := testPreview()
	s := defaultSelection(p)
	s.hosts["jumpy"] = true
	req := importRequest(p, s)
	if !slices.Equal(req.Hosts, []string{"web", "jumpy"}) || len(req.Keys) != 0 || req.WithoutJump != nil {
		t.Fatalf("%+v", req)
	}
	s.keys[pathWeb].on, s.keys[pathDB].on, s.keys[pathBad].on = true, true, true
	if missingPassphrase(p, s) != 1 {
		t.Fatal("the encrypted key with no passphrase is not found")
	}
	s.keys[pathDB].pass, s.keys[pathWeb].pass = "pw", "ignored"
	if missingPassphrase(p, s) != -1 {
		t.Fatal("still missing")
	}
	req = importRequest(p, s)
	want := []keelclient.ImportKey{{Path: pathWeb}, {Path: pathDB, Passphrase: "pw"}}
	if !slices.Equal(req.Keys, want) {
		t.Fatalf("keys %v", req.Keys) // an unreadable key is never sent; a passphrase only for a key that has one
	}
}

func TestImportTableMarksSavedHostsFlagsProxiesAndKeepsPatternsApart(t *testing.T) {
	p := testPreview()
	if hostAddress(p.Hosts[1]) != "10.0.0.5:2222" || hostAddress(p.Hosts[0]) != "web.example.net" || hostAddress(keelclient.ConfigHost{Alias: "x"}) != "x" {
		t.Error("hostAddress")
	}
	if v := viaOf(p.Hosts[2]); v.kind != "unused" || v.text != "ProxyJump bastion, not used" || !v.noJump {
		t.Errorf("%+v", v)
	}
	if v := viaOf(p.Hosts[0]); v.kind != "" {
		t.Errorf("a host with no proxy has a via: %+v", v)
	}
	if got := keyNames(p.Hosts[1]); got != "id_db" {
		t.Errorf("key cell %q", got)
	}
	if baseName(`C:\Users\me\.ssh\id_win`) != "id_win" {
		t.Error("baseName with backslashes")
	}
}

func TestImportNotesMergeConfigAndHostNotesWithProxyNotesLeftToTheViaColumn(t *testing.T) {
	var sources []string
	for _, n := range notesList(testPreview()) {
		sources = append(sources, n.source)
	}
	if !slices.Equal(sources, []string{"~/.ssh/config:14", "jumpy"}) {
		t.Fatalf("%v", sources)
	}
}

func TestImportLeadAndResultText(t *testing.T) {
	p := testPreview()
	if got := leadText(p); got != "Keel found ~/.ssh/config with 3 hosts. Pick the ones to add. Your config file is not changed." {
		t.Error(got)
	}
	if got := leadText(keelclient.SSHPreview{Path: "~/.ssh/config"}); got != "Keel found no ~/.ssh/config. Add hosts later with New host." {
		t.Error(got)
	}
	two := []keelclient.Host{{}, {}}
	if got := importedText(keelclient.ImportResult{Hosts: two, Identities: []keelclient.Identity{{}}}); got != "Added 2 hosts and 1 key" {
		t.Error(got)
	}
	if got := importedText(keelclient.ImportResult{Hosts: two[:1]}); got != "Added 1 host" {
		t.Error(got)
	}
	if got := importedText(keelclient.ImportResult{Hosts: two[:1], Failed: []keelclient.ImportFailure{{Alias: "db", Error: "its jump host bastion was not added"}}}); got != "Added 1 host. Not added: db (its jump host bastion was not added)" {
		t.Error(got)
	}
}

func TestImportAProxyJumpKeelCanKeepShowsItsJumpHostInsteadOfTheFlag(t *testing.T) {
	saved := keelclient.ConfigHost{Alias: "db2", Proxy: "ProxyJump", ProxyJump: "gate", JumpSaved: "gate"}
	added := keelclient.ConfigHost{Alias: "db3", Proxy: "ProxyJump", ProxyJump: "bastion", JumpAlias: "bastion"}
	if v := viaOf(saved); v.kind != "saved" || v.text != "Via gate" || v.noJump {
		t.Errorf("%+v", v)
	}
	if v := viaOf(added); v.kind != "if-added" || v.text != "Via bastion if added" || !v.noJump {
		t.Errorf("%+v", v)
	}
	if v := viaOf(keelclient.ConfigHost{Alias: "c", Proxy: "ProxyCommand"}); v.kind != "unused" || v.text != "ProxyCommand, not used" || v.noJump {
		t.Errorf("a ProxyCommand has no Without the jump box: %+v", v)
	}
}

func TestImportAProxyJumpHostWhoseJumpIsNotKeptNeedsWithoutTheJump(t *testing.T) {
	p := testPreview()
	s := defaultSelection(p)
	s.hosts["jumpy"] = true
	if h := missingJump(p, s); h == nil || h.Alias != "jumpy" {
		t.Fatalf("%v", h)
	}
	if importRequest(p, s).WithoutJump != nil {
		t.Error("withoutJump is sent before it is ticked")
	}
	s.noJump["jumpy"] = true
	if missingJump(p, s) != nil {
		t.Error("still missing")
	}
	if got := importRequest(p, s).WithoutJump; !slices.Equal(got, []string{"jumpy"}) {
		t.Errorf("withoutJump %v", got)
	}
	// Its config jump host being added keeps the jump; nothing to waive.
	p.Hosts = append(p.Hosts, keelclient.ConfigHost{Alias: "db3", HostName: "10.0.0.7", User: "d", Port: 22, Proxy: "ProxyJump", ProxyJump: "web", JumpAlias: "web", JumpVia: "deploy@web.example.net:22"})
	s2 := defaultSelection(p)
	s2.hosts["db3"] = true
	if missingJump(p, s2) != nil {
		t.Error("db3's jump host web is added: nothing to waive")
	}
	if got := importRequest(p, s2).WithoutJump; got != nil {
		t.Errorf("withoutJump %v", got)
	}
	delete(s2.hosts, "web")
	if h := missingJump(p, s2); h == nil || h.Alias != "db3" {
		t.Errorf("%v", h)
	}
}

func TestImportSelectionAndItsPassphrasesPrintWithoutThem(t *testing.T) {
	p := testPreview()
	s := defaultSelection(p)
	s.keyOf(pathDB).on, s.keyOf(pathDB).pass = true, "typed-secret-xyz"
	for _, f := range []string{fmt.Sprintf("%v", s), fmt.Sprintf("%+v", s), fmt.Sprintf("%#v", s), fmt.Sprintf("%+v", *s.keys[pathDB]), fmt.Sprintf("%#v", s.keys[pathDB])} {
		if strings.Contains(f, "typed-secret-xyz") {
			t.Errorf("a selection prints the passphrase: %s", f)
		}
	}
	s.clearSecrets()
	if s.keys[pathDB].pass != "" {
		t.Error("clearSecrets left a passphrase")
	}
}
