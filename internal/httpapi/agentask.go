package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"keel/internal/agent"
	"keel/internal/agent/service"
	"keel/internal/runtime"
	"keel/internal/store"
)

// The agent panel's routes (phase 6b). POST /api/agent/ask is the only
// route in Keel that sends anything off the machine, and only what the
// agent service has read through the Auditor and redacted. These handlers
// never write to a session: typing a proposal goes through the session
// socket's approve-before-type path (sessionWS, "inject" with "agent"),
// which asks service.AuthorizeType first. agentask_test.go checks this
// file makes no write, inject or spawn call.

const maxAskBody = 32 * 1024

// WithAgentProvider sets how the agent builds its provider. keel serve
// passes service.FromEnv; tests pass a fake. Without it the real clients
// are used.
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

func (s *Server) initAgent() {
	if s.logs == nil || s.rt == nil || s.store == nil {
		return
	}
	svc, err := service.New(s.store, agentPanes{rt: s.rt}, s.logs, service.Options{Factory: s.agentFactory, Judge: s.agentJudge, TestProvider: s.agentTest, DetectLogWindow: s.agentDetectWindow})
	if err == nil {
		s.agent = svc
	}
}

func (s *Server) agentAsk(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "the agent needs keel.log to record what it reads")
		return
	}
	var req service.AskRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxAskBody)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid ask payload")
		return
	}
	resp, err := s.agent.Ask(r.Context(), req)
	if err != nil {
		writeAgentErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) agentEditProposal(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "agent unavailable")
		return
	}
	var req struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxAskBody)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid proposal payload")
		return
	}
	p, err := s.agent.EditProposal(r.PathValue("id"), r.PathValue("pid"), req.Command)
	if err != nil {
		writeAgentErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) agentReset(w http.ResponseWriter, r *http.Request) {
	if s.agent != nil {
		s.agent.Reset(r.PathValue("id"))
	}
	w.WriteHeader(http.StatusNoContent)
}

// agentResetAll is Settings, Delete all history: it drops every
// conversation the agent service keeps in memory and writes one log entry
// with the count. Nothing else is stored, and what was said is never logged.
func (s *Server) agentResetAll(w http.ResponseWriter, _ *http.Request) {
	n := 0
	if s.agent != nil {
		n = s.agent.ResetAll()
	}
	s.note("agent", fmt.Sprintf("agent history deleted (%d %s)", n, plural(n, "conversation", "conversations")), "", "", "")
	w.WriteHeader(http.StatusNoContent)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func writeAgentErr(w http.ResponseWriter, err error) {
	var pe *service.ProviderError
	switch {
	case errors.Is(err, service.ErrNoKey):
		writeJSON(w, http.StatusPreconditionFailed, map[string]string{"error": err.Error(), "reason": "no-key"})
	case errors.Is(err, service.ErrBadRequest), errors.Is(err, service.ErrNotProvider):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrBusy):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, context.Canceled):
		// The panel stopped waiting; nobody reads this.
		writeError(w, http.StatusRequestTimeout, "ask cancelled")
	case errors.As(err, &pe):
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": pe.Msg, "reason": "provider"})
	case errors.Is(err, store.ErrLocked), errors.Is(err, store.ErrNotFound):
		writeStoreErr(w, err)
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// agentTypeMsg is the "agent" part of a session socket "inject" message:
// the user pressed Type into pane on a proposal (and approved the bytes).
type agentTypeMsg struct {
	ConversationID string `json:"conversationId"`
	ProposalID     string `json:"proposalId"`
	Shift          bool   `json:"shift"`
}

// authorizeAgentType runs service.AuthorizeType for a client of sess. The
// role comes from the session as it is now, never from the client.
func (s *Server) authorizeAgentType(sess *runtime.Session, clientID uint64, msg *agentTypeMsg, bytes string) error {
	if s.agent == nil {
		return errors.New("agent unavailable")
	}
	info := sess.Info()
	role := agent.RoleFollower
	switch clientID {
	case info.DriverID:
		role = agent.RoleDriver
	case info.OwnerID:
		role = agent.RoleOwner
	}
	return s.agent.AuthorizeType(service.TypeRequest{
		ConversationID: msg.ConversationID,
		ProposalID:     msg.ProposalID,
		SessionID:      info.ID,
		Role:           role,
		Bytes:          bytes,
		Shift:          msg.Shift,
	})
}
