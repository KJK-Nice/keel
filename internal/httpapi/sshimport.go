package httpapi

import (
	"fmt"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"keel/internal/hostimport"
	"keel/internal/sshconfig"
	"keel/internal/store"
	"keel/internal/wire"
)

// First-run host import (docs/ui-redesign.md §5.15) over HTTP. The import
// itself, reading ~/.ssh/config (KEEL_SSH_DIR in keel serve) and adding
// the hosts the user picks, is internal/hostimport.

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

func (s *Server) sshConfigPreview(w http.ResponseWriter, _ *http.Request) {
	opts, _ := s.sshOptions()
	out, err := hostimport.BuildPreview(s.store, opts)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) sshConfigImport(w http.ResponseWriter, r *http.Request) {
	var req hostimport.Request
	if err := decode(w, r, bodyBulk, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid import payload")
		return
	}
	opts, _ := s.sshOptions()
	res, err := hostimport.Import(s.store, opts, req)
	if err != nil {
		writeErr(w, err)
		return
	}
	hosts := make([]store.Host, len(res.Hosts))
	for i, h := range res.Hosts {
		hosts[i] = h.Redacted()
	}
	ids := make([]store.Identity, len(res.Identities))
	for i, id := range res.Identities {
		ids[i] = id.Redacted()
	}
	for _, f := range res.Failed {
		s.note(wire.LogVault, "host import: "+f.Alias+" not added: "+f.Error, "", "", "")
	}
	s.note(wire.LogVault, fmt.Sprintf("imported %d hosts (%d through a jump host) and %d keys from %s", len(hosts), res.Jumps, len(ids), res.Path), "", "", "")
	writeJSON(w, http.StatusOK, map[string]any{"hosts": hosts, "identities": ids, "failed": res.Failed})
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
		// The vault state now (vaultrev.go): the native client polls it to refresh hosts and tunnels.
		"rev": s.vault.rev(),
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
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
