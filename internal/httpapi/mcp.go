package httpapi

import (
	"net/http"

	"keel/internal/agent"
	"keel/internal/agent/service"
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
	list, err := s.agent.ExternalPanes()
	if err != nil {
		writeErr(w, mcpWords(err))
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) mcpRead(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PaneID string `json:"paneId"`
		Lines  int    `json:"lines"`
	}
	if err := decode(w, r, bodyAgent, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid read payload")
		return
	}
	ex, err := s.agent.ExternalRead(req.PaneID, req.Lines)
	if err != nil {
		writeErr(w, mcpWords(err))
		return
	}
	writeJSON(w, http.StatusOK, ex)
}

func (s *Server) mcpPropose(w http.ResponseWriter, r *http.Request) {
	var req service.ExternalProposeRequest
	if err := decode(w, r, bodyAgent, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid proposal payload")
		return
	}
	p, err := s.agent.ExternalPropose(req)
	if err != nil {
		writeErr(w, mcpWords(err))
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
	st, err := s.agent.ExternalProposal(r.PathValue("id"))
	if err != nil {
		writeErr(w, mcpWords(err))
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// mcpWords are the words keel mcp passes on to Claude Code for a pane it
// may not read and for one that is gone.
func mcpWords(err error) error {
	err = worded(err, agent.ErrNotReadable, "the agent may not read this pane: turn on \"Agent can read\" for its host in Keel")
	return worded(err, service.ErrNotFound, "no such pane or proposal (list_panes gives the open panes)")
}
