package native

import (
	"context"

	"keel/internal/native/keelclient"
)

// The agent half of fakeBackend: what the panel asks of keel serve, recorded, and answered as the test says. Nothing
// here reads a pane: an ask records only the question and the session ids.

type fakeAgent struct {
	settings  keelclient.AgentSettings
	settingsE error
	providers []keelclient.AgentProvider
	keys      []keelclient.AgentKeyInput
	keyErr    error
	asks      []keelclient.AskRequest
	askReply  func(req keelclient.AskRequest) (keelclient.AskResponse, error) // when nil: an empty answer
	askWait   chan struct{}                                                   // when set: the ask closes it and waits for its context
	edits     []string                                                        // "conv/proposal: command"
	editReply func(command string) (keelclient.Proposal, error)
}

func (f *fakeBackend) AgentSettings(context.Context) (keelclient.AgentSettings, error) {
	f.rec("agent settings")
	s := f.agent.settings
	if f.agent.settingsE != nil {
		return s, f.agent.settingsE
	}
	if s.Settings.Provider == "" {
		s.Settings.Provider, s.Settings.DefaultScope, s.Settings.ScrollbackLines = "gemini", "focused", 200
	}
	if s.Keys == nil {
		s.Keys = map[string]struct {
			Configured bool `json:"configured"`
		}{}
	}
	return s, nil
}

func (f *fakeBackend) AgentProviders(context.Context) ([]keelclient.AgentProvider, error) {
	f.rec("agent providers")
	if f.agent.providers != nil {
		return f.agent.providers, nil
	}
	return []keelclient.AgentProvider{{ID: "gemini", Name: "Google Gemini", ShortName: "Gemini", Kind: "model", KeyLabel: "Gemini API key",
		Warning: "On the free tier of the Gemini API, Google may use the content you submit.", TermsURL: "https://ai.google.dev/terms", KeyHelpURL: "https://aistudio.google.com/apikey"}}, nil
}

func (f *fakeBackend) SetAgentKey(_ context.Context, in keelclient.AgentKeyInput) error {
	f.rec("agent key")
	f.agent.keys = append(f.agent.keys, in)
	if f.agent.keyErr != nil {
		return f.agent.keyErr
	}
	s := f.agent.settings
	if s.Settings.Provider == "" {
		s.Settings.Provider, s.Settings.DefaultScope, s.Settings.ScrollbackLines = "gemini", "focused", 200
	}
	if s.Keys == nil {
		s.Keys = map[string]struct {
			Configured bool `json:"configured"`
		}{}
	}
	s.Keys[in.Provider] = struct {
		Configured bool `json:"configured"`
	}{true}
	f.agent.settings = s
	return nil
}

func (f *fakeBackend) Ask(ctx context.Context, req keelclient.AskRequest) (keelclient.AskResponse, error) {
	f.rec("ask")
	f.agent.asks = append(f.agent.asks, req)
	if f.agent.askWait != nil { // an ask that waits for its context, as keel serve's waits for a model
		close(f.agent.askWait)
		<-ctx.Done()
		return keelclient.AskResponse{}, ctx.Err()
	}
	if f.agent.askReply != nil {
		return f.agent.askReply(req)
	}
	return keelclient.AskResponse{ConversationID: req.ConversationID, Answer: "ok"}, nil
}

func (f *fakeBackend) EditProposal(_ context.Context, conv, id, command string) (keelclient.Proposal, error) {
	f.rec("edit proposal")
	f.agent.edits = append(f.agent.edits, conv+"/"+id+": "+command)
	if f.agent.editReply != nil {
		return f.agent.editReply(command)
	}
	return keelclient.Proposal{ID: id, Command: command, Bytes: "\x15" + command + "\n", Edited: true}, nil
}
