package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"keel/internal/session"
	"keel/internal/store"
)

// Edit host: Test connection and the host key line (docs/ui-redesign.md
// §5.8).

// testHost runs Test connection on the dialog's current, unsaved values:
// a real connect and authentication (through the jump host, if one is
// picked), then close. It opens no session or PTY and runs no command.
//
// Credentials typed into the dialog arrive in this request body only,
// over the authed loopback API. They are used for this one connect, kept
// in memory for it, and never stored or logged; blank fields fall back to
// the saved host's vault secrets (store.ResolveDraft). An unknown or
// changed host key is reported with its fingerprint and never trusted
// here: trusting is a separate, explicit POST /api/known-hosts.
//
// The connect stops after session.ProbeTimeout, or as soon as the client
// goes away (the dialog's Cancel aborts the fetch).
func (s *Server) testHost(w http.ResponseWriter, r *http.Request) {
	var h store.Host
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&h); err != nil {
		writeError(w, http.StatusBadRequest, "invalid host payload")
		return
	}
	if h.AuthType == "local" {
		writeError(w, http.StatusBadRequest, "the local shell needs no connection test")
		return
	}
	label := strings.TrimSpace(h.Name)
	if label == "" {
		label = strings.TrimSpace(h.Address)
	}
	check := h
	check.Name = label
	if err := validateHost(check); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	host, err := s.store.ResolveDraft(h)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if host.Name == "" {
		host.Name = label
	}
	res := session.Probe(r.Context(), host, s.store.CheckHostKey)
	s.note("host", "connection test "+label+": "+res.Kind, h.ID, "", "")
	writeJSON(w, http.StatusOK, res)
}

// getHostKey returns the saved host key for a saved host's address:port.
func (s *Server) getHostKey(w http.ResponseWriter, r *http.Request) {
	h, err := s.store.GetHost(r.PathValue("id"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if h.AuthType == "local" {
		writeError(w, http.StatusBadRequest, "the local shell has no host key")
		return
	}
	k, err := s.store.KnownHost(store.HostKeyName(h))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, k)
}

// forgetHostKey removes the saved host key, so the next connect asks
// again (trust on first use). The forget is logged.
func (s *Server) forgetHostKey(w http.ResponseWriter, r *http.Request) {
	h, err := s.store.GetHost(r.PathValue("id"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if h.AuthType == "local" {
		writeError(w, http.StatusBadRequest, "the local shell has no host key")
		return
	}
	name := store.HostKeyName(h)
	prev, err := s.store.KnownHost(name)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	ok, err := s.store.ForgetHostKey(name)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "no saved host key for "+name)
		return
	}
	s.note("host", "forgot host key for "+h.Name+" ("+name+", "+prev.Fingerprint+")", h.ID, "", "")
	writeJSON(w, http.StatusOK, store.KnownHostKey{Hostname: name})
}
