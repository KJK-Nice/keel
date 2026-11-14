package native

import (
	"context"
	"errors"
	"math"
	"strings"

	"github.com/egoist/mygo/ui"

	"keel/internal/limits"
	"keel/internal/native/keelclient"
)

// Settings · Agent (docs/ui-redesign.md §5.9): the agent's settings are keel serve's, shown and saved here.
//
// What this file does and does not do:
//   - Every change is saved as it is made with PUT /api/agent/settings and only the fields that changed (keel serve merges
//     them over the saved settings); the answer is the settings as saved and the screen shows that. A failed save says so and
//     reads the settings again.
//   - Redaction, scope and retention are keel serve's. This sends the value; it implements none of their effect.
//   - A provider key goes from a masked field to PUT /api/agent/keys/{provider} once; the field is emptied the moment it is
//     sent and nothing keeps it. The key is in no error, no toast, no log. Disconnect and Delete all history ask first.
//   - "Allow Claude Code (keel mcp)" is the setting claudeCode and nothing else: this client never calls keel mcp's routes.
//
// Settings whose feature is not built in this window (Claude Code's mcp) are still settings of keel serve, which acts on
// them, so they are shown and saved, not hidden.

// keyCard is a provider card's own state: whether its key field is open, its error and the masked field.
type keyCard struct {
	changing bool
	err      string
	busy     bool
	field    string // the masked field: emptied the moment the key is sent
	h        ui.Handle
	focus    bool
}

func (p *settingsPage) card(id string) *keyCard {
	if p.cards == nil {
		p.cards = map[string]*keyCard{}
	}
	if p.cards[id] == nil {
		p.cards[id] = &keyCard{}
	}
	return p.cards[id]
}

// loadAgentSettings reads the settings and the providers (GET /api/agent/settings, /api/agent/providers).
func (w *workspace) loadAgentSettings() {
	a := w.agent
	if p := w.settingsPage(); p != nil {
		w.loadNotes(&p.notes) // Notes for the agent: the user's own
	}
	type read struct {
		s    keelclient.AgentSettings
		list []keelclient.AgentProvider
		lerr error
	}
	call(w, func(c context.Context) (r read, err error) {
		if r.s, err = w.be.AgentSettings(c); err == nil {
			r.list, r.lerr = w.be.AgentProviders(c)
		}
		return r, err
	}, func(r read, err error) {
		var se *keelclient.StatusError
		switch {
		case errors.As(err, &se) && se.Code == 423: // the vault is locked: the Unlock screen says so
			return
		case err != nil:
			w.warn(errText("Could not load the agent settings", err))
			return
		}
		w.takeAgentSettings(r.s)
		if r.lerr == nil {
			a.provs = a.provs[:0]
			for _, d := range r.list {
				if d.Kind == "model" || d.ID == "jev" {
					a.provs = append(a.provs, d)
				}
			}
		}
	})
}

// takeAgentSettings makes the settings keel serve answered with what every view shows: the panel's key state, its saved note.
func (w *workspace) takeAgentSettings(s keelclient.AgentSettings) {
	a := w.agent
	a.settings = &s
	k := s.KeyConfigured()
	a.keyConfigured = &k
	d := s.Settings.HistoryDays
	a.list.historyDays = &d
	w.noteSuggestSettings(s)
}

// applyPatch is what a patch changes, shown at once while keel serve answers.
func applyPatch(s *keelclient.AgentSettings, p keelclient.AgentSettingsPatch) {
	v := &s.Settings
	if p.Provider != nil {
		v.Provider = *p.Provider
	}
	if p.Model != nil {
		v.Model = *p.Model
	}
	if p.Detection != nil {
		v.Detection = *p.Detection
	}
	if p.Suggestions != nil {
		v.Suggestions = *p.Suggestions
	}
	if p.HistorySuggestions != nil {
		v.HistorySuggestions = *p.HistorySuggestions
	}
	if p.DefaultScope != nil {
		v.DefaultScope = *p.DefaultScope
	}
	if p.ScrollbackLines != nil {
		v.ScrollbackLines = clampLines(*p.ScrollbackLines)
	}
	if p.Redact != nil {
		v.Redact.Secrets, v.Redact.IPs, v.Redact.Hostnames = true, p.Redact.IPs, p.Redact.Hostnames
	}
	if p.HistoryDays != nil {
		v.HistoryDays = *p.HistoryDays
	}
	if p.ClaudeCode != nil {
		v.ClaudeCode = *p.ClaudeCode
	}
}

// putAgent saves a patch over the saved settings and takes the saved value back, so the screen always shows what keel
// serve holds. done, when there is one, runs when the answer is in.
func (w *workspace) putAgent(p keelclient.AgentSettingsPatch, done func()) {
	a := w.agent
	if a.settings == nil {
		if done != nil {
			done()
		}
		return
	}
	cp := *a.settings
	applyPatch(&cp, p)
	a.settings = &cp // shown at once; keel serve's answer replaces it
	w.noteSuggestSettings(cp)
	call(w, func(c context.Context) (keelclient.AgentSettings, error) { return w.be.SaveAgentSettings(c, p) }, func(saved keelclient.AgentSettings, err error) {
		if done != nil {
			defer done()
		}
		if err != nil {
			w.warn(errText("Could not save", err))
			w.loadAgentSettings()
			return
		}
		w.takeAgentSettings(saved)
		if p.HistoryDays != nil { // a shorter Keep conversations (or Only while open) applies now: the list follows
			w.loadConversations()
		}
	})
}

func (w *workspace) agentVal() keelclient.AgentSettings {
	if w.agent.settings != nil {
		return *w.agent.settings
	}
	var s keelclient.AgentSettings // before the first read: the defaults (store.DefaultAgentSettings)
	s.Settings.Provider, s.Settings.Detection, s.Settings.Suggestions, s.Settings.HistorySuggestions = limits.DefaultAgentProvider, limits.DetectionHashOnly, limits.SuggestionsOn, limits.SuggestionsOn
	s.Settings.DefaultScope, s.Settings.ScrollbackLines, s.Settings.HistoryDays = limits.ScopeFocused, limits.DefaultScrollbackLines, limits.DefaultHistoryDays
	s.Settings.Redact.Secrets = true
	return s
}

func (w *workspace) providerFor(id string) (keelclient.AgentProvider, bool) {
	for _, d := range w.agent.provs {
		if d.ID == id {
			return d, true
		}
	}
	return keelclient.AgentProvider{}, false
}

// ---- the key ----

// saveCardKey sends the key in a card's masked field, once. The field is emptied as it is sent: nothing keeps the key, and
// when keel serve refuses it the user types it again.
func (w *workspace) saveCardKey(p *settingsPage, id string) {
	card := p.card(id)
	if card.busy {
		return
	}
	key := strings.TrimSpace(card.field)
	card.field = ""
	if key == "" {
		card.err, card.focus = "Paste a key first.", true
		return
	}
	in := keelclient.AgentKeyInput{Provider: id, Key: key}
	key = ""
	card.busy, card.err = true, ""
	name := id
	if d, ok := w.providerFor(id); ok {
		name = d.Name
	}
	callErr(w, func(c context.Context) error {
		err := w.be.SetAgentKey(c, in)
		in = keelclient.AgentKeyInput{}
		return err
	}, func(err error) {
		card.busy = false
		if err != nil {
			card.err, card.focus = keyError(err), true
			return
		}
		*card = keyCard{}
		w.say(name + " connected. The key is in the vault.")
		w.loadAgentSettings()
	})
}

// askDisconnect is a card's Disconnect: it asks first.
func (w *workspace) askDisconnect(id string) {
	name := id
	if d, ok := w.providerFor(id); ok {
		name = d.Name
	}
	w.askConfirm(&confirmDialog{title: "Disconnect " + name + "?", message: disconnectMessage(id, name), action: "Disconnect", run: func() { w.disconnect(id, name) }})
}

func (w *workspace) disconnect(id, name string) {
	callErr(w, func(c context.Context) error { return w.be.DeleteAgentKey(c, id) }, func(err error) {
		if err != nil {
			w.warn(errText("Could not disconnect", err))
			return
		}
		if p := w.settingsPage(); p != nil {
			*p.card(id) = keyCard{}
		}
		w.say(name + " disconnected")
		w.loadAgentSettings()
	})
}

// askDeleteHistory is Settings · Agent's Delete all history: it asks first.
func (w *workspace) askDeleteHistory() {
	w.askConfirm(&confirmDialog{
		title:   "Delete all history?",
		message: "Every agent conversation is forgotten, including the ones saved in the vault, in every window. The log entries that say what the agent read stay in Logs.",
		action:  "Delete all history", run: w.deleteHistory,
	})
}

func (w *workspace) deleteHistory() {
	callErr(w, w.be.DeleteAllConversations, func(err error) {
		if err != nil {
			w.warn(errText("Could not delete the history", err))
			return
		}
		w.say("Agent history deleted")
		w.loadConversations()
	})
}

// ---- the lines slider ----

// syncAgentLines sends a scrollback-lines value the user moved the slider to, one request at a time, and takes a value
// changed elsewhere (the panel, another window).
func (w *workspace) syncAgentLines(p *settingsPage) {
	a := w.agent
	if a.settings == nil {
		return
	}
	cur := clampLines(a.settings.Settings.ScrollbackLines)
	n := clampLines(int(math.Round(p.linesV)))
	switch {
	case !p.linesInit:
		p.linesV, p.linesApplied, p.linesInit = float64(cur), cur, true
	case n != p.linesApplied && !p.linesBusy:
		p.linesApplied, p.linesBusy = n, true
		w.act(func() { w.putAgent(keelclient.AgentSettingsPatch{ScrollbackLines: &n}, func() { p.linesBusy = false }) })
	case !p.linesBusy && cur != p.linesApplied:
		p.linesApplied, p.linesV = cur, float64(cur)
	}
}
