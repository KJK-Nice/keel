// Package hostimport is the first-run host import (docs/ui-redesign.md
// §5.15): it reads ~/.ssh/config, says what it holds, and adds the hosts
// the user picks to the vault. The config and the key files are only ever
// read. A private key is copied into the vault, as a Keychain identity,
// only when the user turns that key on; its passphrase, if any, goes into
// the vault with it.
//
// httpapi serves it; this package knows nothing of HTTP.
package hostimport

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"keel/internal/sshconfig"
	"keel/internal/store"
)

// ErrNotSetUp is returned when keel serve has no ssh folder to read.
var ErrNotSetUp = errors.New("ssh config import is not set up")

// Host is one config host as the import screen lists it.
type Host struct {
	sshconfig.Host
	// Saved is true when a host with the same address, user and port is
	// already in the vault; the UI leaves it unchecked.
	Saved bool `json:"saved"`
	// JumpAlias is the config host this host's ProxyJump names; it becomes
	// the jump host when that host is added too. JumpSaved is the label of
	// the one saved host that matches the jump's user, address and port,
	// used when JumpAlias is not added. JumpVia is that user@address:port.
	// JumpProblem says why the ProxyJump cannot be kept, if it cannot.
	JumpAlias   string `json:"jumpAlias,omitempty"`
	JumpSaved   string `json:"jumpSaved,omitempty"`
	JumpVia     string `json:"jumpVia,omitempty"`
	JumpProblem string `json:"jumpProblem,omitempty"`
}

// Key is one private key the config names, and the hosts that use it.
type Key struct {
	sshconfig.Key
	UsedBy []string `json:"usedBy"`
}

// Preview is what the config holds, before anything is added.
type Preview struct {
	Path     string              `json:"path"`
	Found    bool                `json:"found"`
	Hosts    []Host              `json:"hosts"`
	Patterns []sshconfig.Pattern `json:"patterns"`
	Keys     []Key               `json:"keys"`
	Notes    []sshconfig.Note    `json:"notes"`
}

// jumpPlan is where a ProxyJump host's jump comes from on import.
type jumpPlan struct {
	alias   string // a config host, used when it is imported too
	savedID string // a saved host, used otherwise
	saved   string // its label
	via     string // user@address:port of the jump
	problem string // why it cannot be kept
}

// linked reports whether the plan gives h a jump host when the hosts in
// importing are added.
func (p jumpPlan) linked(importing map[string]bool) bool {
	return p.savedID != "" || (p.alias != "" && importing[p.alias])
}

// planJump resolves h's ProxyJump to a config alias and/or a saved host.
// The jump target must not need a proxy itself (one hop only). A saved
// host is used only when exactly one matches the jump's user, address and
// port: its credentials go to that jump, so a host for another user on the
// same address must never be picked. A ProxyJump without a user means the
// local user, as ssh does.
func planJump(h sshconfig.Host, byAlias map[string]sshconfig.Host, saved []store.Host, localUser string) jumpPlan {
	if h.Proxy != "ProxyJump" {
		return jumpPlan{}
	}
	j, ok := sshconfig.ParseJump(h.ProxyJump)
	if !ok {
		return jumpPlan{problem: "only a single ProxyJump hop can be kept"}
	}
	var out jumpPlan
	addr, user, port := j.Host, j.User, j.Port
	for alias, c := range byAlias {
		if !strings.EqualFold(alias, j.Host) || strings.EqualFold(alias, h.Alias) {
			continue
		}
		if c.Proxy != "" {
			return jumpPlan{problem: c.Alias + " needs a proxy itself; only one hop is supported"}
		}
		out.alias = c.Alias
		addr = c.HostName
		if user == "" {
			user = c.User
		}
		if port == 0 {
			port = c.Port
		}
		break
	}
	if user == "" {
		user = localUser
	}
	if port == 0 {
		port = 22
	}
	out.via = user + "@" + net.JoinHostPort(addr, strconv.Itoa(port))
	var matches []store.Host
	for _, sh := range saved {
		if sh.AuthType == "local" || strings.TrimSpace(sh.JumpHost) != "" {
			continue
		}
		p := sh.Port
		if p == 0 {
			p = 22
		}
		if strings.EqualFold(strings.TrimSpace(sh.Address), addr) && p == port && sh.Username == user {
			matches = append(matches, sh)
		}
	}
	switch {
	case len(matches) == 1:
		out.savedID, out.saved = matches[0].ID, matches[0].Name
	case len(matches) > 1 && out.alias == "":
		out.problem = "several saved hosts are " + out.via + "; pick the jump host after import"
	case len(matches) == 0 && out.alias == "":
		out.problem = "no saved host is " + out.via
	}
	return out
}

// BuildPreview parses the config in opts and compares it with the vault.
func BuildPreview(st *store.Store, opts sshconfig.Options) (Preview, error) {
	p, _, err := buildPreview(st, opts)
	return p, err
}

func buildPreview(st *store.Store, opts sshconfig.Options) (Preview, sshconfig.Result, error) {
	if opts.SSHDir == "" {
		return Preview{}, sshconfig.Result{}, ErrNotSetUp
	}
	res, err := sshconfig.Parse(opts)
	if err != nil {
		return Preview{}, res, err
	}
	saved, err := st.ListHosts()
	if err != nil {
		return Preview{}, res, err
	}
	out := Preview{
		Path:     res.Path,
		Found:    res.Found,
		Hosts:    make([]Host, 0, len(res.Hosts)),
		Patterns: res.Patterns,
		Keys:     []Key{},
		Notes:    res.Notes,
	}
	byAlias := map[string]sshconfig.Host{}
	for _, h := range res.Hosts {
		byAlias[h.Alias] = h
	}
	for _, h := range res.Hosts {
		plan := planJump(h, byAlias, saved, opts.LocalUser)
		out.Hosts = append(out.Hosts, Host{Host: h, Saved: alreadySaved(saved, h), JumpAlias: plan.alias, JumpSaved: plan.saved, JumpVia: plan.via, JumpProblem: plan.problem})
	}
	for _, path := range res.KeyPaths() {
		k := Key{Key: sshconfig.InspectKey(path, sshconfig.DisplayPath(opts, path)), UsedBy: []string{}}
		for _, h := range res.Hosts {
			for _, p := range h.IdentityFiles {
				if p == path {
					k.UsedBy = append(k.UsedBy, h.Alias)
				}
			}
		}
		out.Keys = append(out.Keys, k)
	}
	return out, res, nil
}

func alreadySaved(saved []store.Host, h sshconfig.Host) bool {
	for _, s := range saved {
		if s.AuthType == "local" {
			continue
		}
		port := s.Port
		if port == 0 {
			port = 22
		}
		if strings.EqualFold(strings.TrimSpace(s.Address), h.HostName) && s.Username == h.User && port == h.Port {
			return true
		}
	}
	return false
}

// Request names what the user picked. It carries no addresses or paths of
// its own: Import parses the config again, so a request cannot make keel
// serve read a file the config does not name.
type Request struct {
	Hosts []string `json:"hosts"`
	// WithoutJump names ProxyJump hosts the user chose to add without
	// their jump host, as a direct host. Any other ProxyJump host whose
	// jump cannot be kept is refused.
	WithoutJump []string     `json:"withoutJump"`
	Keys        []KeyRequest `json:"keys"`
}

// KeyRequest turns on one key, with its passphrase if it has one.
type KeyRequest struct {
	Path       string `json:"path"`
	Passphrase string `json:"passphrase"`
}

// Failure is a host that was picked but not added.
type Failure struct {
	Alias string `json:"alias"`
	Error string `json:"error"`
}

// Result is what Import added. Hosts and Identities are as the store
// returned them: the caller redacts them before they leave keel serve.
type Result struct {
	Path       string
	Hosts      []store.Host
	Identities []store.Identity
	Failed     []Failure
	Jumps      int // hosts added behind a jump host
}

// RequestError is a request that names something the config does not.
type RequestError struct{ Msg string }

func (e *RequestError) Error() string { return e.Msg }

// JumpError lists ProxyJump hosts whose jump host cannot be kept and that
// the user did not pick to add without it. Nothing was saved.
type JumpError struct{ Hosts []string }

func (e *JumpError) Error() string {
	return "Keel cannot keep the jump host of " + strings.Join(e.Hosts, ", ") + ": add its jump host too, or tick Without the jump to add it as a direct host"
}

// KeyError is a key that could not be read. Kind is "passphrase" (one is
// needed), "wrongPassphrase" or "unreadable". Nothing was saved.
type KeyError struct {
	Kind string
	Path string
	Msg  string
}

func (e *KeyError) Error() string { return e.Msg }

// Import adds the hosts and keys req picks. Every check (unknown names,
// jump hosts, key passphrases) runs before anything is saved, so a refused
// request leaves the vault as it was. Errors from the store are returned
// as they are.
func Import(st *store.Store, opts sshconfig.Options, req Request) (Result, error) {
	preview, res, err := buildPreview(st, opts)
	if err != nil {
		return Result{}, err
	}
	byAlias := map[string]sshconfig.Host{}
	for _, h := range res.Hosts {
		byAlias[h.Alias] = h
	}
	known := map[string]sshconfig.Key{}
	for _, k := range preview.Keys {
		known[k.Path] = k.Key
	}
	var hosts []sshconfig.Host
	seen := map[string]bool{}
	for _, a := range req.Hosts {
		h, ok := byAlias[a]
		if !ok {
			return Result{}, &RequestError{fmt.Sprintf("%s is not a host in %s", a, preview.Path)}
		}
		if !seen[a] {
			seen[a] = true
			hosts = append(hosts, h)
		}
	}
	// A ProxyJump host keeps its jump host, or is added direct only when
	// the user ticked that. Checked before anything is saved.
	savedHosts, err := st.ListHosts()
	if err != nil {
		return Result{}, err
	}
	importing := map[string]bool{}
	for _, h := range hosts {
		importing[h.Alias] = true
	}
	direct := map[string]bool{}
	for _, a := range req.WithoutJump {
		direct[a] = true
	}
	plans := map[string]jumpPlan{}
	var unlinked []string
	for _, h := range hosts {
		plan := planJump(h, byAlias, savedHosts, opts.LocalUser)
		plans[h.Alias] = plan
		if h.Proxy == "ProxyJump" && !plan.linked(importing) && !direct[h.Alias] {
			unlinked = append(unlinked, h.Alias)
		}
	}
	if len(unlinked) > 0 {
		return Result{}, &JumpError{Hosts: unlinked}
	}
	// Check every key before anything is saved, so a wrong passphrase
	// leaves the vault as it was.
	type loadedKey struct {
		key        sshconfig.Key
		pem        []byte
		passphrase string
	}
	var keys []loadedKey
	keySeen := map[string]bool{}
	for _, rk := range req.Keys {
		k, ok := known[rk.Path]
		if !ok {
			return Result{}, &RequestError{"that key is not named by the ssh config"}
		}
		if keySeen[rk.Path] {
			continue
		}
		keySeen[rk.Path] = true
		pem, err := sshconfig.LoadKey(rk.Path, rk.Passphrase)
		if err != nil {
			kind := "unreadable"
			switch {
			case errors.Is(err, sshconfig.ErrKeyPassphrase):
				kind = "passphrase"
			case errors.Is(err, sshconfig.ErrKeyWrongPass):
				kind = "wrongPassphrase"
			}
			return Result{}, &KeyError{Kind: kind, Path: rk.Path, Msg: fmt.Sprintf("%s: %v", k.Display, err)}
		}
		pass := rk.Passphrase
		if !k.Encrypted {
			pass = ""
		}
		keys = append(keys, loadedKey{key: k, pem: pem, passphrase: pass})
	}

	existing, err := st.ListIdentities()
	if err != nil {
		return Result{}, err
	}
	names := map[string]bool{}
	for _, id := range existing {
		names[id.Name] = true
	}
	out := Result{Path: preview.Path, Hosts: []store.Host{}, Identities: []store.Identity{}, Failed: []Failure{}}
	identityFor := map[string]string{}
	for _, k := range keys {
		name := uniqueName(k.key.Name, names)
		names[name] = true
		saved, err := st.UpsertIdentity(store.Identity{
			Name:       name,
			Kind:       "key",
			PrivateKey: string(k.pem),
			Passphrase: k.passphrase,
		})
		if err != nil {
			return Result{}, err
		}
		identityFor[k.key.Path] = saved.ID
		out.Identities = append(out.Identities, saved)
	}
	// Hosts that need no jump host first, so a jump host added in this
	// request has its id when the hosts behind it are saved.
	newID := map[string]string{}
	for pass := 0; pass < 2; pass++ {
		for _, h := range hosts {
			plan := plans[h.Alias]
			behind := plan.linked(importing) && !direct[h.Alias]
			if behind != (pass == 1) {
				continue
			}
			in := store.Host{
				Name:     h.Alias,
				Address:  h.HostName,
				Port:     h.Port,
				Username: h.User,
				AuthType: "password",
			}
			for _, p := range h.IdentityFiles {
				if id, ok := identityFor[p]; ok {
					in.AuthType = "identity"
					in.IdentityID = id
					break
				}
			}
			if behind {
				in.JumpHost = newID[plan.alias]
				if in.JumpHost == "" {
					in.JumpHost = plan.savedID
				}
				if in.JumpHost == "" {
					out.Failed = append(out.Failed, Failure{h.Alias, "its jump host " + plan.alias + " was not added"})
					continue
				}
			}
			saved, err := st.UpsertHost(in)
			if err != nil {
				if behind && errors.Is(err, store.ErrHostField) {
					out.Failed = append(out.Failed, Failure{h.Alias, err.Error()})
					continue
				}
				return Result{}, err
			}
			if behind {
				out.Jumps++
			}
			newID[h.Alias] = saved.ID
			out.Hosts = append(out.Hosts, saved)
		}
	}
	return out, nil
}

func uniqueName(base string, taken map[string]bool) string {
	if base == "" {
		base = "key"
	}
	if !taken[base] {
		return base
	}
	for i := 2; ; i++ {
		n := fmt.Sprintf("%s (%d)", base, i)
		if !taken[n] {
			return n
		}
	}
}
