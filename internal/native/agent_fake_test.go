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

// ---- saved conversations and Settings · Agent ----

// fakeConvs is the vault's saved conversations as the fake keeps them, most recent first, and what was asked of them.
type fakeConvs struct {
	list       keelclient.ConversationList
	transcr    map[string]keelclient.Transcript
	renamed    []string // "id: title"
	deleted    []string
	deletedAll int
	again      []string // "conv/proposal"
	againReply func(conv, id string) (keelclient.Proposal, error)
	listErr    error
	saves      []keelclient.AgentSettingsPatch
	keysGone   []string
}

func (f *fakeBackend) Conversations(context.Context) (keelclient.ConversationList, error) {
	f.rec("conversations")
	l := f.convs.list
	if f.convs.listErr != nil {
		return keelclient.ConversationList{}, f.convs.listErr
	}
	l.Sessions = append([]keelclient.ConversationSummary{}, l.Sessions...)
	return l, nil
}

func (f *fakeBackend) Conversation(_ context.Context, id string) (keelclient.Transcript, error) {
	f.rec("conversation " + id)
	if t, ok := f.convs.transcr[id]; ok {
		return t, nil
	}
	return keelclient.Transcript{}, &keelclient.StatusError{Code: 404, Status: "404 Not Found"}
}

func (f *fakeBackend) RenameConversation(_ context.Context, id, title string) (string, error) {
	f.rec("rename conversation")
	f.convs.renamed = append(f.convs.renamed, id+": "+title)
	for i := range f.convs.list.Sessions {
		if f.convs.list.Sessions[i].ID == id {
			f.convs.list.Sessions[i].Title = title
		}
	}
	return title, nil
}

func (f *fakeBackend) DeleteConversation(_ context.Context, id string) error {
	f.rec("delete conversation")
	f.convs.deleted = append(f.convs.deleted, id)
	var keep []keelclient.ConversationSummary
	for _, s := range f.convs.list.Sessions {
		if s.ID != id {
			keep = append(keep, s)
		}
	}
	f.convs.list.Sessions = keep
	return nil
}

func (f *fakeBackend) DeleteAllConversations(context.Context) error {
	f.rec("delete all conversations")
	f.convs.deletedAll++
	f.convs.list.Sessions = nil
	return nil
}

func (f *fakeBackend) ProposeAgain(_ context.Context, conv, id string) (keelclient.Proposal, error) {
	f.rec("propose again")
	f.convs.again = append(f.convs.again, conv+"/"+id)
	if f.convs.againReply != nil {
		return f.convs.againReply(conv, id)
	}
	return keelclient.Proposal{ID: "again-" + id, SessionID: "s1", HostID: "h-api", Host: "api-1", Command: "echo keel-agent-ok", Bytes: "\x15echo keel-agent-ok\n", Risk: "readonly"}, nil
}

func (f *fakeBackend) SaveAgentSettings(_ context.Context, p keelclient.AgentSettingsPatch) (keelclient.AgentSettings, error) {
	f.rec("save agent settings")
	f.convs.saves = append(f.convs.saves, p)
	s, _ := f.AgentSettings(context.Background())
	if p.Provider != nil {
		s.Settings.Provider = *p.Provider
	}
	if p.Model != nil {
		s.Settings.Model = *p.Model
	}
	if p.Detection != nil {
		s.Settings.Detection = *p.Detection
	}
	if p.Suggestions != nil {
		s.Settings.Suggestions = *p.Suggestions
	}
	if p.HistorySuggestions != nil {
		s.Settings.HistorySuggestions = *p.HistorySuggestions
	}
	if p.DefaultScope != nil {
		s.Settings.DefaultScope = *p.DefaultScope
	}
	if p.ScrollbackLines != nil {
		s.Settings.ScrollbackLines = min(max(*p.ScrollbackLines, 50), 1000)
	}
	if p.Redact != nil {
		s.Settings.Redact.IPs, s.Settings.Redact.Hostnames, s.Settings.Redact.Secrets = p.Redact.IPs, p.Redact.Hostnames, true
	}
	if p.HistoryDays != nil {
		s.Settings.HistoryDays = *p.HistoryDays
	}
	if p.ClaudeCode != nil {
		s.Settings.ClaudeCode = *p.ClaudeCode
	}
	f.agent.settings = s
	return s, nil
}

func (f *fakeBackend) DeleteAgentKey(_ context.Context, provider string) error {
	f.rec("delete agent key")
	f.convs.keysGone = append(f.convs.keysGone, provider)
	s, _ := f.AgentSettings(context.Background())
	s.Keys[provider] = struct {
		Configured bool `json:"configured"`
	}{false}
	f.agent.settings = s
	return nil
}
