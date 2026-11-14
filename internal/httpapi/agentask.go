package httpapi

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"keel/internal/agent"
	"keel/internal/agent/service"
	"keel/internal/runtime"
	"keel/internal/shellmark"
	"keel/internal/store"
	"keel/internal/wire"
)

// The agent panel's routes. POST /api/agent/ask is the only
// route in Keel that sends anything off the machine, and only what the
// agent service has read through the Auditor and redacted. These handlers
// never write to a session: typing a proposal goes through the session
// socket's approve-before-type path (sessionWS, "inject" with "agent"),
// which asks service.AuthorizeType first. agentask_test.go checks this
// file makes no write, inject or spawn call.

// WithAgentProvider sets how the agent builds its provider, and whether it
// is a test fake (the trace then says so). keel serve passes the real
// clients, or the fake in a build with the tag keelfake
// (internal/app/agent_fake.go); tests pass a fake.
func (s *Server) WithAgentProvider(f service.Factory, test bool) *Server {
	s.agentFactory = f
	s.agentTest = test
	return s
}

// agentPanes shows the agent service a runtime's sessions as panes. It
// passes on only the session id, its host id and its scrollback.
type agentPanes struct{ rt *runtime.Runtime }

func (a agentPanes) Pane(id string) (agent.Pane, bool) {
	sess, err := a.rt.Get(id)
	if err != nil {
		return agent.Pane{}, false
	}
	info := sess.Info()
	return agent.Pane{SessionID: info.ID, HostID: info.HostID}, true
}

func (a agentPanes) ReadScrollback(id string) ([]byte, error) { return a.rt.ReadScrollback(id) }

// PaneIDs lists the running sessions, oldest first (service.PaneLister),
// for keel mcp's pane list. Ids only: the service resolves each through
// Pane and keeps the ones the agent may read.
func (a agentPanes) PaneIDs() []string {
	list := a.rt.List()
	out := make([]string, 0, len(list))
	for _, info := range list {
		out = append(out, info.ID)
	}
	return out
}

// LastCommand passes on the pane's last command from its shell's OSC 133
// marks (agent.CommandSource), with the same mask as ReadScrollback.
func (a agentPanes) LastCommand(id string) (shellmark.Block, bool) { return a.rt.LastCommand(id) }

// CommandsAfter passes on the commands a pane's shell reported after a
// stream offset (service.StepSource), for a runbook step's result.
func (a agentPanes) CommandsAfter(id string, from uint64) []shellmark.Command {
	return a.rt.CommandsAfter(id, from)
}

// CommandOutput passes on a finished command's raw output
// (service.CaptureSource), read only by keel serve's capture code for a
// group run's step: never sent to a provider or logged.
func (a agentPanes) CommandOutput(id string, c shellmark.Command) ([]byte, bool) {
	return a.rt.CommandOutput(id, c)
}

// initAgent builds the agent service. Without it (no keel.log to record
// what it reads, or a service that could not be made) the reason is logged
// once here and every route that needs the agent answers with it.
func (s *Server) initAgent() {
	if s.logs == nil || s.rt == nil || s.store == nil {
		s.agentOff = "the agent needs keel.log to record what it reads"
		return
	}
	svc, err := service.New(s.store, agentPanes{rt: s.rt}, s.logs, service.Options{Factory: s.agentFactory, Judge: s.agentJudge, TestProvider: s.agentTest, SuggestTimeout: s.agentSuggestTimeout, Chats: s.store, Notes: s.store, Runbooks: s.store, Notebooks: s.store})
	if err != nil {
		s.agentOff = err.Error()
		log.Printf("agent unavailable: %v", err)
		return
	}
	s.agent = svc
	go s.agentRetention()
}

// withAgent answers 503 with the reason when there is no agent service,
// so no handler behind it checks for one.
func (s *Server) withAgent(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.agent == nil {
			writeError(w, http.StatusServiceUnavailable, "agent unavailable: "+s.agentOff)
			return
		}
		next(w, r)
	}
}

// agentRetentionEvery is how often saved conversations past the retention
// (Settings · Agent, Keep conversations) are pruned, besides at start and
// on every session list.
const agentRetentionEvery = time.Hour

// agentRetention prunes until Close.
func (s *Server) agentRetention() {
	s.pruneAgentChats()
	t := time.NewTicker(agentRetentionEvery)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			s.pruneAgentChats()
		case <-s.closed:
			return
		}
	}
}

// pruneAgentChats removes expired saved conversations and logs how many,
// never what they said. A locked vault is tried again next time.
func (s *Server) pruneAgentChats() {
	n, err := s.agent.Prune()
	if err == nil && n > 0 {
		s.note(wire.LogAgent, fmt.Sprintf("%d agent %s past the retention deleted from the vault", n, plural(n, "conversation", "conversations")), "", "", "")
	}
}

func (s *Server) agentAsk(w http.ResponseWriter, r *http.Request) {
	var req service.AskRequest
	if err := decode(w, r, bodyAsk, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid ask payload")
		return
	}
	resp, err := s.agent.Ask(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) agentEditProposal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Command string `json:"command"`
	}
	if err := decode(w, r, bodyAsk, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid proposal payload")
		return
	}
	p, err := s.agent.EditProposal(r.PathValue("id"), r.PathValue("pid"), req.Command)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) agentSessions(w http.ResponseWriter, _ *http.Request) {
	if s.agent == nil {
		writeJSON(w, http.StatusOK, service.Sessions{Sessions: []store.AgentChatSummary{}})
		return
	}
	out, err := s.agent.ListSessions()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// agentTranscript returns one conversation to resume. Reading it sends
// nothing anywhere and reads no pane.
func (s *Server) agentTranscript(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusNotFound, "no such conversation")
		return
	}
	t, err := s.agent.Transcript(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) agentRename(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}
	if err := decode(w, r, bodyAsk, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid rename payload")
		return
	}
	title, err := s.agent.Rename(r.PathValue("id"), req.Title)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": r.PathValue("id"), "title": title})
}

// agentProposeAgain makes a new proposal from a saved (expired) one. The
// new proposal is typed, if at all, through the session socket's
// approve-before-type path like any other.
func (s *Server) agentProposeAgain(w http.ResponseWriter, r *http.Request) {
	p, err := s.agent.ProposeAgain(r.PathValue("id"), r.PathValue("pid"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) agentReset(w http.ResponseWriter, r *http.Request) {
	if s.agent != nil {
		if err := s.agent.Delete(r.PathValue("id")); err != nil {
			writeErr(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// agentResetAll is Delete all history (Settings and the session list): it
// drops every conversation, in memory and saved in the vault, and writes
// one log entry with the count. What was said is never logged.
func (s *Server) agentResetAll(w http.ResponseWriter, _ *http.Request) {
	n := 0
	if s.agent != nil {
		var err error
		if n, err = s.agent.DeleteAll(); err != nil {
			writeErr(w, err)
			return
		}
	}
	s.note(wire.LogAgent, fmt.Sprintf("agent history deleted (%d %s)", n, plural(n, "conversation", "conversations")), "", "", "")
	w.WriteHeader(http.StatusNoContent)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// authorizeAgentType runs service.AuthorizeType for a client of sess and
// the "agent" part of its inject (wire.AgentType): the user pressed Type
// into pane on a proposal and approved the bytes. The role comes from the
// session as it is now, never from the client.
func (s *Server) authorizeAgentType(sess *runtime.Session, clientID uint64, msg *wire.AgentType, bytes string) error {
	if s.agent == nil {
		return errors.New("agent unavailable")
	}
	return s.agent.AuthorizeType(service.TypeRequest{
		ConversationID: msg.ConversationID,
		GroupRunID:     msg.GroupRunID,
		ProposalID:     msg.ProposalID,
		SessionID:      sess.Info().ID,
		Role:           sess.Role(clientID),
		Bytes:          bytes,
		Shift:          msg.Shift,
	})
}
