package store

import (
	"errors"
	"fmt"
	"strings"
)

// Agent settings choices. The settings live in keel.json and hold no
// secret: provider keys are vault secrets (agentkeys.go).
const (
	DetectionAuto     = "auto"      // a classifier notices plain language
	DetectionHashOnly = "hash-only" // only lines starting with #

	SuggestionsOn  = "on"  // plain-language suggestions in panes (§5.5)
	SuggestionsOff = "off" // no notice, no hint, no detection call

	ScopeFocused = "focused" // the focused pane only
	ScopePick    = "pick"    // pick panes each conversation
	ScopeTab     = "tab"     // every pane in the tab

	MinScrollbackLines     = 50
	MaxScrollbackLines     = 1000
	DefaultScrollbackLines = 200

	// HistoryDays 0 keeps a conversation only while it is open.
	MaxHistoryDays     = 365
	DefaultHistoryDays = 30

	DefaultAgentProvider = "gemini"
	DefaultAgentModel    = "gemini-3.8-flash"
	// retiredDefaultModel was the default before Google closed the 2.5
	// family to new keys; settings saved with it move to the new default.
	retiredDefaultModel = "gemini-2.5-flash"
	maxModelLen         = 128
)

// AgentRedaction is what is redacted before anything leaves the machine.
// Secrets is always true: Normalize sets it whatever was sent.
type AgentRedaction struct {
	Secrets   bool `json:"secrets"`
	IPs       bool `json:"ips"`
	Hostnames bool `json:"hostnames"`
}

type AgentSettings struct {
	// Provider is a provider id (internal/agent/provider), e.g. "gemini".
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Detection string `json:"detection"`
	// Suggestions turns plain-language suggestions in panes (§5.5) "on"
	// (the default) or "off". A string, not a bool, so an older keel.json
	// without the field reads as on.
	Suggestions string `json:"suggestions"`
	// HistorySuggestions lets in-pane suggestions offer commands seen
	// after the pane's prompt in its scrollback (phase 6c+): up to about
	// 40 recent ones are sent to Jev, redacted, on each ⇥. "on" (the
	// default) or "off"; off still offers agent-usable snippets.
	HistorySuggestions string         `json:"historySuggestions"`
	DefaultScope       string         `json:"defaultScope"`
	ScrollbackLines    int            `json:"scrollbackLines"`
	Redact             AgentRedaction `json:"redact"`
	HistoryDays        int            `json:"historyDays"`
}

// DefaultAgentSettings are the settings before the user changes any.
func DefaultAgentSettings() AgentSettings {
	return AgentSettings{
		Provider:           DefaultAgentProvider,
		Model:              DefaultAgentModel,
		Detection:          DetectionAuto,
		Suggestions:        SuggestionsOn,
		HistorySuggestions: SuggestionsOn,
		DefaultScope:       ScopeFocused,
		ScrollbackLines:    DefaultScrollbackLines,
		Redact:             AgentRedaction{Secrets: true},
		HistoryDays:        DefaultHistoryDays,
	}
}

var ErrAgentSettings = errors.New("invalid agent settings")

// Normalize fills empty fields with defaults, clamps the numbers, forces
// secret redaction on, and rejects unknown choices. It does not check that
// the provider id exists; the API does that against the provider list.
func (a AgentSettings) Normalize() (AgentSettings, error) {
	def := DefaultAgentSettings()
	a.Provider = strings.ToLower(strings.TrimSpace(a.Provider))
	if a.Provider == "" {
		a.Provider = def.Provider
	}
	if !validProviderID(a.Provider) {
		return AgentSettings{}, fmt.Errorf("%w: provider %q", ErrAgentSettings, a.Provider)
	}
	a.Model = strings.TrimSpace(a.Model)
	if a.Model == "" {
		a.Model = def.Model
	}
	if len(a.Model) > maxModelLen || strings.ContainsAny(a.Model, " \t\r\n/?#%") {
		return AgentSettings{}, fmt.Errorf("%w: model %q", ErrAgentSettings, a.Model)
	}
	switch a.Detection {
	case "":
		a.Detection = def.Detection
	case DetectionAuto, DetectionHashOnly:
	default:
		return AgentSettings{}, fmt.Errorf("%w: detection must be auto or hash-only", ErrAgentSettings)
	}
	switch a.Suggestions {
	case "":
		a.Suggestions = def.Suggestions
	case SuggestionsOn, SuggestionsOff:
	default:
		return AgentSettings{}, fmt.Errorf("%w: suggestions must be on or off", ErrAgentSettings)
	}
	switch a.HistorySuggestions {
	case "":
		a.HistorySuggestions = def.HistorySuggestions
	case SuggestionsOn, SuggestionsOff:
	default:
		return AgentSettings{}, fmt.Errorf("%w: historySuggestions must be on or off", ErrAgentSettings)
	}
	switch a.DefaultScope {
	case "":
		a.DefaultScope = def.DefaultScope
	case ScopeFocused, ScopePick, ScopeTab:
	default:
		return AgentSettings{}, fmt.Errorf("%w: defaultScope must be focused, pick or tab", ErrAgentSettings)
	}
	switch {
	case a.ScrollbackLines == 0:
		a.ScrollbackLines = def.ScrollbackLines
	case a.ScrollbackLines < MinScrollbackLines:
		a.ScrollbackLines = MinScrollbackLines
	case a.ScrollbackLines > MaxScrollbackLines:
		a.ScrollbackLines = MaxScrollbackLines
	}
	if a.HistoryDays < 0 {
		a.HistoryDays = 0
	}
	if a.HistoryDays > MaxHistoryDays {
		a.HistoryDays = MaxHistoryDays
	}
	a.Redact.Secrets = true
	return a, nil
}

// SuggestionsEnabled reports whether suggestions in panes are on (the
// default).
func (a AgentSettings) SuggestionsEnabled() bool {
	return a.Suggestions != SuggestionsOff
}

// HistorySuggestionsEnabled reports whether in-pane suggestions may offer
// commands from the pane's scrollback (the default).
func (a AgentSettings) HistorySuggestionsEnabled() bool {
	return a.HistorySuggestions != SuggestionsOff
}

// AgentSettings returns the saved settings, or the defaults.
func (s *Store) AgentSettings() (AgentSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return AgentSettings{}, ErrLocked
	}
	if s.data.Agent == nil {
		return DefaultAgentSettings(), nil
	}
	// A hand-edited keel.json may hold anything; never return it raw.
	out, err := s.data.Agent.Normalize()
	if err != nil {
		return DefaultAgentSettings(), nil
	}
	if out.Model == retiredDefaultModel {
		out.Model = DefaultAgentModel
	}
	return out, nil
}

// SetAgentSettings normalizes and saves the settings, returning what was
// saved.
func (s *Store) SetAgentSettings(a AgentSettings) (AgentSettings, error) {
	a, err := a.Normalize()
	if err != nil {
		return AgentSettings{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return AgentSettings{}, ErrLocked
	}
	s.data.Agent = &a
	return a, s.saveLocked()
}
