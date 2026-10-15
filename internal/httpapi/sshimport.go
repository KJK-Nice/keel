package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"keel/internal/sshconfig"
	"keel/internal/store"
)

// First-run host import (docs/ui-redesign.md §5.15): read ~/.ssh/config
// (KEEL_SSH_DIR in keel serve), show what it holds, and add the hosts the
// user picks. The config and the key files are only ever read. A private
// key is copied into the vault, as a Keychain identity, only when the user
// turns that key on; its passphrase, if any, goes into the vault with it.

// WithSSHDir sets the folder that holds the ssh config to import (~/.ssh).
func (s *Server) WithSSHDir(dir string) *Server {
	s.sshDir = dir
	return s
}

func (s *Server) sshOptions() (sshconfig.Options, bool) {
	if s.sshDir == "" {
		return sshconfig.Options{}, false
	}
	return sshconfig.Options{SSHDir: s.sshDir, LocalUser: localUser()}, true
}

func localUser() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return ""
}

type importHost struct {
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

type importKey struct {
	sshconfig.Key
	UsedBy []string `json:"usedBy"`
}

type importPreview struct {
	Path     string              `json:"path"`
	Found    bool                `json:"found"`
	Hosts    []importHost        `json:"hosts"`
	Patterns []sshconfig.Pattern `json:"patterns"`
	Keys     []importKey         `json:"keys"`
	Notes    []sshconfig.Note    `json:"notes"`
}

func (s *Server) buildPreview() (importPreview, sshconfig.Result, error) {
	opts, ok := s.sshOptions()
	if !ok {
		return importPreview{}, sshconfig.Result{}, errors.New("ssh config import is not set up")
	}
	res, err := sshconfig.Parse(opts)
	if err != nil {
		return importPreview{}, res, err
	}
	saved, err := s.store.ListHosts()
	if err != nil {
		return importPreview{}, res, err
	}
	out := importPreview{
		Path:     res.Path,
		Found:    res.Found,
		Hosts:    make([]importHost, 0, len(res.Hosts)),
		Patterns: res.Patterns,
		Keys:     []importKey{},
		Notes:    res.Notes,
	}
	byAlias := map[string]sshconfig.Host{}
	for _, h := range res.Hosts {
		byAlias[h.Alias] = h
	}
	for _, h := range res.Hosts {
		plan := planJump(h, byAlias, saved, opts.LocalUser)
		out.Hosts = append(out.Hosts, importHost{Host: h, Saved: alreadySaved(saved, h), JumpAlias: plan.alias, JumpSaved: plan.saved, JumpVia: plan.via, JumpProblem: plan.problem})
	}
	for _, path := range res.KeyPaths() {
		k := importKey{Key: sshconfig.InspectKey(path, sshconfig.DisplayPath(opts, path)), UsedBy: []string{}}
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

func (s *Server) sshConfigPreview(w http.ResponseWriter, _ *http.Request) {
	out, _, err := s.buildPreview()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type importRequest struct {
	Hosts []string `json:"hosts"`
	// WithoutJump names ProxyJump hosts the user chose to add without
	// their jump host, as a direct host. Any other ProxyJump host whose
	// jump cannot be kept is refused.
	WithoutJump []string `json:"withoutJump"`
	Keys        []struct {
		Path       string `json:"path"`
		Passphrase string `json:"passphrase"`
	} `json:"keys"`
}

func (s *Server) sshConfigImport(w http.ResponseWriter, r *http.Request) {
	var req importRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid import payload")
		return
	}
	// Parse again: the client only names hosts and keys; addresses and key
	// paths come from the config, so a request cannot make keel serve read
	// a file the config does not name.
	preview, res, err := s.buildPreview()
	if err != nil {
		writeStoreErr(w, err)
		return
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
			writeError(w, http.StatusBadRequest, fmt.Sprintf("%s is not a host in %s", a, preview.Path))
			return
		}
		if !seen[a] {
			seen[a] = true
			hosts = append(hosts, h)
		}
	}
	// A ProxyJump host keeps its jump host, or is added direct only when
	// the user ticked that. Checked before anything is saved.
	savedHosts, err := s.store.ListHosts()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	opts, _ := s.sshOptions()
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
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":     "Keel cannot keep the jump host of " + strings.Join(unlinked, ", ") + ": add its jump host too, or tick Without the jump to add it as a direct host",
			"errorKind": "jump",
			"hosts":     unlinked,
		})
		return
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
			writeError(w, http.StatusBadRequest, "that key is not named by the ssh config")
			return
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
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error":     fmt.Sprintf("%s: %v", k.Display, err),
				"errorKind": kind,
				"key":       rk.Path,
			})
			return
		}
		pass := rk.Passphrase
		if !k.Encrypted {
			pass = ""
		}
		keys = append(keys, loadedKey{key: k, pem: pem, passphrase: pass})
	}

	existing, err := s.store.ListIdentities()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	names := map[string]bool{}
	for _, id := range existing {
		names[id.Name] = true
	}
	identityFor := map[string]string{}
	outIDs := []store.Identity{}
	for _, k := range keys {
		name := uniqueName(k.key.Name, names)
		names[name] = true
		saved, err := s.store.UpsertIdentity(store.Identity{
			Name:       name,
			Kind:       "key",
			PrivateKey: string(k.pem),
			Passphrase: k.passphrase,
		})
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		identityFor[k.key.Path] = saved.ID
		outIDs = append(outIDs, redactIdentity(saved))
	}
	// Hosts that need no jump host first, so a jump host added in this
	// request has its id when the hosts behind it are saved.
	newID := map[string]string{}
	outHosts := []store.Host{}
	type failure struct {
		Alias string `json:"alias"`
		Error string `json:"error"`
	}
	failed := []failure{}
	jumps := 0
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
					failed = append(failed, failure{h.Alias, "its jump host " + plan.alias + " was not added"})
					continue
				}
			}
			saved, err := s.store.UpsertHost(in)
			if err != nil {
				if behind && errors.Is(err, store.ErrHostField) {
					failed = append(failed, failure{h.Alias, err.Error()})
					continue
				}
				writeStoreErr(w, err)
				return
			}
			if behind {
				jumps++
			}
			newID[h.Alias] = saved.ID
			outHosts = append(outHosts, redactHost(saved))
		}
	}
	for _, f := range failed {
		s.note("vault", "host import: "+f.Alias+" not added: "+f.Error, "", "", "")
	}
	s.note("vault", fmt.Sprintf("imported %d hosts (%d through a jump host) and %d keys from %s", len(outHosts), jumps, len(outIDs), preview.Path), "", "", "")
	writeJSON(w, http.StatusOK, map[string]any{"hosts": outHosts, "identities": outIDs, "failed": failed})
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

// vaultState is what the unlock and first-run screens need. It is served
// while the vault is locked (withVault lets it through), so it must stay
// free of anything the vault protects.
func (s *Server) vaultState(w http.ResponseWriter, _ *http.Request) {
	st := s.store.VaultState()
	st.Path = homeShort(st.Path)
	writeJSON(w, http.StatusOK, map[string]any{
		"locked":     st.Locked,
		"needsSetup": st.NeedsSetup,
		"mode":       st.Mode,
		"firstRun":   st.FirstRun,
		"keyringOff": st.KeyringOff,
		"path":       st.Path,
		"sessions":   s.rt.Count(),
		"sshConfig":  s.sshConfigDisplay(),
	})
}

func (s *Server) sshConfigDisplay() string {
	opts, ok := s.sshOptions()
	if !ok {
		return ""
	}
	return sshconfig.DisplayPath(opts, filepath.Join(opts.SSHDir, "config"))
}

func homeShort(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + path[len(home):]
	}
	return path
}

func (s *Server) finishFirstRun(w http.ResponseWriter, _ *http.Request) {
	if err := s.store.FinishFirstRun(); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
