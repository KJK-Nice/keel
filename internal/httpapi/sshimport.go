package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
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
	// the saved host it matches, used when JumpAlias is not added. Both
	// empty: the ProxyJump cannot be kept (more than one hop, a jump host
	// that needs a proxy itself, or no such host).
	JumpAlias string `json:"jumpAlias,omitempty"`
	JumpSaved string `json:"jumpSaved,omitempty"`
}

// jumpPlan is where a ProxyJump host's jump comes from on import.
type jumpPlan struct {
	alias   string // a config host, used when it is imported too
	savedID string // a saved host, used otherwise
	saved   string // its label
}

// planJump resolves h's ProxyJump to a config alias and/or a saved host.
// The jump target must not need a proxy itself (one hop only).
func planJump(h sshconfig.Host, byAlias map[string]sshconfig.Host, saved []store.Host) jumpPlan {
	if h.Proxy != "ProxyJump" {
		return jumpPlan{}
	}
	j, ok := sshconfig.ParseJump(h.ProxyJump)
	if !ok {
		return jumpPlan{}
	}
	var out jumpPlan
	addr, user, port := j.Host, j.User, j.Port
	for alias, c := range byAlias {
		if !strings.EqualFold(alias, j.Host) || strings.EqualFold(alias, h.Alias) {
			continue
		}
		if c.Proxy != "" {
			return jumpPlan{}
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
	if port == 0 {
		port = 22
	}
	for _, sh := range saved {
		if sh.AuthType == "local" || strings.TrimSpace(sh.JumpHost) != "" {
			continue
		}
		p := sh.Port
		if p == 0 {
			p = 22
		}
		if !strings.EqualFold(strings.TrimSpace(sh.Address), addr) || p != port {
			continue
		}
		if user != "" && sh.Username != user {
			continue
		}
		if sh.Address == h.HostName && sh.Username == h.User && p == h.Port {
			continue // the host itself
		}
		out.savedID, out.saved = sh.ID, sh.Name
		break
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
		plan := planJump(h, byAlias, saved)
		out.Hosts = append(out.Hosts, importHost{Host: h, Saved: alreadySaved(saved, h), JumpAlias: plan.alias, JumpSaved: plan.saved})
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
	Keys  []struct {
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
	savedHosts, err := s.store.ListHosts()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	plans := map[string]jumpPlan{}
	for _, h := range hosts {
		plans[h.Alias] = planJump(h, byAlias, savedHosts)
	}
	newID := map[string]string{}
	outHosts := []store.Host{}
	for _, h := range hosts {
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
		saved, err := s.store.UpsertHost(in)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		newID[h.Alias] = saved.ID
		outHosts = append(outHosts, redactHost(saved))
	}
	// ProxyJump: point each host at its jump host, imported in this same
	// request or already saved. Anything else stays without it, as the
	// preview said.
	jumps := 0
	for i, h := range hosts {
		plan := plans[h.Alias]
		jid := newID[plan.alias]
		if jid == "" {
			jid = plan.savedID
		}
		if jid == "" || jid == newID[h.Alias] {
			continue
		}
		full, err := s.store.GetHost(newID[h.Alias])
		if err != nil {
			continue
		}
		full.JumpHost = jid
		saved, err := s.store.UpsertHost(full)
		if err != nil {
			continue
		}
		outHosts[i] = redactHost(saved)
		jumps++
	}
	s.note("vault", fmt.Sprintf("imported %d hosts (%d through a jump host) and %d keys from %s", len(outHosts), jumps, len(outIDs), preview.Path), "", "", "")
	writeJSON(w, http.StatusOK, map[string]any{"hosts": outHosts, "identities": outIDs})
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
