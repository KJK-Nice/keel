package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"

	"keel/internal/agent/provider"
	"keel/internal/store"
)

// Agent settings and provider keys (phase 6a). These routes store choices
// and keys; none of them calls a provider. Keys go into the vault and are
// never returned: responses only say whether one is configured.

const maxAgentBody = 16 * 1024

type agentKeyState struct {
	Configured bool `json:"configured"`
}

type agentSettingsResp struct {
	Settings store.AgentSettings      `json:"settings"`
	Keys     map[string]agentKeyState `json:"keys"`
}

type agentProviderResp struct {
	provider.Descriptor
	Configured bool `json:"configured"`
}

func (s *Server) agentKeyStates() (map[string]agentKeyState, error) {
	out := map[string]agentKeyState{}
	for _, d := range provider.Descriptors() {
		ok, err := s.store.AgentKeyConfigured(d.ID)
		if err != nil {
			return nil, err
		}
		out[d.ID] = agentKeyState{Configured: ok}
	}
	return out, nil
}

func (s *Server) writeAgentSettings(w http.ResponseWriter, status int, set store.AgentSettings) {
	keys, err := s.agentKeyStates()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, status, agentSettingsResp{Settings: set, Keys: keys})
}

func (s *Server) getAgentSettings(w http.ResponseWriter, _ *http.Request) {
	set, err := s.store.AgentSettings()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	s.writeAgentSettings(w, http.StatusOK, set)
}

// putAgentSettings merges the sent fields over the saved settings, so a
// client may send only what it changes. Secret redaction stays on.
func (s *Server) putAgentSettings(w http.ResponseWriter, r *http.Request) {
	cur, err := s.store.AgentSettings()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxAgentBody))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid settings payload")
		return
	}
	if err := json.Unmarshal(body, &cur); err != nil {
		writeError(w, http.StatusBadRequest, "invalid settings payload")
		return
	}
	d, ok := provider.Lookup(strings.ToLower(strings.TrimSpace(cur.Provider)))
	if cur.Provider != "" && (!ok || d.Kind != provider.KindModel) {
		writeError(w, http.StatusBadRequest, "unknown agent provider")
		return
	}
	// A provider switch that kept the old provider's model (a client that
	// sent only the provider) takes the new provider's default model: a
	// Gemini model is never sent to Anthropic, nor the other way round. A
	// model no provider lists (a custom one) is kept.
	if ok && cur.Model != "" && !slices.Contains(d.Models, cur.Model) && provider.ListedElsewhere(d.ID, cur.Model) {
		cur.Model = d.DefaultModel
	}
	saved, err := s.store.SetAgentSettings(cur)
	if err != nil {
		if errors.Is(err, store.ErrAgentSettings) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeStoreErr(w, err)
		return
	}
	s.note("agent", "agent settings saved", "", "", "")
	// A shorter Keep conversations (or Only while open) applies now, not
	// at the next hourly prune.
	if s.agent != nil {
		s.pruneAgentChats()
	}
	s.writeAgentSettings(w, http.StatusOK, saved)
}

func (s *Server) listAgentProviders(w http.ResponseWriter, _ *http.Request) {
	out := []agentProviderResp{}
	for _, d := range provider.Descriptors() {
		ok, err := s.store.AgentKeyConfigured(d.ID)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		out = append(out, agentProviderResp{Descriptor: d, Configured: ok})
	}
	writeJSON(w, http.StatusOK, out)
}

// putAgentKey stores a provider key in the vault. The key is not checked
// against the provider: that would send it off the machine.
func (s *Server) putAgentKey(w http.ResponseWriter, r *http.Request) {
	d, ok := provider.Lookup(r.PathValue("provider"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown agent provider")
		return
	}
	var req struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxAgentBody)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid key payload")
		return
	}
	if err := s.store.SetAgentKey(d.ID, req.Key); err != nil {
		if errors.Is(err, store.ErrAgentKey) {
			writeError(w, http.StatusBadRequest, "the key is empty, too long, or has spaces")
			return
		}
		writeStoreErr(w, err)
		return
	}
	s.note("agent", "agent key saved for "+d.Name, "", "", "")
	writeJSON(w, http.StatusOK, map[string]any{"provider": d.ID, "configured": true})
}

func (s *Server) deleteAgentKey(w http.ResponseWriter, r *http.Request) {
	d, ok := provider.Lookup(r.PathValue("provider"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown agent provider")
		return
	}
	if err := s.store.ClearAgentKey(d.ID); err != nil {
		writeStoreErr(w, err)
		return
	}
	s.note("agent", "agent key removed for "+d.Name, "", "", "")
	writeJSON(w, http.StatusOK, map[string]any{"provider": d.ID, "configured": false})
}
