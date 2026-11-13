package httpapi

import (
	"net/http"
	"strconv"

	"keel/internal/store"
	"keel/internal/wire"
)

func (s *Server) listIdentities(w http.ResponseWriter, _ *http.Request) {
	ids, err := s.store.ListIdentities()
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]store.Identity, len(ids))
	for i, id := range ids {
		out[i] = id.Redacted()
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) upsertIdentity(w http.ResponseWriter, r *http.Request) {
	var in store.Identity
	if err := decode(w, r, bodyHost, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid identity payload")
		return
	}
	in.ID = r.PathValue("id")
	saved, err := s.saveIdentity(in)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, savedStatus(r.PathValue("id")), saved.Redacted())
}

func (s *Server) deleteIdentity(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteIdentity(id); err != nil {
		writeErr(w, err)
		return
	}
	s.note("identity", "identity deleted", "", "", "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listTunnels(w http.ResponseWriter, _ *http.Request) {
	if s.tunnels != nil {
		live, err := s.tunnels.List()
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, live)
		return
	}
	saved, err := s.store.ListTunnels()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) upsertTunnel(w http.ResponseWriter, r *http.Request) {
	var tn store.Tunnel
	if err := decode(w, r, bodyForm, &tn); err != nil {
		writeError(w, http.StatusBadRequest, "invalid tunnel payload")
		return
	}
	tn.ID = r.PathValue("id")
	saved, err := s.saveTunnel(tn)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, savedStatus(r.PathValue("id")), saved)
}

func (s *Server) deleteTunnel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.tunnels != nil {
		_ = s.tunnels.Stop(id)
	}
	if err := s.store.DeleteTunnel(id); err != nil {
		writeErr(w, err)
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
		writeErr(w, otherwise(err, http.StatusBadRequest))
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
		writeErr(w, err)
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
	// Only a key a connect was just refused for can be trusted: the
	// request names that refusal's offer (token) and must repeat its exact
	// address:port and fingerprint. The key type comes from the offer.
	var req wire.TrustHostKey
	if err := decode(w, r, bodyLine, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	if !store.ValidHostKeyName(req.Hostname) {
		writeError(w, http.StatusBadRequest, "hostname must be address:port")
		return
	}
	if !store.ValidFingerprint(req.Fingerprint) {
		writeError(w, http.StatusBadRequest, "fingerprint must be an OpenSSH SHA256 fingerprint")
		return
	}
	key, err := s.store.TrustOffered(req.Token, req.Hostname, req.Fingerprint, req.Replace)
	if err != nil {
		writeErr(w, otherwise(err, http.StatusConflict))
		return
	}
	verb := "trusted"
	if req.Replace {
		verb = "replaced"
	}
	s.note("host", verb+" host key for "+key.Hostname+" ("+key.Fingerprint+")", "", "", "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "hostname": key.Hostname, "fingerprint": key.Fingerprint, "keyType": key.KeyType})
}
