package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"keel/internal/agent"
	"keel/internal/agent/service"
	"keel/internal/store"
)

// keel mcp's routes (docs/agent.md, Claude Code): the bridge that lets
// Claude Code list and read panes and propose commands. They are closed
// (403) until the user turns on "Allow Claude Code (keel mcp)" in Settings ·
// Agent (store.AgentSettings.ClaudeCode, off by default); then only keel
// mcp's own token opens them (auth.go), and they reach the agent service
// only:
//
//	GET  /api/mcp/panes            panes on hosts the agent may read
//	POST /api/mcp/read             one pane, redacted, logged agent.read
//	POST /api/mcp/propose          a command waiting in the agent panel
//	GET  /api/mcp/proposals/{id}   whether the user typed it
//
// Nothing here writes to a session, reads a vault secret or calls a model
// provider: a proposal is typed only by the user, through the session
// socket's approve-before-type path. mcp_test.go checks this file makes no
// write, inject or spawn call.

const maxMCPBody = 16 * 1024

func (s *Server) mcpPanes(w http.ResponseWriter, _ *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "the agent needs keel.log to record what it reads")
		return
	}
	list, err := s.agent.ExternalPanes()
	if err != nil {
		writeMCPErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) mcpRead(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "the agent needs keel.log to record what it reads")
		return
	}
	var req struct {
		PaneID string `json:"paneId"`
		Lines  int    `json:"lines"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxMCPBody)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid read payload")
		return
	}
	ex, err := s.agent.ExternalRead(req.PaneID, req.Lines)
	if err != nil {
		writeMCPErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ex)
}

func (s *Server) mcpPropose(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "the agent needs keel.log to record what it reads")
		return
	}
	var req service.ExternalProposeRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxMCPBody)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid proposal payload")
		return
	}
	p, err := s.agent.ExternalPropose(req)
	if err != nil {
		writeMCPErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"proposalId":     p.ID,
		"conversationId": service.ExternalConversationID,
		"conversation":   service.ExternalTitle,
		"paneId":         p.SessionID,
		"host":           p.Host,
		"risk":           p.Risk,
		"needsShift":     p.NeedsShift,
		"placeholders":   p.Placeholders,
	})
}

func (s *Server) mcpProposal(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "the agent needs keel.log to record what it reads")
		return
	}
	st, err := s.agent.ExternalProposal(r.PathValue("id"))
	if err != nil {
		writeMCPErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// writeMCPErr maps the agent service's errors for keel mcp, which passes
// the message on to Claude Code.
func writeMCPErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agent.ErrNotReadable):
		writeError(w, http.StatusForbidden, "the agent may not read this pane: turn on \"Agent can read\" for its host in Keel")
	case errors.Is(err, service.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such pane or proposal (list_panes gives the open panes)")
	case errors.Is(err, service.ErrBadRequest):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrBusy):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrNoPaneList):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, store.ErrLocked):
		writeError(w, http.StatusLocked, "the Keel vault is locked: unlock it in Keel")
	default:
		writeStoreErr(w, err)
	}
}
