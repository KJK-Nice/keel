package sshconfig

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// home builds a fake home with .ssh/ and the given files (relative to .ssh).
func home(t *testing.T, files map[string]string) (sshDir string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "home", ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func parse(t *testing.T, sshDir string) Result {
	t.Helper()
	res, err := Parse(Options{SSHDir: sshDir, LocalUser: "me"})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func host(t *testing.T, res Result, alias string) Host {
	t.Helper()
	for _, h := range res.Hosts {
		if h.Alias == alias {
			return h
		}
	}
	t.Fatalf("no host %q in %+v", alias, aliases(res))
	return Host{}
}

func aliases(res Result) []string {
	var out []string
	for _, h := range res.Hosts {
		out = append(out, h.Alias)
	}
	return out
}

func hasNote(res Result, sub string) bool {
	for _, n := range res.Notes {
		if strings.Contains(n.Text, sub) {
			return true
		}
	}
	return false
}

func TestMissingConfig(t *testing.T) {
	res := parse(t, home(t, nil))
	if res.Found || len(res.Hosts) != 0 {
		t.Fatalf("missing config: %+v", res)
	}
	if res.Path != "~/.ssh/config" {
		t.Fatalf("path = %q", res.Path)
	}
}

func TestBasicHost(t *testing.T) {
	dir := home(t, map[string]string{"config": `
# a comment
Host web
  HostName web.example.net
  User deploy
  Port 2222
  IdentityFile ~/.ssh/id_web
`})
	h := host(t, parse(t, dir), "web")
	want := filepath.Join(filepath.Dir(dir), ".ssh", "id_web")
	if h.HostName != "web.example.net" || h.User != "deploy" || h.Port != 2222 || len(h.IdentityFiles) != 1 || h.IdentityFiles[0] != want {
		t.Fatalf("host = %+v", h)
	}
	if h.Source != "~/.ssh/config:3" {
		t.Fatalf("source = %q", h.Source)
	}
}

func TestDefaults(t *testing.T) {
	h := host(t, parse(t, home(t, map[string]string{"config": "Host plain\n"})), "plain")
	if h.HostName != "plain" || h.User != "me" || h.Port != 22 || h.Proxy != "" {
		t.Fatalf("defaults = %+v", h)
	}
}

func TestMultiNameHostGivesSeveralRows(t *testing.T) {
	res := parse(t, home(t, map[string]string{"config": "Host a b c\n  User ops\n"}))
	if got := strings.Join(aliases(res), ","); got != "a,b,c" {
		t.Fatalf("aliases = %s", got)
	}
	for _, a := range []string{"a", "b", "c"} {
		if host(t, res, a).User != "ops" {
			t.Fatalf("%s user", a)
		}
	}
}

func TestWildcardsArePatternsAndApplyFirstValueWins(t *testing.T) {
	res := parse(t, home(t, map[string]string{"config": `
User top
Host db-1 *.prod web-? !bastion
  Port 2200
Host db-1
  User late
  Port 9999
Host *
  User star
  IdentityFile ~/.ssh/id_all
Host app
  User app
`}))
	pats := []string{}
	for _, p := range res.Patterns {
		pats = append(pats, p.Pattern)
	}
	if got := strings.Join(pats, ","); got != "*.prod,web-?,!bastion,*" {
		t.Fatalf("patterns = %s", got)
	}
	if got := strings.Join(aliases(res), ","); got != "db-1,app" {
		t.Fatalf("aliases = %s", got)
	}
	db := host(t, res, "db-1")
	// Options before the first Host apply to every host and win.
	if db.User != "top" || db.Port != 2200 || len(db.IdentityFiles) != 1 {
		t.Fatalf("db-1 = %+v", db)
	}
	if app := host(t, res, "app"); app.User != "top" || app.Port != 22 {
		t.Fatalf("app = %+v", app)
	}
}

func TestNegatedPatternRulesBlockOut(t *testing.T) {
	res := parse(t, home(t, map[string]string{"config": `
Host keep skip
Host * !skip
  User viaStar
`}))
	if host(t, res, "keep").User != "viaStar" {
		t.Fatal("keep should match Host * !skip")
	}
	if host(t, res, "skip").User != "me" {
		t.Fatal("skip is negated")
	}
}

func TestIdentityFilesAddUp(t *testing.T) {
	res := parse(t, home(t, map[string]string{"config": `
Host k
  IdentityFile ~/.ssh/one
  IdentityFile ~/.ssh/two
  IdentityFile ~/.ssh/one
  IdentityFile none
Host *
  IdentityFile ~/.ssh/three
`}))
	h := host(t, res, "k")
	var names []string
	for _, p := range h.IdentityFiles {
		names = append(names, filepath.Base(p))
	}
	if got := strings.Join(names, ","); got != "one,two,three" {
		t.Fatalf("identity files = %s", got)
	}
	if got := len(res.KeyPaths()); got != 3 {
		t.Fatalf("key paths = %d", got)
	}
}

func TestIncludeRelativeAndGlob(t *testing.T) {
	res := parse(t, home(t, map[string]string{
		"config":            "Include config.d/*.conf\nHost main\n",
		"config.d/b.conf":   "Host bee\n  User b\n",
		"config.d/a.conf":   "Host ay\n  User a\n",
		"config.d/skip.txt": "Host nope\n",
	}))
	if got := strings.Join(aliases(res), ","); got != "ay,bee,main" {
		t.Fatalf("aliases = %s", got)
	}
	if h := host(t, res, "bee"); h.Source != "~/.ssh/config.d/b.conf:1" {
		t.Fatalf("source = %q", h.Source)
	}
}

func TestIncludeTildeAndAbsolute(t *testing.T) {
	dir := home(t, map[string]string{"extra": "Host extra\n"})
	other := filepath.Join(t.TempDir(), "abs.conf")
	if err := os.WriteFile(other, []byte("Host abs\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte("Include ~/.ssh/extra "+other+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	res := parse(t, dir)
	if got := strings.Join(aliases(res), ","); got != "extra,abs" {
		t.Fatalf("aliases = %s", got)
	}
}

func TestIncludeLoopIsCut(t *testing.T) {
	res := parse(t, home(t, map[string]string{
		"config": "Include a\nHost top\n",
		"a":      "Host fromA\nInclude b\n",
		"b":      "Host fromB\nInclude a\nInclude config\n",
	}))
	if got := strings.Join(aliases(res), ","); got != "fromA,fromB,top" {
		t.Fatalf("aliases = %s", got)
	}
	if !hasNote(res, "Include loop") {
		t.Fatalf("notes = %+v", res.Notes)
	}
}

func TestIncludeDepthLimit(t *testing.T) {
	files := map[string]string{"config": "Include f1\n"}
	for i := 1; i <= 12; i++ {
		files[fmt.Sprintf("f%d", i)] = fmt.Sprintf("Host h%d\nInclude f%d\n", i, i+1)
	}
	res := parse(t, home(t, files))
	if len(res.Hosts) != DefaultMaxDepth {
		t.Fatalf("hosts = %v", aliases(res))
	}
	if !hasNote(res, "deeper than 8") {
		t.Fatalf("notes = %+v", res.Notes)
	}
}

func TestIncludeFileCountLimit(t *testing.T) {
	files := map[string]string{"config": "Include d/*\n"}
	for i := 0; i < DefaultMaxFiles+5; i++ {
		files[fmt.Sprintf("d/%03d", i)] = fmt.Sprintf("Host n%d\n", i)
	}
	res := parse(t, home(t, files))
	if len(res.Hosts) != DefaultMaxFiles-1 {
		t.Fatalf("hosts = %d", len(res.Hosts))
	}
	if !hasNote(res, "more than 64 config files") {
		t.Fatalf("notes = %+v", res.Notes)
	}
}

func TestIncludeNonRegularAndMissing(t *testing.T) {
	dir := home(t, map[string]string{"config": "Include adir nothing-*\nHost ok\n"})
	if err := os.Mkdir(filepath.Join(dir, "adir"), 0o700); err != nil {
		t.Fatal(err)
	}
	res := parse(t, dir)
	if got := strings.Join(aliases(res), ","); got != "ok" {
		t.Fatalf("aliases = %s", got)
	}
	if !hasNote(res, "not a regular file") || !hasNote(res, "matched no file") {
		t.Fatalf("notes = %+v", res.Notes)
	}
}

func TestProxyWarnings(t *testing.T) {
	res := parse(t, home(t, map[string]string{"config": `
Host jumped
  HostName 10.0.0.5
  ProxyJump ops@bastion:2222
Host piped
  ProxyCommand ssh -W %h:%p secret-gateway
Host direct
  ProxyJump none
`}))
	j := host(t, res, "jumped")
	if j.Proxy != "ProxyJump" || j.ProxyJump != "ops@bastion:2222" || len(j.Notes) == 0 {
		t.Fatalf("jumped = %+v", j)
	}
	p := host(t, res, "piped")
	if p.Proxy != "ProxyCommand" {
		t.Fatalf("piped = %+v", p)
	}
	if d := host(t, res, "direct"); d.Proxy != "" {
		t.Fatalf("direct = %+v", d)
	}
	b, _ := json.Marshal(res)
	if strings.Contains(string(b), "secret-gateway") {
		t.Fatal("a ProxyCommand's command must not be returned")
	}
}

func TestMatchBlocksAreSkipped(t *testing.T) {
	res := parse(t, home(t, map[string]string{"config": `
Host m
Match host m exec "false"
  User fromMatch
  Include nope
Host after
  User after
`}))
	if host(t, res, "m").User != "me" {
		t.Fatal("Match options must not apply")
	}
	if host(t, res, "after").User != "after" {
		t.Fatal("a Host after Match starts a new block")
	}
	if !hasNote(res, "Match block skipped") {
		t.Fatalf("notes = %+v", res.Notes)
	}
}

func TestMalformedLines(t *testing.T) {
	res := parse(t, home(t, map[string]string{"config": strings.Join([]string{
		"Host",
		"Host bad",
		"  Port abc",
		"  HostName",
		`  User "unterminated`,
		"  =novalue",
		"  Bogus keyword here",
		"  IdentityFile",
		"  " + strings.Repeat("x", MaxLineBytes+1),
		"Host good",
		"  HostName=good.example",
		"  port = 2022",
		"  USER   \"quoted user\"",
	}, "\n")}))
	b := host(t, res, "bad")
	if b.Port != 22 || b.HostName != "bad" || b.User != "me" {
		t.Fatalf("bad = %+v", b)
	}
	g := host(t, res, "good")
	if g.HostName != "good.example" || g.Port != 2022 || g.User != "quoted user" {
		t.Fatalf("good = %+v", g)
	}
	for _, want := range []string{"Host without a name", "HostName without a value", "unterminated quote", "line has no keyword", "IdentityFile without a value", "longer than"} {
		if !hasNote(res, want) {
			t.Errorf("missing note %q in %+v", want, res.Notes)
		}
	}
	if len(b.Notes) == 0 || !strings.Contains(b.Notes[0], "not a port number") {
		t.Fatalf("port note = %+v", b.Notes)
	}
}

func TestCRLF(t *testing.T) {
	res := parse(t, home(t, map[string]string{"config": "Host win\r\n  HostName win.example\r\n  User ops\r\n  Port 2200\r\n"}))
	h := host(t, res, "win")
	if h.HostName != "win.example" || h.User != "ops" || h.Port != 2200 {
		t.Fatalf("crlf = %+v", h)
	}
}

func TestHugeFilesAreSkipped(t *testing.T) {
	big := "Host huge\n" + strings.Repeat("# filler\n", DefaultMaxFileBytes/9+10)
	res := parse(t, home(t, map[string]string{"config": big}))
	if len(res.Hosts) != 0 || !res.Found || !hasNote(res, "larger than 256 KiB") {
		t.Fatalf("huge = %+v", res.Notes)
	}
	res = parse(t, home(t, map[string]string{"config": "Include big\nHost small\n", "big": big}))
	if got := strings.Join(aliases(res), ","); got != "small" {
		t.Fatalf("aliases = %s", got)
	}
}

func TestHostLimit(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < DefaultMaxHosts+20; i++ {
		fmt.Fprintf(&sb, "Host h%d\n", i)
	}
	res := parse(t, home(t, map[string]string{"config": sb.String()}))
	if len(res.Hosts) != DefaultMaxHosts || !hasNote(res, "more than 1000 hosts") {
		t.Fatalf("hosts = %d", len(res.Hosts))
	}
}

func TestTokens(t *testing.T) {
	dir := home(t, map[string]string{"config": `
Host tok
  HostName %h.example.com
  IdentityFile %d/.ssh/id_%h
  IdentityFile ~/.ssh/key_%u
  IdentityFile ${HOME}/.ssh/env
  IdentityFile ~root/.ssh/id
Host weird/../x
  IdentityFile ~/.ssh/id_%h
Host odd
  HostName %r.example
`})
	res := parse(t, dir)
	h := host(t, res, "tok")
	home := filepath.Dir(dir)
	if h.HostName != "tok.example.com" {
		t.Fatalf("hostname = %q", h.HostName)
	}
	if len(h.IdentityFiles) != 1 || h.IdentityFiles[0] != filepath.Join(home, ".ssh", "id_tok") {
		t.Fatalf("identity = %+v", h.IdentityFiles)
	}
	if len(h.Notes) < 3 {
		t.Fatalf("token notes = %+v", h.Notes)
	}
	if w := host(t, res, "weird/../x"); len(w.IdentityFiles) != 0 {
		t.Fatalf("%%h with an unsafe alias must not be expanded: %+v", w.IdentityFiles)
	}
	if o := host(t, res, "odd"); o.HostName != "%r.example" || len(o.Notes) == 0 {
		t.Fatalf("odd = %+v", o)
	}
}

// ~ is the parent of SSHDir, never the real home of whoever runs the test.
func TestTildeIsParentOfSSHDir(t *testing.T) {
	dir := home(t, map[string]string{"config": "Host t\n  IdentityFile ~/k\n"})
	h := host(t, parse(t, dir), "t")
	if h.IdentityFiles[0] != filepath.Join(filepath.Dir(dir), "k") {
		t.Fatalf("identity = %v", h.IdentityFiles)
	}
	if real, _ := os.UserHomeDir(); real != "" && strings.HasPrefix(h.IdentityFiles[0], real+string(filepath.Separator)+".ssh") {
		t.Fatal("~ reached the real home")
	}
}

func TestGlobMatch(t *testing.T) {
	cases := []struct {
		pat, s string
		ok     bool
	}{
		{"*", "anything", true},
		{"*.prod", "db.prod", true},
		{"*.prod", "db.prod.x", false},
		{"web-?", "web-1", true},
		{"web-?", "web-12", false},
		{"a*b*c", "axxbyyc", true},
		{"a*b*c", "axxbyy", false},
		{"", "", true},
	}
	for _, c := range cases {
		if got := globMatch(c.pat, c.s); got != c.ok {
			t.Errorf("globMatch(%q,%q) = %v", c.pat, c.s, got)
		}
	}
}

// --- keys ---

type fileState struct {
	data []byte
	mode os.FileMode
	mod  time.Time
}

func stateOf(t *testing.T, path string) fileState {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fileState{data: b, mode: fi.Mode(), mod: fi.ModTime()}
}

func sameState(t *testing.T, path string, before fileState) {
	t.Helper()
	after := stateOf(t, path)
	if string(after.data) != string(before.data) || after.mode != before.mode || !after.mod.Equal(before.mod) {
		t.Fatalf("%s changed: mode %v -> %v", path, before.mode, after.mode)
	}
}

// writeTestKey writes an ed25519 private key in OpenSSH format.
func writeTestKey(t *testing.T, path, passphrase string, mode os.FileMode) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var blk *pem.Block
	if passphrase == "" {
		blk, err = ssh.MarshalPrivateKey(priv, "test")
	} else {
		blk, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "test", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(blk), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestKeysInspectAndLoadLeaveOriginalsUntouched(t *testing.T) {
	dir := home(t, map[string]string{"config": "Host a\n  IdentityFile ~/.ssh/plain\n  IdentityFile ~/.ssh/locked\n  IdentityFile ~/.ssh/junk\n  IdentityFile ~/.ssh/gone\n"})
	plain := filepath.Join(dir, "plain")
	locked := filepath.Join(dir, "locked")
	junk := filepath.Join(dir, "junk")
	writeTestKey(t, plain, "", 0o600)
	writeTestKey(t, locked, "s3cret", 0o400)
	if err := os.WriteFile(junk, []byte("not a key"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := map[string]fileState{}
	for _, p := range []string{plain, locked, junk, filepath.Join(dir, "config")} {
		before[p] = stateOf(t, p)
	}
	// Give mtimes room to differ if anything wrote.
	time.Sleep(20 * time.Millisecond)

	res := parse(t, dir)
	if got := len(res.KeyPaths()); got != 4 {
		t.Fatalf("key paths = %d", got)
	}
	if k := InspectKey(plain, "~/.ssh/plain"); !k.Exists || k.Encrypted || k.Problem != "" {
		t.Fatalf("plain = %+v", k)
	}
	if k := InspectKey(locked, "~/.ssh/locked"); !k.Encrypted || k.Problem != "" {
		t.Fatalf("locked = %+v", k)
	}
	if k := InspectKey(junk, ""); k.Problem == "" {
		t.Fatalf("junk = %+v", k)
	}
	if k := InspectKey(filepath.Join(dir, "gone"), ""); k.Exists || k.Problem != "file not found" {
		t.Fatalf("gone = %+v", k)
	}
	if b, err := LoadKey(plain, ""); err != nil || !strings.Contains(string(b), "OPENSSH PRIVATE KEY") {
		t.Fatalf("load plain: %v", err)
	}
	if _, err := LoadKey(locked, ""); !errors.Is(err, ErrKeyPassphrase) {
		t.Fatalf("locked without passphrase: %v", err)
	}
	if _, err := LoadKey(locked, "wrong"); !errors.Is(err, ErrKeyWrongPass) {
		t.Fatalf("locked with wrong passphrase: %v", err)
	}
	if _, err := LoadKey(locked, "s3cret"); err != nil {
		t.Fatalf("locked with passphrase: %v", err)
	}
	if _, err := LoadKey(junk, ""); err == nil {
		t.Fatal("junk loaded")
	}
	for p, st := range before {
		sameState(t, p, st)
	}
}

func TestKeyTooLargeOrNotRegular(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big")
	if err := os.WriteFile(big, make([]byte, MaxKeyBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadKeyFile(big); err == nil {
		t.Fatal("big key read")
	}
	if _, err := ReadKeyFile(dir); err == nil {
		t.Fatal("directory read as key")
	}
}
