package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"keel/internal/store"
)

func redactIdentity(in store.Identity) store.Identity {
	if in.Password != "" {
		in.Password = "__saved__"
	}
	if in.PrivateKey != "" {
		in.PrivateKey = "__saved__"
	}
	if in.Passphrase != "" {
		in.Passphrase = "__saved__"
	}
	return in
}

func (s *Server) listIdentities(w http.ResponseWriter, _ *http.Request) {
	ids, err := s.store.ListIdentities()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	out := make([]store.Identity, len(ids))
	for i, id := range ids {
		out[i] = redactIdentity(id)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) upsertIdentity(w http.ResponseWriter, r *http.Request) {
	var in store.Identity
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid identity payload")
		return
	}
	in.ID = ""
	if err := validateIdentity(in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	saved, err := s.store.UpsertIdentity(in)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	s.note("identity", "identity created: "+saved.Name, "", "", "")
	writeJSON(w, http.StatusCreated, redactIdentity(saved))
}

func (s *Server) updateIdentity(w http.ResponseWriter, r *http.Request) {
	var in store.Identity
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid identity payload")
		return
	}
	in.ID = r.PathValue("id")
	if err := validateIdentity(in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	saved, err := s.store.UpsertIdentity(in)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	s.note("identity", "identity updated: "+saved.Name, "", "", "")
	writeJSON(w, http.StatusOK, redactIdentity(saved))
}

func (s *Server) deleteIdentity(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteIdentity(id); err != nil {
		writeStoreErr(w, err)
		return
	}
	s.note("identity", "identity deleted", "", "", "")
	w.WriteHeader(http.StatusNoContent)
}

func validateIdentity(in store.Identity) error {
	if strings.TrimSpace(in.Name) == "" {
		return errors.New("name is required")
	}
	return store.ValidateIdentityKind(in.Kind)
}

func (s *Server) listTunnels(w http.ResponseWriter, _ *http.Request) {
	if s.tunnels != nil {
		writeJSON(w, http.StatusOK, s.tunnels.List())
		return
	}
	saved, err := s.store.ListTunnels()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) upsertTunnel(w http.ResponseWriter, r *http.Request) {
	var tn store.Tunnel
	if err := json.NewDecoder(r.Body).Decode(&tn); err != nil {
		writeError(w, http.StatusBadRequest, "invalid tunnel payload")
		return
	}
	tn.ID = ""
	saved, err := s.store.UpsertTunnel(tn)
	if err != nil {
		if errors.Is(err, store.ErrLocked) || errors.Is(err, store.ErrNotFound) {
			writeStoreErr(w, err)
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.note("tunnel", "tunnel created: "+saved.Name, saved.HostID, "", saved.ID)
	writeJSON(w, http.StatusCreated, saved)
}

func (s *Server) updateTunnel(w http.ResponseWriter, r *http.Request) {
	var tn store.Tunnel
	if err := json.NewDecoder(r.Body).Decode(&tn); err != nil {
		writeError(w, http.StatusBadRequest, "invalid tunnel payload")
		return
	}
	tn.ID = r.PathValue("id")
	saved, err := s.store.UpsertTunnel(tn)
	if err != nil {
		if errors.Is(err, store.ErrLocked) || errors.Is(err, store.ErrNotFound) {
			writeStoreErr(w, err)
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.note("tunnel", "tunnel updated: "+saved.Name, saved.HostID, "", saved.ID)
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) deleteTunnel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.tunnels != nil {
		_ = s.tunnels.Stop(id)
	}
	if err := s.store.DeleteTunnel(id); err != nil {
		writeStoreErr(w, err)
		return
	}
	s.note("tunnel", "tunnel deleted", "", "", id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) startTunnel(w http.ResponseWriter, r *http.Request) {
	if s.tunnels == nil {
		writeError(w, http.StatusServiceUnavailable, "tunnels unavailable")
		return
	}
	id := r.PathValue("id")
	info, err := s.tunnels.Start(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrLocked) {
			writeStoreErr(w, err)
			return
		}
		if writeHostKeyErr(w, err) {
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.note("tunnel", "tunnel started: "+info.Name, info.HostID, "", info.ID)
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) stopTunnel(w http.ResponseWriter, r *http.Request) {
	if s.tunnels == nil {
		writeError(w, http.StatusServiceUnavailable, "tunnels unavailable")
		return
	}
	id := r.PathValue("id")
	if err := s.tunnels.Stop(id); err != nil {
		writeStoreErr(w, err)
		return
	}
	s.note("tunnel", "tunnel stopped", "", "", id)
	info, err := s.tunnels.Get(id)
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) listLogs(w http.ResponseWriter, r *http.Request) {
	if s.logs == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	writeJSON(w, http.StatusOK, s.logs.Recent(limit))
}

func (s *Server) trustHostKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Hostname    string `json:"hostname"`
		Fingerprint string `json:"fingerprint"`
		KeyType     string `json:"keyType"`
		Replace     bool   `json:"replace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	if err := s.store.TrustHostKeyType(req.Hostname, req.Fingerprint, req.KeyType, req.Replace); err != nil {
		if writeHostKeyErr(w, err) {
			return
		}
		if errors.Is(err, store.ErrLocked) {
			writeStoreErr(w, err)
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.note("host", "trusted host key for "+strings.TrimSpace(req.Hostname), "", "", "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func writeHostKeyErr(w http.ResponseWriter, err error) bool {
	var unk *store.UnknownHostError
	if errors.As(err, &unk) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":       err.Error(),
			"errorKind":   "unknown-host",
			"hostname":    unk.Host,
			"fingerprint": unk.Fingerprint,
			"keyType":     unk.KeyType,
		})
		return true
	}
	var mm *store.HostKeyMismatchError
	if errors.As(err, &mm) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":       err.Error(),
			"errorKind":   "host-key-mismatch",
			"hostname":    mm.Host,
			"fingerprint": mm.Got,
			"expected":    mm.Expected,
			"keyType":     mm.KeyType,
		})
		return true
	}
	return false
}
