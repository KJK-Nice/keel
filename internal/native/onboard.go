package native

import (
	"fmt"
	"path/filepath"
	"strings"

	"keel/internal/limits"
	"keel/internal/native/keelclient"
)

// The host import's rules (docs/ui-redesign.md §5.15): which hosts start ticked, what the table says about each,
// what the request carries and the words around it. They are cmd/keel/web/js/lib/onboard.js, function by
// function, with the cases of test/onboard.test.mjs in onboard_test.go. Nothing here reads a file or a key: keel
// serve parses the ssh config and copies a key into the vault only when the request names it.

// keyState is one key's copy switch and the passphrase typed for it. String leaves the passphrase out.
type keyState struct {
	on   bool
	pass string
}

func (k keyState) String() string { return fmt.Sprintf("{on=%v}", k.on) }

func (k keyState) GoString() string { return "keyState" + k.String() }

// importSel is what the user has ticked: hosts by alias, keys by path, and the ProxyJump hosts to add without
// their jump.
type importSel struct {
	hosts  map[string]bool
	keys   map[string]*keyState
	noJump map[string]bool
}

func (s *importSel) String() string {
	return fmt.Sprintf("importSel{%d hosts %d keys}", len(s.hosts), len(s.keys))
}

func (s *importSel) GoString() string { return s.String() }

// clearSecrets forgets every typed key passphrase.
func (s *importSel) clearSecrets() {
	for _, k := range s.keys {
		k.pass = ""
	}
}

// defaultSelection: every host that is not saved yet and needs no proxy is on; saved hosts and proxy hosts are
// off (the user may still turn them on). Every key copy is off: copying a private key is always opt-in.
func defaultSelection(p keelclient.SSHPreview) *importSel {
	s := &importSel{hosts: map[string]bool{}, keys: map[string]*keyState{}, noJump: map[string]bool{}}
	for _, h := range p.Hosts {
		if !h.Saved && h.Proxy == "" {
			s.hosts[h.Alias] = true
		}
	}
	for _, k := range p.Keys {
		s.keys[k.Path] = &keyState{}
	}
	return s
}

// keyOf returns the state of the key at path, made when there is none.
func (s *importSel) keyOf(path string) *keyState {
	k := s.keys[path]
	if k == nil {
		k = &keyState{}
		s.keys[path] = k
	}
	return k
}

func hostAddress(h keelclient.ConfigHost) string {
	addr := h.HostName
	if addr == "" {
		addr = h.Alias
	}
	if h.Port != 0 && h.Port != limits.DefaultPort {
		return fmt.Sprintf("%s:%d", addr, h.Port)
	}
	return addr
}

// copyable is false for a key keel serve cannot read (missing, too large, not a private key): its switch is
// disabled.
func copyable(k keelclient.ConfigKey) bool { return k.Exists && k.Problem == "" }

func leadText(p keelclient.SSHPreview) string {
	path := p.Path
	if path == "" {
		path = "~/.ssh/config"
	}
	if !p.Found {
		return "Keel found no " + path + ". Add hosts later with New host."
	}
	return fmt.Sprintf("Keel found %s with %s. Pick the ones to add. Your config file is not changed.", path, countWord(len(p.Hosts), "host", "hosts"))
}

func countWord(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func selectedCount(p keelclient.SSHPreview, s *importSel) (n int) {
	for _, h := range p.Hosts {
		if s.hosts[h.Alias] {
			n++
		}
	}
	return n
}

func addLabel(n int) string {
	switch n {
	case 0:
		return "Add hosts"
	case 1:
		return "Add 1 host"
	}
	return fmt.Sprintf("Add %d hosts", n)
}

func summaryText(p keelclient.SSHPreview, s *importSel) string {
	return fmt.Sprintf("%d of %d selected", selectedCount(p, s), len(p.Hosts))
}

// checkState is a checkbox: off, on, or mixed (the select-all box with some hosts ticked).
type checkState int

const (
	checkOff checkState = iota
	checkOn
	checkMixed
)

func allState(p keelclient.SSHPreview, s *importSel) checkState {
	switch n := selectedCount(p, s); {
	case n == 0:
		return checkOff
	case n == len(p.Hosts):
		return checkOn
	}
	return checkMixed
}

// jumpKept: the host's one-hop ProxyJump becomes its Keel jump host with this selection (a saved jump host, or its
// config jump host being added).
func jumpKept(h keelclient.ConfigHost, s *importSel) bool {
	return h.Proxy == "ProxyJump" && (h.JumpSaved != "" || (h.JumpAlias != "" && s.hosts[h.JumpAlias]))
}

// via is the Via cell of a host: what to say about its proxy.
type via struct {
	kind    string // "" no proxy, "saved" (tide), "if-added" (haze), "unused" (signal)
	text    string // the badge text
	tip     string // its tooltip
	noJump  bool   // draw the "Without the jump" checkbox (a ProxyJump that is not kept)
	command bool   // ProxyCommand: no checkbox, only the flag
}

func viaOf(h keelclient.ConfigHost) via {
	if h.Proxy == "" {
		return via{}
	}
	at := ""
	if h.JumpVia != "" {
		at = " (" + h.JumpVia + ")"
	}
	// A one-hop ProxyJump to a host that is saved, or added in this import, becomes the host's jump host.
	if h.Proxy == "ProxyJump" && h.JumpSaved != "" {
		return via{kind: "saved", text: "Via " + h.JumpSaved, tip: "Connects through saved host " + h.JumpSaved + at}
	}
	v := via{}
	if h.Proxy == "ProxyJump" && h.JumpAlias != "" {
		v = via{kind: "if-added", text: "Via " + h.JumpAlias + " if added", tip: "Connects through " + h.JumpAlias + at + " when you add it too"}
	} else {
		what := h.Proxy
		if h.Proxy == "ProxyJump" && h.ProxyJump != "" {
			what = "ProxyJump " + h.ProxyJump
		}
		why := "Keel does not support it"
		if h.JumpProblem != "" {
			why = "Keel cannot keep it: " + h.JumpProblem
		}
		v = via{kind: "unused", text: what + ", not used", tip: what + ": " + why}
	}
	// Adding a ProxyJump host without its jump host connects to it directly: only when the user ticks that.
	v.noJump = h.Proxy == "ProxyJump"
	v.command = !v.noJump
	return v
}

// keyNames is the Key cell: the base names of the host's identity files.
func keyNames(h keelclient.ConfigHost) string {
	var names []string
	for _, f := range h.IdentityFiles {
		names = append(names, baseName(f))
	}
	return strings.Join(names, ", ")
}

// baseName is the last element of a path written with either separator.
func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return filepath.Base(p)
}

// noteLine is a note of the config's or of a host's.
type noteLine struct{ source, text string }

// notesList merges the config's notes with the hosts' own (bad Port, unexpanded tokens), proxy notes aside: the
// Via column shows those.
func notesList(p keelclient.SSHPreview) []noteLine {
	var out []noteLine
	for _, n := range p.Notes {
		out = append(out, noteLine{n.Source, n.Text})
	}
	for _, h := range p.Hosts {
		for _, t := range h.Notes {
			if h.Proxy != "" && strings.HasPrefix(t, h.Proxy) {
				continue
			}
			out = append(out, noteLine{h.Alias, t})
		}
	}
	return out
}

// importRequest is the body of POST /api/ssh-config/import: host aliases and the keys to copy. Only keys that are
// on and copyable are sent, and a passphrase only for a key that has one.
func importRequest(p keelclient.SSHPreview, s *importSel) keelclient.ImportRequest {
	req := keelclient.ImportRequest{Hosts: []string{}, Keys: []keelclient.ImportKey{}}
	for _, h := range p.Hosts {
		if s.hosts[h.Alias] {
			req.Hosts = append(req.Hosts, h.Alias)
		}
	}
	for _, k := range p.Keys {
		st := s.keys[k.Path]
		if st == nil || !st.on || !copyable(k) {
			continue
		}
		ik := keelclient.ImportKey{Path: k.Path}
		if k.Encrypted {
			ik.Passphrase = st.pass
		}
		req.Keys = append(req.Keys, ik)
	}
	for _, h := range p.Hosts {
		if s.hosts[h.Alias] && h.Proxy == "ProxyJump" && s.noJump[h.Alias] && !jumpKept(h, s) {
			req.WithoutJump = append(req.WithoutJump, h.Alias)
		}
	}
	return req
}

// missingJump is the first chosen ProxyJump host whose jump host is neither kept nor waived with Without the jump.
func missingJump(p keelclient.SSHPreview, s *importSel) *keelclient.ConfigHost {
	for i, h := range p.Hosts {
		if s.hosts[h.Alias] && h.Proxy == "ProxyJump" && !jumpKept(h, s) && !s.noJump[h.Alias] {
			return &p.Hosts[i]
		}
	}
	return nil
}

// missingPassphrase is the index of the first key turned on whose passphrase is still empty, or -1.
func missingPassphrase(p keelclient.SSHPreview, s *importSel) int {
	for i, k := range p.Keys {
		if st := s.keys[k.Path]; st != nil && st.on && copyable(k) && k.Encrypted && st.pass == "" {
			return i
		}
	}
	return -1
}

func importedText(r keelclient.ImportResult) string {
	hosts := countWord(len(r.Hosts), "host", "hosts")
	notAdded := ""
	if len(r.Failed) > 0 {
		parts := make([]string, len(r.Failed))
		for i, f := range r.Failed {
			parts[i] = f.Alias + " (" + f.Error + ")"
		}
		notAdded = ". Not added: " + strings.Join(parts, ", ")
	}
	if len(r.Identities) == 0 {
		return "Added " + hosts + notAdded
	}
	return "Added " + hosts + " and " + countWord(len(r.Identities), "key", "keys") + notAdded
}
